# Issue: Refactor internal/api/http.go - Split into Single Responsibility Modules

**Status:** 📋 Planned
**Priority:** Medium
**Effort:** ~2-3 hours
**Type:** Refactoring / Code Quality
**Assignee:** TBD

## Problem Statement

The `internal/api/http.go` file has grown to **1,007 lines** and bundles multiple responsibilities:
- HTTP routing and server setup
- File handling and path security
- Authentication and authorization
- Health check handlers
- Refresh operation handlers
- HDHomeRun integration
- API versioning logic

This violates the **Single Responsibility Principle** and makes the code:
- ❌ Hard to review (large diffs)
- ❌ Difficult to test in isolation
- ❌ Challenging to navigate and understand
- ❌ Risky to modify (tight coupling)

## Proposed Solution

Split `http.go` into focused, single-responsibility modules:

```
internal/api/
├── server.go         (~150 lines) - Core server struct, initialization, routing
├── handlers.go       (~200 lines) - Base HTTP handlers (status, health, ready, refresh)
├── files.go          (~250 lines) - File operations (XMLTV, M3U, path security)
├── auth.go           (~100 lines) - Authentication middleware and helpers
├── middleware.go     (~50 lines)  - Security headers and other middleware
├── versioning.go     (exists)     - API v1/v2 route registration
├── hdhr.go           (~100 lines) - HDHomeRun-specific handlers
└── http.go           (deprecated) - Kept temporarily for backward compatibility
```

## Benefits

### Code Quality
- ✅ Single Responsibility Principle compliance
- ✅ Easier code navigation (clear file names)
- ✅ Smaller, focused files (~100-250 lines each)
- ✅ Reduced cognitive load for developers

### Testing
- ✅ Isolated unit tests per module
- ✅ Easier to mock dependencies
- ✅ Faster test execution (can run subsets)
- ✅ Clear test file organization

### Maintenance
- ✅ Smaller diffs in pull requests
- ✅ Faster code reviews
- ✅ Reduced merge conflicts
- ✅ Easier to locate bugs

### Development
- ✅ Parallel development on different modules
- ✅ Clear ownership boundaries
- ✅ Easier onboarding for new contributors

## Milestones

### 🎯 Milestone 1: Planning & Analysis (30 min)
**Goal:** Understand current structure and dependencies

- [ ] Map all functions and their dependencies
- [ ] Identify shared state and interfaces
- [ ] Document current test coverage per function
- [ ] Create detailed function-to-file mapping
- [ ] Identify potential breaking changes

**Deliverables:**
- Function dependency graph
- Detailed migration plan
- Risk assessment document

---

### 🎯 Milestone 2: Extract File Handling (45 min)
**Goal:** Create `files.go` with all file-related operations

**Functions to move:**
- `dataFilePath(rel string) (string, error)` - Path resolution with security checks
- `handleXMLTV(w, r)` - XMLTV file serving
- `checkFile(ctx, path) bool` - File existence check
- `isPathTraversal(p string) bool` - Security validation
- `secureFileServer() http.Handler` - Secure static file serving

**Tasks:**
- [ ] Create `internal/api/files.go`
- [ ] Move file-related functions from `http.go`
- [ ] Add package-level documentation
- [ ] Update imports in `http.go`
- [ ] Create `internal/api/files_test.go`
- [ ] Move/adapt existing file tests
- [ ] Add new edge case tests
- [ ] Run tests: `go test -v ./internal/api/...`

**Test Checklist:**
- [ ] Path traversal prevention (`../`, `/etc/passwd`)
- [ ] Symlink escape detection
- [ ] File existence checks
- [ ] XMLTV serving with correct headers
- [ ] M3U serving with correct headers
- [ ] 404 handling for missing files
- [ ] Security headers on file responses

---

### 🎯 Milestone 3: Extract Authentication (30 min)
**Goal:** Create `auth.go` with authentication logic

**Functions to move:**
- `authRequired(next http.HandlerFunc) http.HandlerFunc` - Token validation wrapper
- `AuthMiddleware(next http.Handler) http.Handler` - Token validation middleware

**Tasks:**
- [ ] Create `internal/api/auth.go`
- [ ] Move authentication functions
- [ ] Add comprehensive documentation
- [ ] Update imports in `http.go`
- [ ] Create `internal/api/auth_test.go`
- [ ] Move existing auth tests
- [ ] Add token validation edge cases
- [ ] Run tests: `go test -v ./internal/api/...`

**Test Checklist:**
- [ ] Missing token (401 Unauthorized)
- [ ] Invalid token (403 Forbidden)
- [ ] Valid token (passes through)
- [ ] Empty token (401 Unauthorized)
- [ ] Malformed header (401 Unauthorized)
- [ ] Constant-time comparison (timing attack prevention)

