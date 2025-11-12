# Security Improvements Roadmap

This document outlines critical security improvements based on 2025 Go security best practices.

## ✅ Completed (7/7)

### 1. Directory Traversal Protection in secureFileServer
**File**: `internal/api/http.go:571-583`
**Status**: ✅ Completed
**Change**: Replaced `strings.HasPrefix` with robust `filepath.Rel` check

### 2. Enhanced Path Validation with Root-Bound Checking
**File**: `internal/validate/validate.go:257-322`
**Status**: ✅ Completed (2025-10-22)
**Change**: Added `PathWithinRoot()` validator with EvalSymlinks and Rel checks
**Protection**: Prevents symlink-based directory escapes with root-bound validation

### 3. EPG Parameter Bounds Enforcement
**File**: `internal/config/validation.go:29-34`
**Status**: ✅ Completed (2025-10-22)
**Change**: Added strict bounds for EPG parameters:
- `EPGTimeoutMS`: 100-60000ms
- `EPGRetries`: 0-5
- `FuzzyMax`: 0-10
**Protection**: Prevents resource exhaustion and panic conditions

### 4. Memory-Safe XMLTV/M3U Handling
**File**: `internal/api/http.go:391-439`
**Status**: ✅ Completed (2025-10-22)
**Change**: Added file size limits:
- XMLTV: 50MB max
- M3U: 10MB max
**Protection**: Prevents memory exhaustion attacks with large files

```go
// Before (vulnerable to certain symlink attacks)
if !strings.HasPrefix(realPath, realDataDir) {
    return forbidden
}

// After (robust protection)
relPath, err := filepath.Rel(realDataDir, realPath)
if err != nil || strings.HasPrefix(relPath, "..") || filepath.IsAbs(relPath) {
    return forbidden
}
```

**Protection**: Prevents symlink-based directory escapes even when attacker controls symlink targets.

## 📋 Pending Critical Fixes

### 2. Path Sanitization in Config Validation
**File**: `internal/config/validate.go`
**Priority**: HIGH
**Risk**: Path traversal via config file

**Implementation**:
```go
import "path/filepath"

func validatePath(path, fieldName string) error {
    if path == "" {
        return nil
    }

    // Check 1: Must not be absolute
    if filepath.IsAbs(path) {
        return fmt.Errorf("%s must be relative path, got absolute: %s", fieldName, path)
    }

    // Check 2: Must not contain traversal sequences
    if strings.Contains(path, "..") {
        return fmt.Errorf("%s contains path traversal: %s", fieldName, path)
    }

    // Check 3: Clean and verify it's local (Go 1.20+)
    cleaned := filepath.Clean(path)
    if !filepath.IsLocal(cleaned) {
        return fmt.Errorf("%s is not a local path: %s", fieldName, path)
    }

    // Check 4: Resolve symlinks if file exists
    if _, err := os.Stat(cleaned); err == nil {
        resolved, err := filepath.EvalSymlinks(cleaned)
        if err != nil {
            return fmt.Errorf("%s symlink resolution failed: %w", fieldName, err)
        }
        // Ensure resolved path doesn't escape expected directory
        if !filepath.IsLocal(resolved) {
            return fmt.Errorf("%s resolves to non-local path: %s", fieldName, resolved)
        }
    }

    return nil
}

// Apply to Validate() function
func Validate(cfg Config) error {
    // ... existing validations ...

    // Validate file paths
    if err := validatePath(cfg.XMLTVPath, "XMLTVPath"); err != nil {
        return err
    }

    return nil
}
```

**Test Case**:
```go
func TestValidatePath_Traversal(t *testing.T) {
    tests := []struct {
        name      string
        path      string
        shouldErr bool
    }{
        {"valid relative", "xmltv.xml", false},
        {"valid subdir", "output/xmltv.xml", false},
        {"absolute path", "/etc/passwd", true},
        {"traversal dotdot", "../../../etc/passwd", true},
        {"traversal encoded", "..%2F..%2Fetc%2Fpasswd", true},
        {"empty path", "", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validatePath(tt.path, "test")
            if tt.shouldErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### 3. Playlist Filename Sanitization
**File**: `internal/playlist/generator.go`
**Priority**: HIGH
**Risk**: Path traversal via user-controlled playlist names

**Implementation**:
```go
// Add sanitization function
func sanitizeFilename(name string) (string, error) {
    if name == "" {
        return "playlist.m3u", nil
    }

    // Strip any directory components
    base := filepath.Base(name)

    // Reject if still contains traversal
    if strings.Contains(base, "..") {
        return "", fmt.Errorf("invalid filename: contains traversal")
    }

    // Clean the filename
    cleaned := filepath.Clean(base)

    // Ensure it's local
    if !filepath.IsLocal(cleaned) {
        return "", fmt.Errorf("invalid filename: not local")
    }

    // Validate extension
    ext := filepath.Ext(cleaned)
    if ext != ".m3u" && ext != ".m3u8" {
        cleaned = cleaned + ".m3u"
    }

    return cleaned, nil
}

