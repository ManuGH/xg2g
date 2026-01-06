# Health & Readiness Endpoints

## Overview

xg2g provides standard Kubernetes-style health and readiness endpoints for operational monitoring and orchestration.

## Endpoints

### `/healthz` - Liveness Probe

**Purpose:** Indicates if the process is alive and able to serve requests.

**Behavior:**

- Returns `200 OK` if the process is running
- No external dependencies checked
- Fast response (< 10ms typical)
- Does NOT require authentication

**Response:**

```json
{
  "status": "ok",
  "timestamp": "2026-01-06T19:17:00Z"
}
```

**Usage:**

```bash
curl -i http://localhost:8088/healthz
# Expected: HTTP/1.1 200 OK
```

**Kubernetes Example:**

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8088
  initialDelaySeconds: 10
  periodSeconds: 10
```

---

### `/readyz` - Readiness Probe

**Purpose:** Indicates if the service is ready to handle traffic (dependencies available).

**Behavior:**

- Returns `200 OK` when service is ready
- Returns `503 Service Unavailable` when not ready
- Checks critical dependencies based on configuration
- Does NOT require authentication

**Readiness Criteria:**

- Configuration loaded and valid
- First successful data refresh completed (bouquets/EPG loaded)
- OpenWebIF reachable (if `READY_STRICT=true`)

**Response (Ready):**

```json
{
  "status": "ready",
  "timestamp": "2026-01-06T19:17:00Z"
}
```

**Response (Not Ready):**

```json
{
  "status": "not_ready",
  "reason": "waiting_for_first_refresh",
  "timestamp": "2026-01-06T19:17:00Z"
}
```

**Usage:**

```bash
curl -i http://localhost:8088/readyz
# Expected: HTTP/1.1 200 OK (when ready)
# Expected: HTTP/1.1 503 Service Unavailable (when not ready)
```

**Kubernetes Example:**

```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8088
  initialDelaySeconds: 5
  periodSeconds: 5
  failureThreshold: 3
```

---

## Configuration

### `XG2G_READY_STRICT`

**Type:** Boolean (default: `false`)

**Behavior:**

- `false` (default): Readiness based on internal state only
- `true`: Strict mode - also checks OpenWebIF connectivity

**Strict Mode Requirements:**

- `XG2G_OWI_BASE` must be configured
- OpenWebIF must be reachable
- Fail-start if OWI base URL is missing

**Example:**

```bash
export XG2G_READY_STRICT=true
export XG2G_OWI_BASE=http://receiver:80
./xg2g
```

**Fail-Start Behavior:**

```bash
# If READY_STRICT=true but OWI_BASE is missing:
FATAL: Strict readiness enabled (XG2G_READY_STRICT=true) but OpenWebIF base URL is missing.
```

---

## Testing

### Manual Verification

```bash
# 1. Start service
./xg2g

# 2. Test liveness (should always return 200 when process is alive)
curl -i http://localhost:8088/healthz

# 3. Test readiness (may return 503 initially, then 200 after first refresh)
curl -i http://localhost:8088/readyz

# 4. Test strict mode
export XG2G_READY_STRICT=true
export XG2G_OWI_BASE=http://invalid-receiver:80
./xg2g
# Expected: Fail-start with error message

# OR if service starts:
curl -i http://localhost:8088/readyz
# Expected: 503 (OWI unreachable)

curl -i http://localhost:8088/healthz
# Expected: 200 (healthz unaffected by OWI status)
```

### Integration Tests

Health and readiness endpoints are tested in:

- `internal/health/health_test.go` - Unit tests
- `internal/api/http_test.go` - Integration tests
- `test/contract/api_contract_test.go` - Contract tests
- `test/integration/api_fast_test.go` - Fast integration tests

---

## Operational Notes

### Load Balancer Configuration

**Recommended:**

- Use `/healthz` for liveness checks (determines if pod should be restarted)
- Use `/readyz` for readiness checks (determines if pod should receive traffic)

**Why separate checks?**

- Liveness failures → restart pod
- Readiness failures → remove from load balancer (but don't restart)

### Monitoring

**Prometheus Metrics:**
Health and readiness status can be monitored via Prometheus metrics at `/metrics`.

**Alerting:**

```yaml
# Example Prometheus alert
- alert: XG2GNotReady
  expr: up{job="xg2g"} == 1 and probe_success{endpoint="/readyz"} == 0
  for: 2m
  annotations:
    summary: "xg2g instance not ready for {{ $value }}m"
```

---

## Troubleshooting

### `/readyz` returns 503

**Common causes:**

1. **First refresh not complete:** Wait for initial bouquet/EPG load
2. **READY_STRICT=true and OWI unreachable:** Check `XG2G_OWI_BASE` configuration
3. **Configuration invalid:** Check logs for validation errors

**Debug:**

```bash
# Check logs for readiness state changes
journalctl -u xg2g -f | grep -i ready

# Check current status
curl -s http://localhost:8088/readyz | jq .
```

### `/healthz` returns non-200

**Cause:** Process is not running or crashed.

**Action:** Check process status and logs:

```bash
systemctl status xg2g
journalctl -u xg2g -n 100
```

---

## Implementation Details

**Location:** `internal/health/health.go` + `internal/api/http.go`

**Handler Registration:**

```go
r.Get("/healthz", s.handleHealth)
r.Get("/readyz", s.handleReady)
```

**Health Manager:**

- Manages readiness state
- Tracks first successful refresh
- Provides health/readiness handlers

**Contract:**

- `/healthz` → Always 200 if process alive
- `/readyz` → 200 when ready, 503 when not ready
- Both endpoints return JSON
- Both endpoints skip authentication
- Both endpoints skip OTEL tracing (performance)
