# Context Usage Guidelines

This document explains when `context.Background()` is justified vs. when context should be propagated.

## Production Code Analysis

As of v1.7.0, there are **17 `context.Background()` usages in production code**:

### ✅ Justified Uses (All 17)

#### 1. Signal Handlers (3 occurrences)
**Files**: `cmd/daemon/main.go`, `internal/daemon/bootstrap.go`

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
```

**Justification**: Root-level signal handlers create top-level contexts. This is the entry point for cancellation propagation.

---

#### 2. Background Workers (1 occurrence)
**File**: `internal/gpu/queue.go:137`

```go
ctx, cancel := context.WithCancel(context.Background())
```

**Justification**: Long-lived background goroutine that outlives individual requests. Creates its own cancellation scope.

---

#### 3. Standalone Cache Operations (6 occurrences)
**File**: `internal/cache/redis.go`

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
```

**Justification**: Cache operations (Get/Set/Delete) are short-lived utility operations with their own timeout. Not part of request path.

---

#### 4. Shutdown Handlers (3 occurrences)
**Files**: `internal/daemon/bootstrap.go:112`, `internal/daemon/manager.go:129`

```go
return d.Shutdown(context.Background())
```

**Justification**: Shutdown operations need independent context to complete cleanup even if parent context is cancelled.

---

#### 5. Background Jobs (1 occurrence)
**File**: `internal/api/http.go:373`

```go
// Create independent context for background job
// Use Background() instead of request context to prevent premature cancellation
jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
```

**Justification**: Refresh job continues after client disconnect. Monitored separately via goroutine (line 378).

---

#### 6. Nil Context Fallbacks (2 occurrences)
**File**: `internal/log/context.go:22,30`

```go
if ctx == nil {
    ctx = context.Background()
}
```

**Justification**: Defensive programming for nil context parameters.

---

#### 7. Deprecated Helpers (1 occurrence)
**File**: `internal/openwebif/client.go:585`

```go
// Deprecated: Use client.StreamURL(ctx, ref, name) for proper context propagation.
func StreamURL(base, ref, name string) (string, error) {
    return NewWithPort(base, 0, Options{}).StreamURL(context.Background(), ref, name)
}
```

**Justification**: Legacy compatibility function marked as deprecated. New code uses context-aware version.

---

## Best Practices

### ✅ DO Use context.Background() for:
1. **Root contexts**: Main function, signal handlers
2. **Background workers**: Long-lived goroutines independent of requests
3. **Shutdown operations**: Cleanup that must complete regardless of parent cancellation
4. **Standalone operations**: Cache ops, health checks with their own timeout
5. **Background jobs**: Operations that outlive the HTTP request (with monitoring)

### ❌ DON'T Use context.Background() for:
1. **HTTP request handlers**: Use `r.Context()`
2. **RPC calls**: Propagate caller's context
3. **Database operations**: Use request context for cancellation
4. **Outbound API calls**: Propagate timeout/cancellation from caller

---

## Context Propagation Flow

```
HTTP Request (r.Context())
    ↓
Middleware (trace, logging, auth)
    ↓
Handler
    ↓
Business Logic
    ↓
OpenWebIF Client (receiver rate limiting)
    ↓
External API
```

### Example: Proper Propagation

```go
// ✅ GOOD: Propagate request context
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context() // Get request context

    status, err := s.fetchStatus(ctx) // Pass it through
    if err != nil {
        // Handle cancellation
        if ctx.Err() == context.Canceled {
            http.Error(w, "Request cancelled", 499)
            return
        }
        http.Error(w, err.Error(), 500)
        return
    }

    json.NewEncoder(w).Encode(status)
}

// ✅ GOOD: Respect context in downstream calls
func (s *Server) fetchStatus(ctx context.Context) (*Status, error) {
    // Rate limiting respects context
    if err := s.limiter.Wait(ctx); err != nil {
        return nil, err
    }

    // OpenWebIF call respects context
    return s.client.Status(ctx)
}
```

### Example: Background Job Pattern

```go
// ✅ GOOD: Background job with monitoring
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
    // Create independent context for job
    jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    // Monitor client disconnect (optional logging)
    go func() {
        <-r.Context().Done()
        if r.Context().Err() == context.Canceled {
            log.Info().Msg("client disconnected (job continues)")
        }
    }()

    // Run job with independent context
    result, err := s.refreshJob(jobCtx)
    // ... handle result
}
```

---

## Audit Results

**Date**: 2025-11-12
**Version**: v1.7.0
**Total Production Usage**: 17
**Unjustified Usage**: 0
**Test Code Usage**: 192 (acceptable)

### Conclusion

All `context.Background()` usages in production code are **architecturally justified**. The codebase follows Go best practices for context propagation.

### Future Improvements

1. ✅ **Rate limiting now respects context** (v1.7.0)
2. ✅ **Trace-ID propagation in logs** (v1.7.0)
3. Consider: Linter rule to warn on new `context.Background()` in request handlers

---

## Related Documentation

- [Testing Guidelines](testing-guidelines.md)
- [Observability](../operations/monitoring.md)
- [Rate Limiting](../features/rate-limiting.md)
