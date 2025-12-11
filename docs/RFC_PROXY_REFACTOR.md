# RFC: Proxy Middleware Refactor

## Status

- **Date**: 2025-12-11
- **Status**: Proposed
- **Author**: Antigravity

## Problem

The `Service.handleRequest` function in `internal/proxy/proxy.go` has become a "God function". It currently manages:

- Logging
- HEAD request handling for Enigma2 compatibility
- HLS Client detection (iOS/Plex)
- HLS Streaming routing
- Transcoding pipeline selection (Stream Repair, GPU, Rust, FFmpeg)
- Direct Proxying fallback

This high complexity makes the proxy brittle, hard to test, and difficult to extend with new features (e.g. detailed metrics per stream type, custom buffering).

## Proposal

Refactor `handleRequest` into a "Chain of Responsibility" or Middleware pattern.

### Architecture

We will define a `Handler` interface:

```go
type ProxyHandler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request) (handled bool)
    SetNext(handler ProxyHandler)
}
```

Or simpler, since we just need a chain, we can use a slice of handlers that return `bool` (handled) or `error`.
However, standard HTTP middleware `func(http.Handler) http.Handler` works best for wrapping, but we need decision logic (Try A, if not relevant -> Try B).

**Better Approach: Priority Chain**
We define specialized handlers:

1. `HEADHandler`: Returns 200 OK for HEAD requests immediately.
2. `HLSHandler`: Checks User-Agent/URL. If match -> serve HLS. If not -> pass.
3. `TranscodeHandler`: Checks configs. Tries Stream Repair -> GPU -> Rust -> FFmpeg. If all fail -> pass (to fallback).
4. `DirectHandler`: The final fallback. Proxies directly to target.

### Implementation Plan

1. **Define Handler Interface**: `type RequestHandler func(w, r) bool` (returns true if handled).
2. **Extract Logic**: Move code blocks from `handleRequest` into:
   - `func (s *Server) handleHEAD(w, r) bool`
   - `func (s *Server) handleHLS(w, r) bool`
   - `func (s *Server) handleTranscode(w, r) bool`
3. **Refactor Main Loop**: `handleRequest` becomes a simple iterator over these functions.

### Benefits

- **Testability**: We can unit test `handleHLS` logic without mocking the entire Transcoder stack.
- **Extensibility**: Adding a new "MetricHandler" or "AuthHandler" becomes trivial.
- **readability**: `handleRequest` fits on one screen.

## Risks

- **Behavior Change**: Must ensure the *order* of operations remains exactly consistent (e.g. HLS check must happen before Transcoding check).
- **Context Passing**: Ensure `server` context (logger, config) is available to all handlers. Since they will be methods on `*Server`, this is covered.
