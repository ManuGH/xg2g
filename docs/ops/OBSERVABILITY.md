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

- **Status:** Opt-in, **off by default**. Instrumentation is always compiled in;
  the exporter activates **only when `XG2G_OTEL_ENDPOINT` is set**. Otherwise a
  no-op provider is installed at startup, so spans cost nothing. See ADR-004.
- **Enable it** — point at a collector (Jaeger, Tempo, Grafana, …) and set:
  - `XG2G_OTEL_ENDPOINT` — OTLP endpoint, e.g. `tempo:4317` (unset = disabled)
  - `XG2G_OTEL_PROTOCOL` — `grpc` (default) or `http`
  - `XG2G_OTEL_SAMPLING` — `0.0`–`1.0` (default `1.0`)
  - `XG2G_OTEL_ENVIRONMENT` — `deployment.environment` attribute (default `production`)
- **Wired at:** `cmd/daemon/main.go` (`telemetry.NewProvider` + graceful shutdown);
  ingress spans via the canonical middleware stack (`internal/control/middleware/`).

### Spans — the live-playback trace

When enabled, one playback attempt produces an end-to-end trace:

```text
HTTP request                  (middleware: internal/control/middleware/)
  ├─ playback.plan            decision + ffprobe probes; attr: xg2g.path_id
  └─ ffmpeg.startup           spawn -> first HLS segment;
                              attr: xg2g.time_to_first_segment_ms,
                                    xg2g.session_id, xg2g.source_type, xg2g.hw_backend
```

Also instrumented: the Enigma2/OpenWebIF client (`xg2g.enigma2`, incl. retries),
the recordings decision path, and background refresh jobs.

- **Log ↔ trace correlation:** request logs carry `trace_id` / `span_id`
  (`internal/log/logger.go`), so you can pivot from a log line to its trace.
- **No credential leak:** span URL labels are path-only (`traceLabels`).

### Deliberately NOT traced (do not re-litigate)

- Prometheus metrics and zerolog logs are **not** migrated to OTel — they work;
  migration would be churn. Tracing is the only OTel signal exported.
- No per-HLS-segment spans (high-frequency file serving = trace spam; throughput
  is covered by metrics above).
- No long-lived steady-state transcode spans (use the watchdog metrics/events).
- SQLite queries are not traced (no current latency concern).

## Health & Readiness

- **Liveness:** `/healthz` – basic process alive check
- **Readiness:** `/readyz` – dependencies (DB, receiver) reachable

---

## References

- ADR: `docs/ADR/004-opentelemetry.md`
- Middleware stack: `internal/control/middleware/stack.go`
- Telemetry setup: `internal/telemetry/`
