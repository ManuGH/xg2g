# ADR-004: OpenTelemetry Integration

**Status:** Accepted  
**Date:** 2025-12-19

**Update (2026-06-03):** The exporter is now actually initialized at startup
(`cmd/daemon/main.go` -> `telemetry.NewProvider`), gated on `XG2G_OTEL_ENDPOINT`.
Previously the instrumentation existed but the provider was never created, so no
traces were exported. Added spans: `playback.plan` (decision + ffprobe probes)
and `ffmpeg.startup` (spawn -> first segment). See `docs/ops/OBSERVABILITY.md`.

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
