# Health Checks and Readiness Probes

xg2g provides production-ready health and readiness endpoints for Docker and Kubernetes deployments.

## Endpoints

### `/healthz` - Liveness Probe

**Purpose**: Indicates if the process is alive and responsive.

**Status Codes**:
- `200 OK`: Always (process is running)

**Response** (basic):
```json
{
  "status": "healthy",
  "version": "v1.2.3",
  "timestamp": "2025-11-01T12:00:00Z"
}
```

**Response** (with `?verbose=true`):
```json
{
  "status": "healthy",
  "version": "v1.2.3",
  "timestamp": "2025-11-01T12:00:00Z",
  "checks": {
    "playlist": {
      "status": "healthy",
      "message": "file exists and readable"
    },
    "xmltv": {
      "status": "healthy",
      "message": "file exists and readable"
    },
    "last_job_run": {
      "status": "healthy",
      "message": "last job run successful"
    }
  }
}
```

### `/readyz` - Readiness Probe

**Purpose**: Indicates if the service is ready to accept traffic.

**Status Codes**:
- `200 OK`: Service is ready
- `503 Service Unavailable`: Service is not ready

**Response** (ready):
```json
{
  "ready": true,
  "status": "healthy",
  "timestamp": "2025-11-01T12:00:00Z",
  "checks": {
    "playlist": {
      "status": "healthy",
      "message": "file exists and readable"
    },
    "xmltv": {
      "status": "healthy",
      "message": "file exists and readable"
    },
    "last_job_run": {
      "status": "healthy",
      "message": "last job run successful"
    }
  }
}
```

**Response** (not ready):
```json
{
  "ready": false,
  "status": "unhealthy",
  "timestamp": "2025-11-01T12:00:00Z",
  "checks": {
    "playlist": {
      "status": "unhealthy",
      "error": "file not found",
      "message": "/data/playlist.m3u"
    },
    "last_job_run": {
      "status": "unhealthy",
      "message": "no successful job run yet"
    }
  }
}
```

## Component Statuses

Each check can return one of three statuses:

- **`healthy`**: Component is fully functional
- **`degraded`**: Component is functional but with warnings (e.g., empty file, old data)
- **`unhealthy`**: Component is not functional

## Built-in Health Checks

1. **Playlist File Check**
   - Verifies playlist.m3u exists and is readable
   - Reports `degraded` if file is empty
   - Reports `unhealthy` if file is missing

2. **XMLTV File Check**
   - Verifies EPG file exists and is readable (if EPG is enabled)
   - Reports `degraded` if file is empty
   - Reports `unhealthy` if file is missing

3. **Last Job Run Check**
   - Verifies at least one successful refresh has completed
   - Reports `degraded` if last successful run is over 24 hours old
   - Reports `unhealthy` if no run yet or last run failed

## Docker Integration

### Dockerfile

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/healthz || exit 1
```

### docker-compose.yml

```yaml
services:
  xg2g:
    image: xg2g:latest
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 3s
      start_period: 5s
      retries: 3
```

## Kubernetes Integration

### Liveness Probe

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
  timeoutSeconds: 3
  failureThreshold: 3
```

### Readiness Probe

```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3
  successThreshold: 1
```

### Combined Example

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: xg2g
spec:
  containers:
  - name: xg2g
    image: xg2g:latest
    ports:
    - containerPort: 8080
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8080
      initialDelaySeconds: 10
      periodSeconds: 30
      timeoutSeconds: 3
      failureThreshold: 3
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 10
      timeoutSeconds: 3
      failureThreshold: 3
```

## Best Practices

1. **Liveness Probe**: Should only fail if the process needs to be restarted
   - Use `/healthz` without verbose mode for fast responses
   - Set reasonable timeout (3s recommended)
   - Don't check external dependencies

2. **Readiness Probe**: Should fail if the service can't handle traffic
   - Use `/readyz` to check if service is initialized
   - More frequent checks than liveness (10s vs 30s)
   - Can include external dependency checks

3. **Startup Probe** (Kubernetes 1.16+): For slow-starting containers
   ```yaml
   startupProbe:
     httpGet:
       path: /readyz
       port: 8080
     initialDelaySeconds: 0
     periodSeconds: 5
     timeoutSeconds: 3
     failureThreshold: 30  # 30 * 5s = 150s max startup time
   ```

## Monitoring

Health check metrics are automatically recorded via Prometheus:
- `http_requests_total{path="/healthz"}`
- `http_requests_total{path="/readyz"}`
- `http_request_duration_seconds{path="/healthz"}`
- `http_request_duration_seconds{path="/readyz"}`

## Troubleshooting

### Service always unhealthy

Check individual component status with verbose mode:
```bash
curl http://localhost:8080/readyz?verbose=true | jq
```

Look for `unhealthy` components and their error messages.

### Service never becomes ready

Common causes:
1. No initial refresh has run yet - trigger manual refresh:
   ```bash
   curl -X POST http://localhost:8080/api/refresh
   ```

2. Playlist or XMLTV file missing - check data directory:
   ```bash
   ls -la /data/
   ```

3. Last refresh failed - check logs for errors:
   ```bash
   docker logs xg2g 2>&1 | grep -i error
   ```
