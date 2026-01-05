# Safari HLS 405 Fix: HEAD Support + 410 Gone

**Date**: 2026-01-05
**Status**: ✅ **Implemented**
**Related**: Patch 1 DVR Windowing (Pre-Requisite for Production Test)

---

## Problem Statement

### Symptom

Safari player errors during teardown and occasional playback issues:
```
Failed to load resource: the server responded with a status of 405 (Method Not Allowed) (playlist.m3u8)
[V3Player] Video Element Error: MediaError
Error: Session failed: R_CLIENT_STOP: context canceled
```

### Root Cause Analysis

**Primary Issue**: HLS endpoint only supported GET, not HEAD
- Safari/AVPlayer makes HEAD requests to check Content-Length before streaming
- OpenAPI spec only defined `get:` operation, not `head:`
- Chi router returned 405 Method Not Allowed for HEAD requests

**Secondary Issue**: Terminal sessions returned 404 instead of 410
- After session stop, server returned 404 Not Found
- Safari interprets 404 as "temporarily unavailable" → aggressive retries
- 410 Gone signals "intentionally unavailable" → fewer retries

---

## Solution

### 1. HEAD Method Support

#### OpenAPI Spec Update ([api/openapi.yaml:2058-2081](../api/openapi.yaml:2058-2081))

```yaml
/sessions/{sessionID}/hls/{filename}:
  get:
    summary: Serve HLS playlist or segment
    operationId: serveHLS
    # ... (existing GET definition)

  head:
    summary: Get HLS content metadata (Safari compatibility)
    operationId: serveHLSHead
    tags:
      - v3
    security:
      - BearerAuth: [v3:read]
    responses:
      "200":
        description: HLS content metadata (headers only)
        headers:
          Content-Type:
            schema:
              type: string
          Content-Length:
            schema:
              type: integer
          Cache-Control:
            schema:
              type: string
```

**Impact**: Code generation now creates HEAD route + handler interface

---

#### Handler Implementation ([internal/api/server_impl_v3.go:38-46](../internal/api/server_impl_v3.go:38-46))

```go
// ServeHLSHead implements HEAD /sessions/{sessionID}/hls/{filename}.
// Safari uses HEAD requests to check Content-Length before streaming.
// This delegates to the same handler as GET (handleV3HLS), which internally
// uses http.ServeContent that automatically handles HEAD by sending only headers.
func (s *Server) ServeHLSHead(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID, filename string) {
	_ = sessionID
	_ = filename
	s.scopeMiddleware(ScopeV3Read)(http.HandlerFunc(s.handleV3HLS)).ServeHTTP(w, r)
}
```

**Why it works**:
- `http.ServeContent()` (used in hls.go:295,299) **automatically handles HEAD**
- When request method is HEAD, ServeContent sends only headers (no body)
- No duplicate logic needed - same handler for GET and HEAD

---

### 2. 410 Gone for Terminal States

#### Expired Sessions ([internal/v3/api/hls.go:95-102](../internal/v3/api/hls.go:95-102))

```go
if rec.ExpiresAtUnix > 0 && time.Now().Unix() > rec.ExpiresAtUnix {
	// 410 Gone: Session explicitly ended (better than 404 for Safari retry logic)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "session expired", http.StatusGone)
	return
}
```

---

#### Terminal States ([internal/v3/api/hls.go:113-127](../internal/v3/api/hls.go:113-127))

```go
if !validState {
	// Safari Fix: Use 410 Gone for terminal states (FAILED/CANCELLED/STOPPED)
	// This signals to Safari that the resource is intentionally unavailable and
	// reduces aggressive retry behavior during teardown.
	// For NEW/STARTING: Still use 404 (client should retry)
	statusCode := http.StatusNotFound
	message := "session not ready"
	if rec.State.IsTerminal() {
		statusCode = http.StatusGone
		message = "stream ended"
		w.Header().Set("Cache-Control", "no-store")
	}
	http.Error(w, message, statusCode)
	return
}
```

**Logic**:
- Terminal states (FAILED/CANCELLED/STOPPED) → **410 Gone**
- Non-terminal but invalid (NEW/STARTING when file missing) → **404 Not Found** (retry allowed)

---

## HTTP Status Code Semantics

| State | Status Code | Meaning | Safari Behavior |
|-------|-------------|---------|-----------------|
| **READY/DRAINING** | 200 OK | Content available | Normal playback |
| **NEW/STARTING/PRIMING** (file missing) | 404 Not Found | Temporarily unavailable | Retry with backoff |
| **FAILED/CANCELLED/STOPPED** | **410 Gone** | Intentionally terminated | Stop retrying |
| **Expired** | **410 Gone** | Session expired | Stop retrying |
| **Not Found** | 404 Not Found | Session doesn't exist | Stop after few retries |

---

## Testing

### Manual Verification

