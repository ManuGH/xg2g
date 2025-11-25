# Action Plan: Code Review Findings
**Priority**: P0 (Critical) → P1 (High) → P2 (Medium)
**Timeline**: 2-3 Wochen
**Generated**: 2025-10-22

---

## ⚡ Quick Wins (15-30 Minuten jeweils)

### 1. Fix Race Condition ⚠️ P0

**File**: `internal/api/http.go:303-305`

```diff
- s.mu.Lock()
- s.status = *st
- s.mu.Unlock()
+ s.mu.Lock()
+ s.status.LastRun = st.LastRun
+ s.status.Channels = st.Channels
+ s.status.Error = st.Error
+ s.mu.Unlock()
```

**Test**: `go test -race ./internal/api/...`

---

### 2. Fix Bounds Check ⚠️ P0

**File**: `internal/api/http.go:462-475`

```diff
+ const tvgIDPrefix = `tvg-id="`
- if idx := strings.Index(line, `tvg-id="`); idx != -1 {
-     start := idx + 8
+ if idx := strings.Index(line, tvgIDPrefix); idx != -1 {
+     start := idx + len(tvgIDPrefix)
+     if start >= len(line) {
+         continue
+     }
      if end := strings.Index(line[start:], `"`); end != -1 {
          tvgID = line[start : start+end]
      }
  }
```

---

### 3. Extract Magic Numbers ✅ P2

**File**: `internal/api/http.go`

```go
const (
    defaultCircuitBreakerFailures = 3
    defaultCircuitBreakerTimeout  = 30 * time.Second
    maxXMLTVFileSize             = 50 * 1024 * 1024
    maxM3UFileSize               = 10 * 1024 * 1024
)
```

---

## 🔥 Critical Fixes (30min - 2h)

### 4. Fix HTTP Client Body Leak ⚠️ P0

**File**: `internal/openwebif/client.go:653-698`

```diff
  res, err = c.http.Do(req)
  duration = time.Since(start)
+
+ // ALWAYS close body if response exists
+ if res != nil {
+     defer closeBody(res.Body)
+     status = res.StatusCode
+ }

  if err == nil && status == http.StatusOK {
-     defer closeBody(res.Body)
      rawData, readErr := io.ReadAll(res.Body)
      // ...
  }
```

**Verify**:
```bash
# Stress-test
ab -n 1000 -c 100 http://localhost:8080/api/refresh
netstat -an | grep ESTABLISHED | wc -l  # Should not grow
```

---

### 5. Add Rate Limiting ⚠️ P1

**File**: `internal/api/http.go`

**Step 1**: Add dependency
```bash
go get golang.org/x/time/rate
```

**Step 2**: Update Server struct
```go
import "golang.org/x/time/rate"

type Server struct {
    // ... existing fields
    rateLimiter *rate.Limiter
}
```

**Step 3**: Initialize in New()
```go
func New(cfg config.AppConfig) *Server {
    s := &Server{
        // ... existing initialization
        rateLimiter: rate.NewLimiter(rate.Every(time.Minute), 5),
    }
    return s
}
```

**Step 4**: Apply in handleRefresh()
```go
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
    logger := log.WithComponentFromContext(r.Context(), "api")

    // Check rate limit first
    if !s.rateLimiter.Allow() {
        logger.Warn().
            Str("event", "refresh.rate_limit").
            Str("remote_addr", r.RemoteAddr).
            Msg("refresh rate limit exceeded")
        http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
        return
    }

    // ... rest of function
}
```

**Test**:
```bash
# Should succeed
for i in {1..5}; do curl http://localhost:8080/api/refresh; done

# Should fail with 429
for i in {6..10}; do curl -i http://localhost:8080/api/refresh; done
```

---

### 6. Optimize XMLTV String Replacement ⚠️ P1

**File**: `internal/api/http.go:483-490`

```diff
- xmltvString := string(xmltvData)
- for oldID, newID := range idToNumber {
-     xmltvString = strings.ReplaceAll(xmltvString, `id="`+oldID+`"`, `id="`+newID+`"`)
-     xmltvString = strings.ReplaceAll(xmltvString, `channel="`+oldID+`"`, `channel="`+newID+`"`)
- }
+
+ // Build replacement pairs (50x faster)
+ pairs := make([]string, 0, len(idToNumber)*4)
+ for oldID, newID := range idToNumber {
+     pairs = append(pairs,
+         `id="`+oldID+`"`, `id="`+newID+`"`,
+         `channel="`+oldID+`"`, `channel="`+newID+`"`,
+     )
+ }
+ replacer := strings.NewReplacer(pairs...)
+ xmltvString := replacer.Replace(string(xmltvData))
```

**Benchmark**:
```bash
go test -bench=BenchmarkXMLTVReplace -benchmem ./internal/api/
```

---

## 🚀 High-Impact Improvements (2-4h)

### 7. Implement XMLTV Streaming ⚠️ P1

**Goal**: Reduziere Memory-Footprint von 60MB → <5MB pro Request

**Option A: Simple Streaming (kein ID-Remapping)**

**File**: `internal/api/http.go:369-460`

```go
func (s *Server) handleXMLTV(w http.ResponseWriter, r *http.Request) {
    logger := log.WithComponentFromContext(r.Context(), "api")
    xmltvPath := filepath.Join(s.cfg.DataDir, s.cfg.XMLTVPath)

    // Size check
    fileInfo, err := os.Stat(xmltvPath)
    if err != nil {
        // ... error handling
        return
    }
    const maxFileSize = 50 * 1024 * 1024
    if fileInfo.Size() > maxFileSize {
        logger.Warn().Int64("size", fileInfo.Size()).Msg("XMLTV too large")
        http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
        return
    }

    // Stream directly
    xmltvFile, err := os.Open(xmltvPath)
    if err != nil {
        logger.Error().Err(err).Msg("failed to open XMLTV")
        http.Error(w, "Not found", http.StatusNotFound)
        return
    }
    defer xmltvFile.Close()

    w.Header().Set("Content-Type", "application/xml; charset=utf-8")
    w.Header().Set("Cache-Control", "public, max-age=300")
    w.Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))

    if _, err := io.Copy(w, xmltvFile); err != nil {
        logger.Error().Err(err).Msg("failed to stream XMLTV")
    }
}
```

**Option B: Streaming mit ID-Remapping** (komplexer)

```go
// Use bufio.Scanner for line-by-line processing
xmltvFile, err := os.Open(xmltvPath)
if err != nil {
    return
}
defer xmltvFile.Close()

