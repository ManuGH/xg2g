# ADR-004: OpenTelemetry Integration

**Status:** Accepted  
**Date:** 2025-12-19

## Context

The sharp edges in `xg2g` are in streaming and receiver integration. When failures occur (HLS readiness, ffmpeg exits, upstream errors), we need correlation across:

- HTTP request lifecycle
- stream session lifecycle (start/stop/exit reason)
- background jobs

## Decision

- OpenTelemetry tracing is supported as an **opt-in** feature.
- Tracing is applied centrally in the canonical middleware stack, so ingress does not drift between API and proxy.
- Stream/job paths emit bounded, low-cardinality metrics and tracing spans (no per-URL labels).

## Consequences

- Operators can correlate a single request to its downstream work (jobs/streams) when traces are enabled.
- Tracing must not change routing behavior or break streaming interfaces (Flush/Hijack).

## References (Docs / Code)

- Observability guide: `docs/ops/OBSERVABILITY.md`
- Middleware wiring: `internal/control/middleware/stack.go`
- Telemetry setup: `internal/telemetry`