```bash
# 1. Start session
sessionID=$(curl -sX POST http://localhost:8080/api/v3/intents \
  -H "Content-Type: application/json" \
  -d '{"type":"stream.start","profileID":"safari","serviceRef":"1:0:1:445D:453:1:C00000:0:0:0:"}' \
  | jq -r '.sessionID')

# 2. Wait for READY
sleep 10

# 3. Test GET (should work)
curl -sv -X GET "http://localhost:8080/api/v3/sessions/$sessionID/hls/index.m3u8" 2>&1 | grep "< HTTP"
# Expected: HTTP/1.1 200 OK

# 4. Test HEAD (should now work, not 405)
curl -sv -I "http://localhost:8080/api/v3/sessions/$sessionID/hls/index.m3u8" 2>&1 | grep "< HTTP"
# Expected: HTTP/1.1 200 OK (before fix: 405 Method Not Allowed)

# 5. Stop session
curl -X POST "http://localhost:8080/api/v3/sessions/$sessionID/stop"

# 6. Wait for terminal state
sleep 2

# 7. Test after stop (should be 410 Gone, not 404)
curl -sv -X GET "http://localhost:8080/api/v3/sessions/$sessionID/hls/index.m3u8" 2>&1 | grep "< HTTP"
# Expected: HTTP/1.1 410 Gone (before fix: 404 Not Found)
```

---

### Expected Behavior After Fix

#### During Active Playback
```http
GET /api/v3/sessions/{id}/hls/index.m3u8
< HTTP/1.1 200 OK
< Content-Type: application/vnd.apple.mpegurl
< Cache-Control: no-store

HEAD /api/v3/sessions/{id}/hls/index.m3u8
< HTTP/1.1 200 OK
< Content-Type: application/vnd.apple.mpegurl
< Content-Length: 1234
< Cache-Control: no-store
(no body sent)
```

#### After Session Stop
```http
GET /api/v3/sessions/{id}/hls/index.m3u8
< HTTP/1.1 410 Gone
< Cache-Control: no-store

stream ended
```

---

## Impact on Safari Player

### Before Fix

**Symptoms**:
- HEAD requests failed with 405 → Safari fell back to GET (extra request)
- Terminal sessions returned 404 → Safari retried aggressively
- Console errors: "Failed to load resource: 405 Method Not Allowed"
- Race condition during teardown: Player tried to load segments from stopped session

**Workaround**: Safari eventually succeeded via retry, but with:
- Extra network requests
- Console spam
- Delayed error recovery

---

### After Fix

**Improvements**:
- ✅ HEAD requests succeed immediately (no retry needed)
- ✅ 410 Gone stops Safari retries faster (cleaner teardown)
- ✅ Fewer console errors
- ✅ Better compliance with HTTP semantics

**Side Effects**: None (backwards compatible)
- Existing GET-only clients: No change
- Safari/modern players: Improved experience

---

## Regression Prevention

### OpenAPI Contract Test

```yaml
# api/openapi.yaml validation
- HEAD /sessions/{sessionID}/hls/{filename} MUST be defined
- Response MUST include Content-Length header
- Response MUST include Content-Type header
```

### Integration Test

```go
// internal/api/hls_integration_test.go
func TestHLS_HeadMethodSupport(t *testing.T) {
    // Setup active session
    sessionID := createTestSession(t)

    // Test HEAD request
    req := httptest.NewRequest("HEAD", "/api/v3/sessions/"+sessionID+"/hls/index.m3u8", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    assert.NotEmpty(t, w.Header().Get("Content-Length"))
    assert.Empty(t, w.Body.String()) // HEAD must not return body
}

func TestHLS_TerminalState410(t *testing.T) {
    // Setup stopped session
    sessionID := createStoppedSession(t)

    // Test GET after stop
    req := httptest.NewRequest("GET", "/api/v3/sessions/"+sessionID+"/hls/index.m3u8", nil)
    w := httptest.NewRecorder()
    handler.ServeHTTP(w, req)

    assert.Equal(t, http.StatusGone, w.Code)
    assert.Contains(t, w.Body.String(), "stream ended")
}
```

---

## Related Issues

### Why Not OPTIONS?

**Question**: Should we also add OPTIONS support for CORS preflight?

**Answer**: **CORS Middleware already handles OPTIONS globally**
- [internal/api/middleware/cors.go:55-57](../internal/api/middleware/cors.go:55-57) sets:
  - `Access-Control-Allow-Methods: GET, POST, OPTIONS, DELETE, PUT, PATCH`
  - `Access-Control-Allow-Headers: Content-Type, X-Request-ID, X-API-Token, Authorization`
- OPTIONS requests never reach the handler (intercepted by middleware)
- No explicit OPTIONS route needed

---

### Grace Period for Teardown?

**Question**: Should sessions stay alive 5-15s after stop to prevent race conditions?

**Current Behavior**:
- Session transitions to STOPPED immediately
- Files deleted immediately
- Safari may retry during this window → 410 Gone

**Recommendation**: **Optional future enhancement**
- Add `GracePeriodSec` config (default 10s)
- Keep session state and files for grace period after stop
- After grace period: Transition to terminal + cleanup

**Why defer**: 410 Gone already reduces retry storms. Grace period is optimization, not correctness fix.

---

## Summary

✅ **HEAD Support**: Safari can check Content-Length without 405 error
✅ **410 Gone**: Terminal sessions signal "stop retrying" to Safari
✅ **Backwards Compatible**: No breaking changes for existing clients
✅ **HTTP Semantics**: Correct use of status codes per RFC 7231

**Key Benefit**: Cleaner Safari teardown, fewer console errors, better HTTP compliance

**Next Phase**: Patch 1 Production Test can now proceed without 405/410 noise obscuring real issues.
