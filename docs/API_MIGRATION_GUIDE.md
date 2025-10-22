# API Migration Guide

This guide helps you migrate from the legacy unversioned API (`/api/*`) to the stable versioned API (`/api/v1/*`).

## Why Migrate?

- **Stability**: Versioned APIs guarantee backwards compatibility
- **Future-proof**: Legacy endpoints will be removed in v2.0.0
- **Better support**: New features will only be added to versioned endpoints
- **Standards compliance**: Follows REST API best practices

## Timeline

| Date | Event |
|------|-------|
| **v1.5.0** | API versioning introduced, legacy endpoints deprecated |
| **2025-12-31** | Sunset date for legacy endpoints |
| **v2.0.0 (Q1 2026)** | Legacy endpoints removed |

You have **until 2025-12-31** to migrate. After that, legacy endpoints will stop working.

## Migration Checklist

- [ ] Identify all places where you call xg2g API
- [ ] Update URLs from `/api/` to `/api/v1/`
- [ ] Test updated integrations
- [ ] Monitor deprecation headers
- [ ] Update documentation/scripts

## Quick Migration

### Shell Scripts

**Before:**
```bash
curl http://localhost:8080/api/status
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: $TOKEN"
```

**After:**
```bash
curl http://localhost:8080/api/v1/status
curl -X POST http://localhost:8080/api/v1/refresh \
  -H "X-API-Token: $TOKEN"
```

### Python

**Before:**
```python
import requests

BASE_URL = "http://localhost:8080"
TOKEN = "your-token"

# Old endpoints
status = requests.get(f"{BASE_URL}/api/status").json()
refresh = requests.post(
    f"{BASE_URL}/api/refresh",
    headers={"X-API-Token": TOKEN}
)
```

**After:**
```python
import requests

BASE_URL = "http://localhost:8080"
TOKEN = "your-token"

# New versioned endpoints
status = requests.get(f"{BASE_URL}/api/v1/status").json()
refresh = requests.post(
    f"{BASE_URL}/api/v1/refresh",
    headers={"X-API-Token": TOKEN}
)
```

**Tip**: Use a constant for the API prefix to make future migrations easier:

```python
API_PREFIX = "/api/v1"
status = requests.get(f"{BASE_URL}{API_PREFIX}/status").json()
```

### Go

**Before:**
```go
resp, err := http.Get("http://localhost:8080/api/status")
```

**After:**
```go
resp, err := http.Get("http://localhost:8080/api/v1/status")
```

**Better (with constant)**:
```go
const (
    baseURL    = "http://localhost:8080"
    apiVersion = "v1"
)

func getStatus() (*Status, error) {
    url := fmt.Sprintf("%s/api/%s/status", baseURL, apiVersion)
    resp, err := http.Get(url)
    // ...
}
```

### JavaScript/TypeScript

**Before:**
```javascript
const response = await fetch('http://localhost:8080/api/status');
const data = await response.json();
```

**After:**
```javascript
const API_BASE = 'http://localhost:8080/api/v1';

const response = await fetch(`${API_BASE}/status`);
const data = await response.json();
```

### Docker Compose Health Checks

**Before:**
```yaml
services:
  xg2g:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/api/status"]
```

**After:**
```yaml
services:
  xg2g:
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/api/v1/status"]
```