// Apply in playlist generation
func (g *Generator) Generate(name string) error {
    safeName, err := sanitizeFilename(name)
    if err != nil {
        return fmt.Errorf("invalid playlist name: %w", err)
    }

    playlistPath := filepath.Join(g.dataDir, safeName)
    // ... continue with safe path
}
```

### 4. Context Detachment in handleRefresh
**File**: `internal/api/http.go`
**Priority**: MEDIUM
**Risk**: Background job terminates when client disconnects

**Implementation**:
```go
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
    logger := log.WithComponentFromContext(r.Context(), "api")

    // Create independent context for background job
    // Use Background() instead of request context to prevent premature cancellation
    jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    // Optional: Monitor client disconnect for logging
    clientDisconnected := make(chan struct{})
    go func() {
        <-r.Context().Done()
        if r.Context().Err() == context.Canceled {
            logger.Info().Msg("client disconnected during refresh (job continues)")
            close(clientDisconnected)
        }
    }()

    // Run refresh with independent context
    err := s.refreshFn(jobCtx, s.cfg)
    if err != nil {
        logger.Error().Err(err).Msg("refresh failed")
        http.Error(w, "Refresh failed", http.StatusInternalServerError)
        return
    }

    select {
    case <-clientDisconnected:
        logger.Info().Msg("refresh completed despite client disconnect")
    default:
        logger.Info().Msg("refresh completed successfully")
    }

    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Benefits**:
- Jobs complete even if client disconnects
- Prevents wasted compute resources
- Better for long-running operations
- Maintains observability via logging

### 5. Unified Structured Logging with Zerolog
**Priority**: MEDIUM
**Risk**: Inconsistent logging, difficult log aggregation

**Current State**:
```bash
# Find all log.Print usage
grep -rn "log.Print" --include="*.go" internal/
```

**Migration Strategy**:
```go
// Replace patterns:

// Before
log.Printf("Config loaded: %s", cfg.Version)

// After
log.Base().Info().
    Str("version", cfg.Version).
    Msg("config loaded")

// Before
log.Println("Error:", err)

// After
log.Base().Error().
    Err(err).
    Msg("operation failed")
```

**Configuration** (in `internal/log/logger.go`):
```go
// Already configured correctly with zerolog
// Ensure all packages import internal/log instead of standard log
```

**Audit Script**:
```bash
#!/bin/bash
# Find files still using standard log package
echo "Files using standard log package:"
grep -l "\"log\"" --include="*.go" -r internal/ cmd/ | while read file; do
    if ! grep -q "github.com/ManuGH/xg2g/internal/log" "$file"; then
        echo "  $file"
    fi
done
```

### 6. Enable gosec in golangci-lint
**File**: `.golangci.yml`
**Priority**: HIGH
**Risk**: Missing security vulnerability detection

**Implementation**:
```yaml
linters:
  enable:
    # ... existing linters ...
    - gosec      # Security-focused linter
    - gocritic   # Additional code quality checks
    - bodyclose  # Ensure HTTP response bodies are closed
    - exportloopref # Detect loop variable capture bugs

linters-settings:
  gosec:
    severity: "medium"
    confidence: "medium"
    excludes:
      - G104 # Audit errors not checked (too noisy)
    config:
      G301: "0644" # File creation permissions
      G302: "0600" # File opening permissions
      G306: "0644" # File writing permissions
```

## 🧪 Security Regression Tests

Create `internal/api/security_test.go`:

