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
  - `xg2g_recordings_preparing_total{probe_state,blocked_reason}` – preparing responses for recording playback truth
  - `xg2g_playback_start_total{schema,mode}` – playback start denominator for SLO ratios
  - `xg2g_playback_ttff_seconds_bucket{schema,mode,outcome}` – TTFF distribution
  - `xg2g_playback_rebuffer_total{schema,mode,severity}` – server-side rebuffer proxy events
  - `xg2g_playback_error_total{schema,stage,code}` – playback error budget numerator

## Playback SLOs

- **Schema labels:** `live|recording` (strict allowlist)
- **Mode labels:** `hls|native_hls|hlsjs|mp4|unknown` (strict allowlist)
- **Stages:** `playback_info|intent|playlist|segment|stream` (strict allowlist)
- **Join keys in logs only:** `request_id`, `session_id`, `recording_id`, `service_ref`
- **No high-cardinality IDs in metric labels** by contract

TTFF boundaries used by implementation:

- `recording`: start at successful `/recordings/{id}/stream-info` (non-deny), stop at first successful playlist/segment/mp4 media response for `session_id=rec:<recording_id>`.
- `live`: start at accepted `POST /api/v3/intents` (`stream_start`, stable session id), stop at first successful `/sessions/{sessionID}/hls/{filename}` response.

Rebuffer proxy thresholds:

- `minor`: media gap >= 12s
- `major`: media gap >= 24s
- Same thresholds are intentionally used for `live` and `recording` to keep
  server-side proxy behavior comparable across schemas. We will only split
  thresholds when reliable per-session target-duration telemetry is available.

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

- ADR: `docs/ADR/004-opentelemetry.md`
- Middleware stack: `internal/control/middleware/stack.go`
- Telemetry setup: `internal/telemetry/`