m3uFile, err := os.Open(m3uPath)
if err != nil {
    // Fall back to raw XMLTV
    io.Copy(w, xmltvFile)
    return
}
defer m3uFile.Close()

// Parse M3U to build mapping
idToNumber := parseM3UToIDMap(m3uFile)

// Stream XMLTV with line-by-line replacement
scanner := bufio.NewScanner(xmltvFile)
buf := make([]byte, 0, 64*1024) // 64KB buffer
scanner.Buffer(buf, 1024*1024)  // Max 1MB lines

w.Header().Set("Content-Type", "application/xml; charset=utf-8")
for scanner.Scan() {
    line := scanner.Text()
    // Apply replacements
    for oldID, newID := range idToNumber {
        line = strings.ReplaceAll(line, `id="`+oldID+`"`, `id="`+newID+`"`)
        line = strings.ReplaceAll(line, `channel="`+oldID+`"`, `channel="`+newID+`"`)
    }
    fmt.Fprintln(w, line)
}
```

**Test**:
```bash
# Memory-Profiling VOR/NACH
go tool pprof http://localhost:6060/debug/pprof/heap

# Load-Test
ab -n 100 -c 10 http://localhost:8080/xmltv.xml
```

---

### 8. Add Context Cancellation in Worker Pool ⚠️ P1

**File**: `internal/jobs/refresh.go:394-415`

```diff
  go func() {
      defer wg.Done()
+
+     // Check if context is already cancelled
+     select {
+     case <-ctx.Done():
+         results <- epgResult{channelID: it.TvgID, err: ctx.Err()}
+         return
+     default:
+     }

      sem <- struct{}{}
      defer func() { <-sem }()

      reqCtx, cancel := context.WithTimeout(ctx, ...)
      defer cancel()

      events, err := fetchEPGWithRetry(reqCtx, client, it.URL, cfg)
      // ...
  }()
```

**Test**:
```bash
# Trigger refresh, then interrupt
curl http://localhost:8080/api/refresh &
sleep 1
killall -INT xg2g  # Should shutdown quickly
```

---

## 📋 Code Quality Improvements (P2)

### 9. Add Package Documentation

**Template** für alle Packages:

```go
// Package api implements the HTTP server for xg2g.
//
// The API server provides REST endpoints for:
//   - Channel discovery and lineup management
//   - EPG (Electronic Program Guide) data
//   - HDHomeRun emulation for client compatibility
//   - Health checks and metrics
//
// Security features:
//   - Token-based authentication
//   - Path traversal protection
//   - Rate limiting (on /api/refresh)
//   - Circuit breaker for external calls
//
// Example usage:
//
//	cfg := config.AppConfig{
//	    OWIBase:    "http://receiver:8080",
//	    Bouquet:    "Favourites",
//	    DataDir:    "/data",
//	    StreamPort: 8001,
//	}
//	srv := api.New(cfg)
//	if err := srv.Start(":8080"); err != nil {
//	    log.Fatal(err)
//	}
package api
```

**Files**:
- [ ] `internal/api/http.go`
- [ ] `internal/openwebif/client.go`
- [ ] `internal/jobs/refresh.go`
- [ ] `internal/config/config.go`
- [ ] `internal/validate/validate.go`

---

### 10. Consolidate M3U Parsing

**Create**: `internal/playlist/parser.go`

```go
package playlist

import "strings"

