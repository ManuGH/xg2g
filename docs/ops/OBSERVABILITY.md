# Observability Guide

**Status:** Active Reference  
**Scope:** Logging, Metrics, Tracing, Health

---

## Logging

- **Format:** Structured JSON (zerolog)
- **Request correlation:** `X-Request-ID` header propagated through context
- **Truth:** Logs are the primary debug artifact; production logs MUST be parseable

## Metrics

- **Endpoint:** `/metrics` (Prometheus format)
- **Required metrics:**
  - `xg2g_sessions_active` – current active stream sessions
  - `xg2g_requests_total` – HTTP request counter by path/status
  - `xg2g_ffmpeg_processes` – active FFmpeg processes

## Tracing (OpenTelemetry)

- **Status:** Opt-in (see ADR-004)
- **Configuration:** `config.yaml` → `telemetry.otlp_endpoint`
- **Middleware:** `internal/control/middleware/stack.go`
- **Exporter:** OTLP/gRPC to configured endpoint

## Health & Readiness

- **Liveness:** `/health` – basic process alive check
- **Readiness:** `/ready` – dependencies (DB, receiver) reachable

---

## References

- ADR: `docs/adr/004-opentelemetry.md`
- Middleware stack: `internal/control/middleware/stack.go`
- Telemetry setup: `internal/telemetry/`