---

### 🎯 Milestone 4: Extract Base Handlers (45 min)
**Goal:** Create `handlers.go` with core HTTP handlers

**Functions to move:**
- `handleStatus(w, r)` - Status endpoint
- `handleRefresh(w, r)` - Refresh trigger
- `HandleRefreshInternal(w, r)` - Internal refresh handler
- `handleHealth(w, r)` - Health check
- `handleReady(w, r)` - Readiness check
- `GetStatus() jobs.Status` - Status getter

**Tasks:**
- [ ] Create `internal/api/handlers.go`
- [ ] Move handler functions
- [ ] Ensure Server receiver methods work
- [ ] Add handler documentation
- [ ] Update imports in `http.go`
- [ ] Create `internal/api/handlers_test.go`
- [ ] Move existing handler tests
- [ ] Add missing handler tests
- [ ] Run tests: `go test -v ./internal/api/...`

**Test Checklist:**
- [ ] Status returns correct JSON structure
- [ ] Refresh with valid token succeeds
- [ ] Refresh without token fails (401)
- [ ] Health returns 200 OK
- [ ] Ready returns 503 before first refresh
- [ ] Ready returns 200 after successful refresh
- [ ] Concurrent refresh serialization
- [ ] Circuit breaker integration

---

### 🎯 Milestone 5: Extract Middleware (20 min)
**Goal:** Create `middleware.go` for HTTP middleware

**Functions to move:**
- `securityHeadersMiddleware(next http.Handler) http.Handler` - Security headers

**Tasks:**
- [ ] Create `internal/api/middleware.go`
- [ ] Move middleware functions
- [ ] Add middleware documentation
- [ ] Update imports in `http.go`
- [ ] Create `internal/api/middleware_test.go`
- [ ] Test security headers application
- [ ] Run tests: `go test -v ./internal/api/...`

**Test Checklist:**
- [ ] X-Content-Type-Options header present
- [ ] X-Frame-Options header present
- [ ] X-XSS-Protection header present
- [ ] Headers applied to all responses
- [ ] Headers not duplicated

---

### 🎯 Milestone 6: Extract HDHomeRun Logic (30 min)
**Goal:** Create `hdhr.go` for HDHomeRun-specific handlers

**Functions to move:**
- `handleLineupJSON(w, r)` - HDHomeRun lineup endpoint

**Tasks:**
- [ ] Create `internal/api/hdhr.go`
- [ ] Move HDHomeRun handler
- [ ] Add HDHomeRun documentation
- [ ] Update imports in `http.go`
- [ ] Create `internal/api/hdhr_test.go`
- [ ] Move HDHomeRun tests
- [ ] Add lineup format tests
- [ ] Run tests: `go test -v ./internal/api/...`

**Test Checklist:**
- [ ] Lineup JSON format validation
- [ ] Channel number generation
- [ ] Stream URL format
- [ ] Empty lineup handling
- [ ] Integration with refresh status

---

### 🎯 Milestone 7: Refactor Core Server (30 min)
**Goal:** Create clean `server.go` with minimal server logic

**Functions remaining:**
- `Server` struct definition
- `New(cfg jobs.Config) *Server` - Constructor
- `HDHomeRunServer() *hdhr.Server` - Getter
- `routes() http.Handler` - Route registration
- `Handler() http.Handler` - Public handler
- `NewRouter(cfg) http.Handler` - Public constructor (deprecated)
- `featureEnabled(flag string) bool` - Feature flag helper
- `registerV1Routes(r *mux.Router)` - Already in versioning.go
- `registerV2Routes(r *mux.Router)` - Already in versioning.go

