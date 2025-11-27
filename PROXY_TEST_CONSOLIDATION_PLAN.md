# Proxy Test Consolidation Plan

## Current State

### proxy_test.go (314 lines)
- `TestNew` - Tests New() with various configs
- `TestHandleHeadRequest` - HEAD request handling
- `TestHandleGetRequest` - GET request proxying
- `TestIsEnabled` - Env variable XG2G_ENABLE_STREAM_PROXY
- `TestGetListenAddr` - Env variable XG2G_PROXY_PORT
- `TestGetTargetURL` - Env variable XG2G_PROXY_TARGET
- `TestShutdown` - Basic shutdown test

### proxy_edge_cases_test.go (379 lines)
- `TestProxyWithQueryParameters` - Query parameter forwarding
- `TestProxyWithLargeResponse` - Large (5MB) response handling
- `TestProxyBackendErrors` - Backend status codes (404, 500, 503, 401)
- `TestProxyWithCustomHeaders` - Header forwarding
- `TestProxyUnsupportedMethods` - POST, PUT, DELETE, PATCH, OPTIONS
- **TestHeadRequestHeaders** - ❌ DUPLICATE of TestHandleHeadRequest
- `TestShutdownWithActiveConnections` - Graceful shutdown with active connections
- **TestNewWithInvalidURL** - ❌ DUPLICATE of TestNew (invalid URL cases)
- `TestStartWithInvalidAddress` - Invalid listen address

### proxy_server_test.go (489 lines)
- **TestServerStart_ErrorPaths** - ❌ DUPLICATE of TestStartWithInvalidAddress
- **TestServerStart_Success** - ❌ DUPLICATE of TestShutdown
- **TestServerShutdown_GracefulShutdown** - ❌ DUPLICATE of TestShutdownWithActiveConnections
- **TestNew_InvalidTargetURL** - ❌ DUPLICATE of TestNewWithInvalidURL
- `TestNew_InvalidListenAddr` - Tests empty listen addr (unique)
- **TestServerIntegration_ReverseProxyHeaders** - ❌ DUPLICATE of TestProxyWithCustomHeaders
- `TestServerShutdown_ContextTimeout` - Shutdown timeout (unique)
- `TestServerIntegration_HTTPClientTimeouts` - Client timeouts (unique)

---

## Identified Duplicates

### 1. HEAD Request Tests (2 versions)
- **proxy_test.go**: `TestHandleHeadRequest` (lines 70-109)
- **proxy_edge_cases_test.go**: `TestHeadRequestHeaders` (lines 218-262)
- **Action**: Merge into TestHandleHeadRequest, add empty body check

### 2. New() with Invalid URL (2 versions)
- **proxy_test.go**: `TestNew` includes invalid URL case (lines 50-57)
- **proxy_edge_cases_test.go**: `TestNewWithInvalidURL` (lines 325-351)
- **proxy_server_test.go**: `TestNew_InvalidTargetURL` (lines 291-329)
- **Action**: Merge all into proxy_test.go::TestNew

### 3. Start Error Paths (2 versions)
- **proxy_edge_cases_test.go**: `TestStartWithInvalidAddress` (lines 353-378)
- **proxy_server_test.go**: `TestServerStart_ErrorPaths` (lines 51-118)
- **Action**: Merge into TestStartWithInvalidAddress

### 4. Shutdown Tests (2 versions)
- **proxy_test.go**: `TestShutdown` (lines 279-313) - basic
- **proxy_server_test.go**: `TestServerStart_Success` (lines 120-159) - same test
- **Action**: Keep TestShutdown in proxy_test.go, remove TestServerStart_Success

### 5. Graceful Shutdown with Connections (2 versions)
- **proxy_edge_cases_test.go**: `TestShutdownWithActiveConnections` (lines 264-323)
- **proxy_server_test.go**: `TestServerShutdown_GracefulShutdown` (lines 161-230)
- **Action**: Keep TestShutdownWithActiveConnections (better implementation with httptest)

