# Observability Guide

## Prometheus Metrics

The `xg2g` daemon exposes metrics at `:9090/metrics` (configurable via `XG2G_METRICS_ADDR`).

### v3 Capacity vs Faults

`R_LEASE_BUSY` is a capacity rejection (no tuner available), not a system fault. Metrics are split:

- `xg2g_v3_capacity_rejections_total{reason="R_LEASE_BUSY",...}`
- `xg2g_v3_session_starts_total{result="fail",reason_class!="none",...}`

### v3 Startup Metrics

Startup quality is tracked with these v3 metrics:

- `xg2g_v3_time_to_first_playlist_seconds` (TTFP)
- `xg2g_v3_time_to_first_segment_seconds` (TTFS)
- `xg2g_v3_session_starts_total{result,reason_class,profile,mode}`
- `xg2g_v3_capacity_rejections_total{reason="R_LEASE_BUSY",profile,mode}`

Interpretation:

- Measurement anchor: TTFP/TTFS timers start at worker session start (handleStart entry).
- Observation rule: TTFP/TTFS are observed only on success (failures/timeouts do not emit histogram observations).
- Polling behavior: TTFS readiness polling interval = 200ms, max wait = 30s.
- Outcomes: `xg2g_v3_session_starts_total` is recorded exactly once per session attempt at terminal state.
- Exact-once contract: “once per start attempt,” not per session ID; an attempt is each worker `handleStart` execution.
- Capacity vs fault: `R_LEASE_BUSY` is a capacity rejection and is tracked via `xg2g_v3_capacity_rejections_total`.

Metric definitions + label sets:

- `xg2g_v3_time_to_first_playlist_seconds{profile,mode}`
  - profile: auto|high|low|dvr|safari|safari_dvr|copy (canonical values from `internal/pipeline/profiles/resolve.go`; inputs are normalized)
  - mode: standard|virtual (worker execution mode; not a capacity label)
  - Observed only on success
- `xg2g_v3_time_to_first_segment_seconds{profile,mode}`
  - profile: auto|high|low|dvr|safari|safari_dvr|copy
  - mode: standard|virtual (worker execution mode; not a capacity label)
  - Observed only on success
- `xg2g_v3_session_starts_total{result,reason_class,profile,mode}`
  - result: success|fail|cancel
  - reason_class: none|timeout|tune_failed|ffmpeg_failed|packager_failed|bad_request|not_found|canceled|internal|unknown
  - profile: auto|high|low|dvr|safari|safari_dvr|copy
  - mode: standard|virtual (worker execution mode; not a capacity label)
  - Recorded exactly once per session attempt
  - Invariants: result=success implies reason_class=none; result!=success implies reason_class!=none
  - Note: capacity rejections (R_LEASE_BUSY) are not emitted in this metric
- `xg2g_v3_capacity_rejections_total{reason,profile,mode}`
  - reason: R_LEASE_BUSY only
  - profile: auto|high|low|dvr|safari|safari_dvr|copy
  - mode: standard|virtual (worker execution mode; not a capacity label)

Label governance:

- Do not add high-cardinality labels (session IDs, stream URLs, receiver IPs, filenames, error messages, correlation IDs).
- `reason_class` is bounded to the documented values; adding a new class requires updating this doc.
- `profile` must remain canonical and bounded; if profiles become user-defined, remove or bucket the label.

SLO example (target):

- Window: 30d, scoped per `profile` and `mode`
- Reliability: `start_success_rate >= 99%`
- Latency: `P99(TTFS) < 10s` for stable profiles on local network (initial target; calibrate after baseline data)

---

## Circuit Breaker Metrics

xg2g uses circuit breakers to protect against cascading failures when upstream services (e.g., Enigma2 receiver) become unhealthy.

### Metrics

| Metric Name | Type | Description | Labels |
|---|---|---|---|
| `xg2g_circuit_breaker_state` | Gauge | Current circuit breaker state (1=active, 0=inactive) | `component`, `state` |
| `xg2g_circuit_breaker_trips_total` | Counter | Total number of times circuit breaker opened | `component`, `reason` |

**States**:

- `closed`: Normal operation, requests pass through
- `half-open`: Testing if service recovered (single trial request)
- `open`: Circuit tripped, requests fail fast

**Trip Reasons**:

- `threshold_exceeded`: Consecutive failure threshold reached (default: 3 failures)
- `half_open_failure`: Trial request failed during recovery test

### Example Queries

**Current Circuit Breaker State**:

```promql
xg2g_circuit_breaker_state{component="refresh", state="open"}
```

**Circuit Breaker Trip Rate**:

```promql
rate(xg2g_circuit_breaker_trips_total[5m])
```

**Total Trips by Reason**:

```promql
sum by (reason) (xg2g_circuit_breaker_trips_total)
```

### Recommended Alert

```yaml
- alert: Xg2gCircuitBreakerOpen
  expr: xg2g_circuit_breaker_state{state="open"} == 1
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "xg2g: Circuit breaker {{ $labels.component }} is OPEN"
    description: |
      Component {{ $labels.component }} circuit breaker has opened.
      Upstream service is failing, requests are being rejected.
      Check logs and upstream health.
```

---

## Health Checks

- `/healthz`: Liveness probe. Returns 200 OK if the process is running.
- `/readyz`: Readiness probe. Returns 200 OK when ready to serve traffic, 503 if not ready (e.g. during startup or strict mode checks).
