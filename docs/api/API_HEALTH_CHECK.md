# API Health Check Feature

## Overview

The `/api/v1/status` endpoint now supports an enhanced health check that can verify receiver connectivity.

## Basic Status Check

**Endpoint:** `GET /api/v1/status`

```bash
curl http://localhost:8080/api/v1/status
```

**Response:**
```json
{
  "status": "ok",
  "version": "1.7.1",
  "lastRun": "2025-11-02T08:00:00Z",
  "channels": 150
}
```

## Enhanced Health Check with Receiver Verification

**Endpoint:** `GET /api/v1/status?check_receiver=true`

```bash
curl http://localhost:8080/api/v1/status?check_receiver=true
```

**Response (Healthy):**
```json
{
  "status": "ok",
  "version": "1.7.1",
  "lastRun": "2025-11-02T08:00:00Z",
  "channels": 150,
  "receiver": {
    "reachable": true,
    "responseTimeMs": 45
  }
}
```

**Response (Degraded - Receiver Unreachable):**
```json
{
  "status": "degraded",
  "version": "1.7.1",
  "lastRun": "2025-11-02T08:00:00Z",
  "channels": 150,
  "receiver": {
    "reachable": false,
    "responseTimeMs": 5000,
    "error": "context deadline exceeded"
  }
}
```

## Receiver Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `reachable` | boolean | Whether the receiver is accessible |
| `responseTimeMs` | number | Response time in milliseconds (optional) |
| `error` | string | Error message if unreachable (optional) |

## Use Cases

### Kubernetes Liveness Probe

```yaml
livenessProbe:
  httpGet:
    path: /api/v1/status
    port: 8080
  initialDelaySeconds: 15
  periodSeconds: 30
```

### Kubernetes Readiness Probe with Receiver Check

```yaml
readinessProbe:
  httpGet:
    path: /api/v1/status?check_receiver=true
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 6
  failureThreshold: 3
```

### Prometheus Alert on Receiver Failure

```yaml
- alert: ReceiverUnreachable
  expr: xg2g_receiver_reachable == 0
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Enigma2 receiver is unreachable"
    description: "xg2g cannot connect to receiver at {{ $labels.owi_base }}"
```

### External Monitoring (UptimeRobot, Pingdom)

Configure health check endpoint:
```
URL: http://your-server:8080/api/v1/status?check_receiver=true
Method: GET
Expected: HTTP 200 + {"status":"ok"}
Interval: 5 minutes
```

## Performance

- **Timeout:** 5 seconds max
- **Overhead:** Minimal - single HTTP HEAD request to receiver
- **Caching:** None - real-time check on every request
- **Cost:** ~45-100ms added latency when enabled

## Backward Compatibility

✅ The `?check_receiver=true` parameter is **optional**
✅ Without the parameter, behavior is identical to v1.7.0
✅ API contract is backward-compatible (additive change only)
✅ Existing clients are not affected

## Security

- Health check respects authentication if configured
- No sensitive data exposed in error messages
- Rate limiting applies to status endpoint
- Receiver credentials are not logged

## Example: Integration with Grafana

```promql
# Receiver reachability (1 = healthy, 0 = down)
xg2g_receiver_reachable

# Response time percentiles
histogram_quantile(0.95, rate(xg2g_receiver_check_duration_seconds_bucket[5m]))
```

## Migration Guide

No migration needed - this is a backward-compatible enhancement.

To enable receiver checks in your monitoring:
1. Update health check URLs to include `?check_receiver=true`
2. Optionally add Prometheus metrics for receiver status
3. Configure alerts based on `status` field ("degraded" indicates issues)

## Troubleshooting

### Receiver check always fails

**Check:**
1. `XG2G_OWI_BASE` is correctly configured
2. Receiver is accessible from xg2g container
3. Firewall allows connections to receiver port
4. Basic auth credentials are correct (if required)

**Debug:**
```bash
# Test receiver connectivity manually
curl -v http://your-receiver-ip/api/statusinfo

# Check xg2g logs
docker logs xg2g | grep "receiver health check"
```

### Slow response times

The health check has a 5-second timeout. If checks are slow:
- Verify network latency to receiver
- Check receiver load
- Consider using basic `/api/v1/status` without receiver check for liveness probes

## Related Documentation

- [Health Checks Guide](HEALTH_CHECKS.md)
- [API v1 Contract](API_V1_CONTRACT.md)
- [Prometheus Metrics](../deploy/monitoring/prometheus/alert_rules.yml)