### 6. Header Forwarding (2 versions)
- **proxy_edge_cases_test.go**: `TestProxyWithCustomHeaders` (lines 134-174)
- **proxy_server_test.go**: `TestServerIntegration_ReverseProxyHeaders` (lines 361-429)
- **Action**: Merge into TestProxyWithCustomHeaders

---

## Consolidation Strategy

### Keep and Enhance: proxy_test.go
**Purpose**: Core unit tests for proxy functionality

Merged tests:
- `TestNew` (merge invalid URL cases from proxy_server_test.go + proxy_edge_cases_test.go)
- `TestNew` (add empty listen addr case from proxy_server_test.go::TestNew_InvalidListenAddr)
- `TestHandleHeadRequest` (add empty body check from proxy_edge_cases_test.go::TestHeadRequestHeaders)
- `TestHandleGetRequest`
- `TestIsEnabled`
- `TestGetListenAddr`
- `TestGetTargetURL`
- `TestShutdown`

**Estimated size**: ~350 lines (from 314)

### Keep and Enhance: proxy_edge_cases_test.go
**Purpose**: Edge cases, error handling, integration scenarios

Merged tests:
- `TestProxyWithQueryParameters`
- `TestProxyWithLargeResponse`
- `TestProxyBackendErrors`
- `TestProxyWithCustomHeaders` (merge header checks from proxy_server_test.go::TestServerIntegration_ReverseProxyHeaders)
- `TestProxyUnsupportedMethods`
- Remove: ~~TestHeadRequestHeaders~~ (duplicate)
- `TestShutdownWithActiveConnections`
- Remove: ~~TestNewWithInvalidURL~~ (duplicate)
- `TestStartWithInvalidAddress` (merge logic from proxy_server_test.go::TestServerStart_ErrorPaths)
- Add: `TestServerShutdown_ContextTimeout` (from proxy_server_test.go)
- Add: `TestServerIntegration_HTTPClientTimeouts` (from proxy_server_test.go)

**Estimated size**: ~400 lines (from 379)

### Delete: proxy_server_test.go
**Reason**: All tests are duplicates or can be merged into other files

Tests to relocate before deletion:
- TestNew_InvalidListenAddr → merge into proxy_test.go::TestNew
- TestServerShutdown_ContextTimeout → move to proxy_edge_cases_test.go
- TestServerIntegration_HTTPClientTimeouts → move to proxy_edge_cases_test.go

**All other tests in this file are duplicates**.

---

## Impact Summary

### Before
- 3 files: 1,182 lines total
- Duplicates: ~40% redundant test coverage

### After
- 2 files: ~750 lines total
- Reduction: ~432 lines (36.5%)
- Coverage: Same (no loss)
- Organization: Clear separation (unit tests vs edge cases)

---

## Implementation Steps

1. **Phase 1**: Merge tests into proxy_test.go
   - Expand TestNew with all invalid URL/addr cases
   - Enhance TestHandleHeadRequest with empty body check

2. **Phase 2**: Reorganize proxy_edge_cases_test.go
   - Remove duplicate tests (TestHeadRequestHeaders, TestNewWithInvalidURL)
   - Merge TestStartWithInvalidAddress with logic from proxy_server_test.go
   - Add TestServerShutdown_ContextTimeout
   - Add TestServerIntegration_HTTPClientTimeouts
   - Enhance TestProxyWithCustomHeaders with response header checks

3. **Phase 3**: Delete proxy_server_test.go
   - Verify all unique tests are relocated
   - Delete file
   - Run tests to verify coverage unchanged

---

## Helpers and Supporting Code

Keep these files unchanged:
- `proxy_helpers_test.go` - Test utilities (getFreeAddr, waitForServer, etc.)
- `proxy_race_test.go` - Concurrency tests
- `proxy_config_test.go` - Config tests
- `proxy_bench_test.go` - Benchmarks
- Other proxy test files

---

## Verification

After consolidation, verify:
```bash
# All tests pass
go test ./internal/proxy -v

# Coverage unchanged
go test ./internal/proxy -cover

# No duplicates
grep -r "func Test" internal/proxy/*_test.go | sort | uniq -d
```
