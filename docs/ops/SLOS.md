# Playback SLOs

## Scope

This document defines operator-facing SLOs for playback health and the telemetry contract used for enforcement.

## SLI Definitions

1. Start denominator

- Metric: `xg2g_playback_start_total{schema,mode}`
- Contract: strict labels only
  - `schema`: `live|recording`
  - `mode`: `hls|native_hls|hlsjs|mp4|unknown`

2. TTFF (Time To First Frame/Segment)

- Metric: `xg2g_playback_ttff_seconds_bucket{schema,mode,outcome}`
- Outcomes:
  - `ok`: first media served
  - `failed`: session failed before first media
  - `aborted`: session stopped before first media

3. Rebuffer proxy

- Metric: `xg2g_playback_rebuffer_total{schema,mode,severity}`
- Severity:
  - `minor`: media gap >= 12s
  - `major`: media gap >= 24s
- Threshold policy: currently identical for `live` and `recording` by design,
  because this is a request-gap proxy and must remain cross-schema comparable.
  Schema-specific thresholds are deferred until per-session target-duration
  telemetry is available and validated.

4. Error budget numerator

- Metric: `xg2g_playback_error_total{schema,stage,code}`
- Stages:
  - `playback_info`
  - `intent`
  - `playlist`
  - `segment`
  - `stream`
- `code` is a strict allowlist with unknown mapping (`UNKNOWN`) for safety.

## Default Targets

- TTFF p95:
  - `recording`: `< 4s`
  - `live`: `< 5s`
- Error rate:
  - `< 1%` over 30m
- Rebuffer proxy rate:
  - `< 0.5` events/min/session

## Burn-Rate Alerts (Suggested)

1. Fast burn (critical)

- Window pair: `5m` and `1h`
- Trigger when error-budget burn exceeds threshold in both windows.

2. Slow burn (warning)

- Window pair: `30m` and `6h`
- Trigger when sustained burn exceeds threshold in both windows.

### PromQL Examples (2-window)

Assume availability SLO = `99%` over `30d` (error budget = `1%`).

```promql
# Error rate by schema (recording/live), denominator clamped to avoid divide-by-zero.
(
  sum by (schema) (rate(xg2g_playback_error_total[5m]))
/
  clamp_min(sum by (schema) (rate(xg2g_playback_start_total[5m])), 1)
)
```

```promql
# Fast burn (critical): both windows must burn fast.
(
  (
    sum by (schema) (rate(xg2g_playback_error_total[5m]))
  /
    clamp_min(sum by (schema) (rate(xg2g_playback_start_total[5m])), 1)
  ) / 0.01
) > 14.4
and
(
  (
    sum by (schema) (rate(xg2g_playback_error_total[1h]))
  /
    clamp_min(sum by (schema) (rate(xg2g_playback_start_total[1h])), 1)
  ) / 0.01
) > 14.4
```

```promql
# Slow burn (warning): sustained burn across longer windows.
(
  (
    sum by (schema) (rate(xg2g_playback_error_total[30m]))
  /
    clamp_min(sum by (schema) (rate(xg2g_playback_start_total[30m])), 1)
  ) / 0.01
) > 6
and
(
  (
    sum by (schema) (rate(xg2g_playback_error_total[6h]))
  /
    clamp_min(sum by (schema) (rate(xg2g_playback_start_total[6h])), 1)
  ) / 0.01
) > 6
```

Use `for: 2m` on fast-burn and `for: 15m` on slow-burn alerts to reduce noise.

## Cardinality Guardrails

- Forbidden metric labels:
  - `request_id`
  - `session_id`
  - `recording_id`
  - `service_ref`
- These identifiers are log/trace join keys only, never metric labels.