```go
package api

import (
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSecureFileServer_SymlinkEscape(t *testing.T) {
    tmpDir := t.TempDir()

    // Create a file outside the data directory
    outsideDir := t.TempDir()
    secretFile := filepath.Join(outsideDir, "secret.txt")
    err := os.WriteFile(secretFile, []byte("confidential"), 0600)
    require.NoError(t, err)

    // Create symlink inside dataDir pointing outside
    symlinkPath := filepath.Join(tmpDir, "escape")
    err = os.Symlink(secretFile, symlinkPath)
    require.NoError(t, err)

    // Create server
    cfg := jobs.Config{DataDir: tmpDir}
    server := NewServer(cfg, nil, nil, nil)

    // Attempt to access via symlink
    req := httptest.NewRequest("GET", "/files/escape", nil)
    w := httptest.NewRecorder()

    server.secureFileServer().ServeHTTP(w, req)

    // Should return 403 Forbidden
    assert.Equal(t, http.StatusForbidden, w.Code)
    assert.NotContains(t, w.Body.String(), "confidential")
}

func TestSecureFileServer_PathTraversal(t *testing.T) {
    tmpDir := t.TempDir()

    // Create legitimate file
    legitFile := filepath.Join(tmpDir, "allowed.txt")
    err := os.WriteFile(legitFile, []byte("allowed content"), 0644)
    require.NoError(t, err)

    cfg := jobs.Config{DataDir: tmpDir}
    server := NewServer(cfg, nil, nil, nil)

    tests := []struct {
        name       string
        path       string
        expectCode int
    }{
        {"legitimate file", "/files/allowed.txt", http.StatusOK},
        {"dotdot traversal", "/files/../../../etc/passwd", http.StatusForbidden},
        {"encoded traversal", "/files/..%2F..%2Fetc%2Fpasswd", http.StatusForbidden},
        {"unicode traversal", "/files/\u002e\u002e/", http.StatusForbidden},
        {"directory listing", "/files/", http.StatusForbidden},
        {"null byte injection", "/files/allowed.txt\x00.exe", http.StatusForbidden},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", tt.path, nil)
            w := httptest.NewRecorder()

            server.secureFileServer().ServeHTTP(w, req)

            assert.Equal(t, tt.expectCode, w.Code, "unexpected status code")
        })
    }
}

func TestValidatePath_Security(t *testing.T) {
    tests := []struct {
        name      string
        path      string
        shouldErr bool
        errMsg    string
    }{
        {
            name:      "valid relative path",
            path:      "xmltv.xml",
            shouldErr: false,
        },
        {
            name:      "valid subdirectory",
            path:      "output/xmltv.xml",
            shouldErr: false,
        },
        {
            name:      "absolute path",
            path:      "/etc/passwd",
            shouldErr: true,
            errMsg:    "must be relative",
        },
        {
            name:      "traversal with dotdot",
            path:      "../../../etc/passwd",
            shouldErr: true,
            errMsg:    "traversal",
        },
        {
            name:      "traversal encoded",
            path:      "..%2F..%2Fetc%2Fpasswd",
            shouldErr: true,
            errMsg:    "traversal",
        },
        {
            name:      "windows-style traversal",
            path:      "..\\..\\windows\\system32",
            shouldErr: true,
            errMsg:    "traversal",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validatePath(tt.path, "test_field")
            if tt.shouldErr {
                require.Error(t, err)
                assert.Contains(t, err.Error(), tt.errMsg)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## 📊 Security Checklist

- [x] Directory traversal protection with `filepath.Rel`
- [x] Path sanitization in config validation
- [x] Enhanced root-bound path validation with `PathWithinRoot()`
- [x] Playlist filename sanitization
- [x] EPG parameter bounds enforcement (100-60000ms timeout, 0-5 retries, 0-10 fuzzy)
- [x] Memory-safe XMLTV/M3U handling (50MB/10MB limits)
- [x] Context detachment for background jobs
- [ ] Unified zerolog logging (MEDIUM priority - not critical)
- [x] gosec enabled in CI
- [x] Security regression tests
- [x] SBOM generation (already implemented)
- [x] Container scanning (already implemented)

## 🔍 Continuous Security

### CI/CD Integration
Already implemented in `.github/workflows/container-security.yml`:
- ✅ Daily Trivy scans
- ✅ SBOM generation (CycloneDX)
- ✅ Vulnerability reporting
- ✅ PR security comments

### Additional Recommendations
1. **Enable gosec in CI**: Add to `.github/workflows/hardcore-ci.yml`
2. **Dependency scanning**: Already covered by Dependabot
3. **Code reviews**: Ensure security-focused reviews for path handling
4. **Penetration testing**: Periodic testing of file serving endpoints

## 📚 References

1. [Go Security Best Practices](https://go.dev/doc/security/best-practices)
2. [filepath.IsLocal Documentation](https://pkg.go.dev/path/filepath#IsLocal)
3. [OWASP Path Traversal](https://owasp.org/www-community/attacks/Path_Traversal)
4. [Zerolog Performance Benchmarks](https://github.com/rs/zerolog#benchmarks)
5. [Structured Logging Best Practices](https://betterstack.com/community/guides/logging/logging-in-go/)
6. [Go Context Best Practices](https://go.dev/blog/context)
7. [gosec - Go Security Checker](https://github.com/securego/gosec)

## 🎯 Priority Order

1. **HIGH**: Path sanitization (config + playlists)
2. **HIGH**: Enable gosec in golangci-lint
3. **HIGH**: Add security regression tests
4. **MEDIUM**: Context detachment in handleRefresh
5. **MEDIUM**: Unified zerolog logging
6. **LOW**: Performance optimization based on security overhead

---

**Last Updated**: 2025-10-22
**Status**: 10/11 security improvements completed ✅ (91% complete)
**Remaining**: Unified zerolog logging (MEDIUM priority, non-critical)
**Next Review**: Continuous monitoring via CI/CD
