# Observability Guide

## Prometheus Metrics

The `xg2g` daemon exposes metrics at `:9090/metrics` (configurable via `XG2G_METRICS_ADDR`).

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