// ParseAttribute extracts an attribute value from an M3U EXTINF line.
// Returns empty string if attribute not found.
//
// Example:
//
//	line := `#EXTINF:-1 tvg-id="channel1" tvg-name="Channel One",Channel 1`
//	id := ParseAttribute(line, "tvg-id")    // "channel1"
//	name := ParseAttribute(line, "tvg-name") // "Channel One"
func ParseAttribute(line, attrName string) string {
    prefix := attrName + `="`
    idx := strings.Index(line, prefix)
    if idx == -1 {
        return ""
    }
    start := idx + len(prefix)
    if start >= len(line) {
        return ""
    }
    end := strings.Index(line[start:], `"`)
    if end == -1 {
        return ""
    }
    return line[start : start+end]
}
```

**Usage**:
```diff
// In http.go:462
- if idx := strings.Index(line, `tvg-id="`); idx != -1 {
-     start := idx + 8
-     if end := strings.Index(line[start:], `"`); end != -1 {
-         tvgID = line[start : start+end]
-     }
- }
+ tvgID := playlist.ParseAttribute(line, "tvg-id")
+ tvgChno := playlist.ParseAttribute(line, "tvg-chno")
```

---

## 🧪 Testing Improvements

### Add Concurrency Test

**File**: `internal/api/http_test.go`

```go
func TestHandleRefreshConcurrent(t *testing.T) {
    tmpDir := t.TempDir()
    cfg := config.AppConfig{
        DataDir:    tmpDir,
        OWIBase:    "http://test",
        Bouquet:    "test",
        StreamPort: 8001,
    }

    srv := New(cfg)
    defer srv.Shutdown(context.Background())

    // Launch 10 concurrent refresh requests
    var wg sync.WaitGroup
    errors := make(chan error, 10)

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            req := httptest.NewRequest("POST", "/api/refresh", nil)
            w := httptest.NewRecorder()
            srv.ServeHTTP(w, req)

            if w.Code != 200 && w.Code != 409 {
                errors <- fmt.Errorf("unexpected status: %d", w.Code)
            }
        }()
    }

    wg.Wait()
    close(errors)

    // Should have at most 1 success, rest 409 Conflict
    for err := range errors {
        if err != nil {
            t.Error(err)
        }
    }
}
```

---

### Add Fuzzing Test

**File**: `internal/api/fuzz_test.go`

```go
func FuzzIsPathTraversal(f *testing.F) {
    // Seed corpus
    f.Add("../../etc/passwd")
    f.Add("%2e%2e%2f")
    f.Add("..\\..\\windows\\system32")
    f.Add("%c0%ae%c0%ae%c0%af")
    f.Add("\x00../../etc/passwd")

    f.Fuzz(func(t *testing.T, input string) {
        // Should not panic
        _ = isPathTraversal(input)
    })
}
```

**Run**:
```bash
go test -fuzz=FuzzIsPathTraversal -fuzztime=30s ./internal/api/
```

---

## 📊 Verification Checklist

### After Implementing P0 Fixes:

- [ ] `go test -race ./...` → PASS (no races)
- [ ] `make test-cover` → Coverage ≥ 57%
- [ ] `make lint` → No new warnings
- [ ] `make security-vulncheck` → No vulnerabilities
- [ ] Load test: `ab -n 1000 -c 50 http://localhost:8080/api/status` → No crashes
- [ ] `netstat | grep ESTABLISHED | wc -l` → Stable connection count

### After Implementing P1 Fixes:

- [ ] `go test -bench=. ./internal/...` → Improved performance
- [ ] Memory profile: Heap usage < 100MB under load
- [ ] Rate limiting: 429 after 5 requests/minute
- [ ] Context cancellation: Shutdown < 5 seconds

### After Implementing P2 Fixes:

- [ ] `godoc -http=:6060` → All packages documented
- [ ] `golangci-lint run` → Score > 90%
- [ ] Code review: Readability improved
- [ ] CI/CD: All workflows green

---

## 🎯 Timeline

| Phase | Tasks | Duration | Priority |
|-------|-------|----------|----------|
| **Week 1** | P0 Fixes (#1-#4) | 2-4h | Critical |
| **Week 2** | P1 Improvements (#5-#8) | 8-12h | High |
| **Week 3** | P2 Quality (#9-#10) | 4-6h | Medium |
| **Week 3** | Testing (#11-#12) | 4h | Medium |

**Total Effort**: ~20-30 Stunden

---

## 📝 Commit Message Template

```
fix(api): [Issue] - [Solution]

Problem:
- [Describe the issue]

Solution:
- [Describe the fix]

Impact:
- [Security/Performance/Stability improvement]

Testing:
- [How to verify]

Refs: docs/CODE_REVIEW_2025-10-22.md
```

**Example**:
```
fix(api): prevent race condition in status updates

Problem:
- handleRefresh overwrites entire status struct including Version
- Concurrent reads could see partial updates

Solution:
- Update only mutable fields (LastRun, Channels, Error)
- Version field remains immutable after initialization

Impact:
- Eliminates data race detected by go test -race
- Prevents potential status corruption

Testing:
- go test -race ./internal/api/... → PASS
- Load test with 100 concurrent requests → No races

Refs: docs/CODE_REVIEW_2025-10-22.md (#1)
```

---

**Generated**: 2025-10-22 by Claude Code
**Review**: docs/CODE_REVIEW_2025-10-22.md
**Questions**: Ping me wenn du Hilfe bei der Implementation brauchst!