**Tasks:**
- [ ] Create `internal/api/server.go`
- [ ] Move core server functions
- [ ] Keep Server struct with all methods (they're in other files now)
- [ ] Add comprehensive server documentation
- [ ] Mark `http.go` as deprecated (keep for imports)
- [ ] Update all internal imports to use new files
- [ ] Create `internal/api/server_test.go`
- [ ] Test server initialization
- [ ] Test route registration
- [ ] Run full integration tests

**Test Checklist:**
- [ ] Server initializes correctly
- [ ] All routes are registered
- [ ] Middleware order is correct
- [ ] HDHomeRun integration works
- [ ] Metrics integration works
- [ ] Circuit breaker initialization

---

### 🎯 Milestone 8: Integration & Verification (20 min)
**Goal:** Verify refactoring maintains all functionality

**Tasks:**
- [ ] Run full test suite: `go test -v ./...`
- [ ] Run integration tests: `go test -tags=integration -v ./test/integration/...`
- [ ] Run contract tests: `go test -tags=integration_fast -v ./test/contract/...`
- [ ] Run benchmarks: `go test -bench=. ./...`
- [ ] Check code coverage: `go test -coverprofile=coverage.out ./internal/api/...`
- [ ] Verify coverage >= 73% (current api package coverage)
- [ ] Build daemon: `go build ./cmd/daemon`
- [ ] Run smoke test with real OpenWebIF stub
- [ ] Check for performance regressions
- [ ] Update documentation if needed

**Acceptance Criteria:**
- [ ] All tests pass
- [ ] No performance regression (< 5% slower)
- [ ] Code coverage maintained or improved
- [ ] No new linter warnings
- [ ] Documentation updated

---

## Detailed Task List

### Pre-Implementation Checklist
- [ ] Create feature branch: `refactor/api-structure`
- [ ] Backup current `http.go`
- [ ] Document current test coverage
- [ ] Run baseline benchmarks
- [ ] Identify external packages that import `internal/api`

### Implementation Order

1. **files.go** - Least dependencies, safe to extract first
2. **auth.go** - Simple middleware, minimal state
3. **middleware.go** - Small, focused module
4. **hdhr.go** - Optional feature, isolated
5. **handlers.go** - Core business logic
6. **server.go** - Final cleanup

### Post-Implementation Checklist
- [ ] Update CHANGELOG.md
- [ ] Update ARCHITECTURE.md (if exists)
- [ ] Update developer documentation
- [ ] Create migration guide for external importers (if any)
- [ ] Submit pull request with detailed description
- [ ] Request code review from 2+ team members

---

## Test Strategy

### Unit Tests (Per Module)
Each new module gets dedicated test file:
- `files_test.go` - File operations
- `auth_test.go` - Authentication
- `middleware_test.go` - Middleware
- `hdhr_test.go` - HDHomeRun
- `handlers_test.go` - HTTP handlers
- `server_test.go` - Server initialization

### Integration Tests
Run existing integration tests to ensure:
- [ ] Full refresh flow works
- [ ] API authentication works
- [ ] File serving works
- [ ] Circuit breaker works
- [ ] HDHomeRun emulation works

### Contract Tests
Run existing contract tests:
- [ ] API contracts maintained
- [ ] Jobs contracts maintained
- [ ] HDHomeRun contracts maintained

### Performance Tests
Run benchmarks to detect regressions:
- [ ] BenchmarkRefreshSmall
- [ ] BenchmarkRefreshLarge
- [ ] Baseline: 75.19ms average refresh

---

## Risk Assessment

### Low Risk
- ✅ Pure code movement (no logic changes)
- ✅ All existing tests remain
- ✅ No external API changes
- ✅ No configuration changes

### Medium Risk
- ⚠️ Import path changes (breaks external importers)
- ⚠️ Test file reorganization (merge conflicts)
- ⚠️ Potential circular dependencies

### Mitigation
- Keep deprecated `http.go` with re-exports for backward compatibility
- Run full test suite after each milestone
- Commit after each milestone (atomic changes)
- Document all import path changes

---

## Success Metrics

### Code Quality
- Target: < 250 lines per file
- Current: 1,007 lines in single file
- Improvement: 75% reduction in max file size

### Test Coverage
- Maintain: >= 73% (current api package)
- Goal: >= 80% (improved testability)

### Performance
- No regression: < 5% slower
- Goal: Maintain current 75.19ms avg refresh

### Developer Experience
- Reduce PR review time by 40%
- Faster onboarding (clearer code structure)
- Fewer merge conflicts (smaller files)

---

## Dependencies

### Required Before Starting
- [ ] All pending PRs merged (avoid conflicts)
- [ ] Clean main branch
- [ ] All tests passing on main

### Blocked By
- None (can start immediately)

### Blocking
- None (non-critical refactoring)

---

## Follow-up Work (Future)

After this refactoring, consider:
1. Extract circuit breaker to `internal/api/circuitbreaker/`
2. Extract metrics to `internal/api/metrics/`
3. Create `internal/api/handlers/` package with subpackages
4. Add OpenAPI/Swagger documentation
5. Add request/response validation layer

---

## References

- Original file: `internal/api/http.go` (1,007 lines)
- Related issue: Code review feedback on file size
- Architecture: Follow SOLID principles
- Testing: Maintain >= 73% coverage

---

## Notes

- This is a **zero-functionality-change** refactoring
- All external APIs remain identical
- All tests must pass before merging
- Consider this a foundation for future API improvements
- Keep `http.go` for backward compatibility (deprecated)

---

**Created:** 2025-10-23
**Updated:** 2025-10-23
**Estimated Completion:** TBD (2-3 hours effort)
**Actual Completion:** TBD