**Note**: You can also use `/healthz` for health checks (this endpoint is versionless and won't be deprecated).

### Kubernetes Probes

**Before:**
```yaml
livenessProbe:
  httpGet:
    path: /api/status
    port: 8080
```

**After (Option 1 - Versioned API)**:
```yaml
livenessProbe:
  httpGet:
    path: /api/v1/status
    port: 8080
```

**After (Option 2 - Dedicated health endpoint)**:
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
```

## Changes Summary

### Endpoints Mapping

| Legacy Endpoint | New Endpoint | Status |
|----------------|--------------|--------|
| `GET /api/status` | `GET /api/v1/status` | ⚠️ Deprecated |
| `POST /api/refresh` | `POST /api/v1/refresh` | ⚠️ Deprecated |

### No Changes Required

The following endpoints are **not affected** and don't need migration:

- `/files/playlist.m3u` - File serving (versionless)
- `/xmltv.xml` - XMLTV endpoint (standard format)
- `/healthz` - Health check (infrastructure)
- `/readyz` - Readiness check (infrastructure)
- `/discover.json` - HDHomeRun discovery (hardware emulation)
- `/lineup.json` - HDHomeRun lineup (hardware emulation)
- `/device.xml` - UPnP device description (hardware emulation)

## Response Format Changes

**Good news**: The response format is **identical** between legacy and v1 endpoints!

The only difference is the addition of the `X-API-Version: 1` header in v1 responses.

**Example**:

```json
{
  "status": "ok",
  "version": "1.4.0",
  "lastRun": "2025-10-21T10:30:00Z",
  "channels": 150
}
```

This is the same in both `/api/status` and `/api/v1/status`.

## Detecting Deprecation

You can programmatically detect if you're using deprecated endpoints by checking response headers:

```python
response = requests.get("http://localhost:8080/api/status")

if response.headers.get("Deprecation") == "true":
    print("WARNING: Using deprecated API!")
    print(f"Sunset date: {response.headers.get('Sunset')}")
    print(f"Use instead: {response.headers.get('Link')}")
```

**Deprecation headers** (per [RFC 8594](https://datatracker.ietf.org/doc/html/rfc8594)):

- `Deprecation: true` - Indicates the endpoint is deprecated
- `Sunset: 2025-12-31T23:59:59Z` - When the endpoint will be removed
- `Link: </api/v1>; rel="successor-version"` - Points to the replacement
- `Warning: 299 - "..."` - Human-readable deprecation message

## Testing Your Migration

### 1. Check API Version Header

Verify you're calling the v1 API:

```bash
curl -I http://localhost:8080/api/v1/status | grep X-API-Version
# Should output: X-API-Version: 1
```

### 2. Ensure No Deprecation Headers

The v1 API should NOT return deprecation headers:

```bash
curl -I http://localhost:8080/api/v1/status | grep -i deprecation
# Should output nothing
```

### 3. Run Integration Tests

```bash
# Test status endpoint
curl http://localhost:8080/api/v1/status | jq '.status'
# Expected: "ok"

# Test refresh endpoint (requires token)
curl -X POST http://localhost:8080/api/v1/refresh \
  -H "X-API-Token: $XG2G_API_TOKEN" \
  | jq '.channels'
# Expected: number of channels
```

## Monitoring Recommendations

### 1. Set Up Alerts for Deprecation

If you're using a monitoring system (e.g., Prometheus + Alertmanager):

```yaml
# Example alert rule
- alert: UsingDeprecatedAPI
  expr: http_requests_total{path=~"/api/(status|refresh)"} > 0
  for: 1h
  labels:
    severity: warning
  annotations:
    summary: "Application using deprecated xg2g API"
    description: "Migrate to /api/v1 before 2025-12-31"
```

### 2. Log Deprecation Warnings

Add logging in your application:

```python
import logging

response = requests.get("http://localhost:8080/api/status")

if "Deprecation" in response.headers:
    logging.warning(
        f"Using deprecated API endpoint. "
        f"Sunset: {response.headers.get('Sunset')}. "
        f"Migrate to: {response.headers.get('Link')}"
    )
```

## Breaking Changes in v2.0.0

While v1 is fully backwards compatible with the legacy API, v2.0.0 (planned for Q1 2026) **may** introduce:

- Different response structures
- New authentication mechanisms
- Changed error formats
- Removed legacy endpoints

**Stay updated**: Watch the [CHANGELOG](../CHANGELOG.md) for v2 announcements.

## FAQ

### Q: Do I need to migrate immediately?

**A**: No, you have until **2025-12-31** to migrate. However, we recommend migrating as soon as possible to take advantage of the stability guarantees.

### Q: Will the response format change?

**A**: No, the v1 response format is identical to the legacy format. The only difference is the `X-API-Version` header.

### Q: Can I use both legacy and v1 endpoints during the transition?

**A**: Yes, both endpoints will work until v2.0.0. However, it's best to migrate all integrations at once to avoid confusion.

### Q: What happens after 2025-12-31?

**A**: Legacy endpoints (`/api/status`, `/api/refresh`) will return `410 Gone` and then be removed in v2.0.0.

### Q: How do I know which version I'm running?

```bash
docker exec xg2g xg2g --version
# Or
curl http://localhost:8080/api/v1/status | jq '.version'
```

### Q: Will health check endpoints (`/healthz`, `/readyz`) be versioned?

**A**: No, infrastructure endpoints remain versionless as they follow Kubernetes conventions.

### Q: Can I disable deprecation warnings?

**A**: Deprecation headers are always sent for deprecated endpoints. However, you can choose to ignore them in your client code (not recommended).

## Support

If you encounter issues during migration:

1. Check the [API v1 Contract documentation](API_V1_CONTRACT.md)
2. Search [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
3. Open a new issue with:
   - Your current integration (code samples)
   - Error messages (if any)
   - xg2g version (`docker exec xg2g xg2g --version`)

## Changelog

### v1.5.0 (Current)

- ✨ Introduced `/api/v1/*` endpoints
- ⚠️ Deprecated `/api/*` endpoints
- ✅ Full backwards compatibility maintained

### v2.0.0 (Planned Q1 2026)

- ❌ Remove `/api/*` legacy endpoints
- ✨ New features (TBD)
- 🔄 Potential breaking changes (will be documented)
