# Observability Guide

## Prometheus Metrics

The `xg2g` daemon exposes metrics at `:9090/metrics` (configurable via `XG2G_METRICS_ADDR`).

### Stream Telemetry

| Metric Name | Type | Description | Labels |
|---|---|---|---|
| `xg2g_stream_tune_duration_seconds` | Histogram | Time taken for receiver tuning (Zap + Delay). | `encrypted` |
| `xg2g_stream_startup_latency_seconds` | Histogram | End-to-end time from request to playlist availability. | `encrypted` |
| `xg2g_stream_first_segment_latency_seconds` | Histogram | Time from request until first media segment is written to disk. | `encrypted` |
| `xg2g_stream_first_segment_serve_latency_seconds` | Histogram | Time from request until first media segment is served to client via HTTP. | `encrypted` |
| `xg2g_stream_start_total` | Counter | Stream start outcomes. | `result` (success/failure), `reason`, `encrypted` |

> **Operational Insight**: The difference between `first_segment_serve_latency` and `first_segment_latency` is the **Delivery Gap**.
>
> - Gap > 5s indicates issues with the Reverse Proxy, Network, or Client buffering (not Receiver/FFmpeg).

#### Buckets

- **Tune Duration**: `[0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 13, 20]`
- **Startup Latency**: `[1, 2, 3, 5, 8, 13, 20, 30]`
- **First Segment (disk)**: `[1, 2, 3, 5, 8, 13, 20, 30]`
- **First Segment (serve)**: `[1, 2, 3, 5, 8, 13, 20, 30, 45, 60]`

### Failure Reasons

The `xg2g_stream_start_total` metric uses the `reason` label for precise debugging.
**Note**: Some reasons like `stream_connect_reset` are derived from FFmpeg stderr logs and may vary with FFmpeg versions.

- `success`: Stream started normally.
- `stream_connect_reset`: Connection reset/refused by receiver (race condition).
- `io_error`: FFmpeg I/O error (often receiver not ready / network reset).
- `zap_timeout`: WebAPI Zap request timed out or deadline exceeded.
- `zap_failed`: WebAPI Zap request failed (non-timeout).
- `ffmpeg_exit`: FFmpeg process exited unexpectedly (non-zero).
- `client_disconnect`: Client disconnected before startup completed.
- `internal_error`: Unexpected panic or error.

> **Note**: If multiple error patterns match (e.g., both "Connection reset" and "Input/output error"), the **connection-reset** classification takes precedence.

---

## Recommended Alerting Rules

Apply these rules to your Prometheus/Alertmanager configuration to monitor production health.

### 1. High Stream Start Failure Rate (>5%)

**Criticality**: Page
**Description**: Triggers if more than 5% of stream starts fail over a 5-minute window.

```yaml
- alert: Xg2gStreamStartFailuresHigh
  expr: |
    sum by (encrypted) (rate(xg2g_stream_start_total{result="failure"}[5m]))
    /
    clamp_min(sum by (encrypted) (rate(xg2g_stream_start_total[5m])), 0.001)
    > 0.05
  for: 10m
  labels:
    severity: page
  annotations:
    summary: "xg2g: High stream start failure ratio"
    description: "Failure ratio >5% over 10m. Investigate logs for 'reason' label breakdown."
```

### 2. Startup Latency Regression (P95 > 20s)

**Criticality**: Ticket
**Description**: Triggers if the 95th percentile of startup latency exceeds 20 seconds.

```yaml
- alert: Xg2gStreamStartupLatencyP95High
  expr: |
    histogram_quantile(0.95,
      sum by (le, encrypted) (rate(xg2g_stream_startup_latency_seconds_bucket[10m]))
    ) > 20
  for: 15m
  labels:
    severity: ticket
  annotations:
    summary: "xg2g: Stream startup latency p95 high"
    description: "P95 startup latency >20s for 15m. Likely receiver lag or FFmpeg contention."
```

### 3. Tune Duration Anomaly (Deviation from ~3s)

**Criticality**: Ticket
**Description**: Triggers if the median tune time deviates significantly from the expected ~3s baseline.
**Note**: This alert acts primarily as a **Regression Guardrail** against changes to the configured tuning delay, rather than a pure receiver performance metric.

```yaml
- alert: Xg2gStreamTuneDurationUnexpected
  expr: |
    histogram_quantile(0.50,
      sum by (le, encrypted) (rate(xg2g_stream_tune_duration_seconds_bucket[10m]))
    ) < 2.5
    OR
    histogram_quantile(0.50,
      sum by (le, encrypted) (rate(xg2g_stream_tune_duration_seconds_bucket[10m]))
    ) > 4.0
  for: 10m
  labels:
    severity: ticket
  annotations:
    summary: "xg2g: Tune duration deviated from baseline"
    description: "Median tune duration is outside 2.5s-4.0s range. Verify 'Post-Zap Delay' configuration."
```

## Recording Rules

```yaml
- record: xg2g:stream:first_segment_serve_latency:p95
  expr: histogram_quantile(0.95, sum by (le, encrypted) (rate(xg2g_stream_first_segment_serve_latency_seconds_bucket[10m])))

- record: xg2g:stream:first_segment_disk_latency:p95
  expr: histogram_quantile(0.95, sum by (le, encrypted) (rate(xg2g_stream_first_segment_latency_seconds_bucket[10m])))
```

### 4. High Delivery Gap (Client/Proxy Latency)

**Criticality**: Ticket
**Description**: Triggers if valid segments exist on disk but are slow to reach the client.
**Logic**: If Serve Latency P95 > 25s BUT Disk Latency P95 < 15s, the bottleneck is downstream (Proxy/Network).

```yaml
- alert: Xg2gStreamDeliveryGapHigh
  expr: |
    xg2g:stream:first_segment_serve_latency:p95 > 25
    AND
    xg2g:stream:first_segment_disk_latency:p95 < 15
    AND
    sum by (encrypted) (rate(xg2g_stream_start_total{result="success"}[10m])) > 0.1
  for: 15m
  labels:
    severity: ticket
  annotations:
    summary: "xg2g: High Delivery Gap detected"
    description: |
      Serve p95={{ printf "%.1fs" $value }} (encrypted={{ $labels.encrypted }}; see panels for disk p95).
      Condition: serve p95 >25s while disk p95 <15s and traffic >0.1/s → downstream bottleneck likely (proxy/network/client).
```

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
