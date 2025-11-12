# API v1 Contract

This document defines the stable API contract for xg2g v1. This contract follows semantic versioning principles and will not introduce breaking changes before v2.0.0.

## Version Information

- **Current Version**: v1
- **Introduced**: v1.5.0
- **Stability**: STABLE
- **Breaking Changes**: None planned before v2.0.0

## Base URLs

- **Versioned (recommended)**: `/api/v1`
- **Legacy (deprecated)**: `/api` (will be removed in v2.0.0, sunset date: 2025-12-31)

All new integrations MUST use the versioned `/api/v1` endpoints.

## Authentication

All mutative endpoints require authentication via the `X-API-Token` header.

```http
X-API-Token: your-secret-token
```

Authentication can be configured via the `XG2G_API_TOKEN` environment variable. If no token is configured, the endpoint will respond with `401 Unauthorized`.

## Common Headers

### Request Headers

| Header | Required | Description |
|--------|----------|-------------|
| `X-API-Token` | For mutative endpoints | API authentication token |

### Response Headers

| Header | Always Present | Description |
|--------|----------------|-------------|
| `Content-Type` | Yes | Always `application/json` |
| `X-API-Version` | Yes | API version (currently "1") |
| `X-Content-Type-Options` | Yes | Security header: `nosniff` |
| `X-Frame-Options` | Yes | Security header: `DENY` |
| `Content-Security-Policy` | Yes | Security header |

## Endpoints

### GET /api/v1/status

Returns the current operational status of the xg2g service.

**Authentication**: Not required

**Response (200 OK)**:

```json
{
  "status": "ok",
  "version": "1.4.0",
  "lastRun": "2025-10-21T10:30:00Z",
  "channels": 150
}
```

**Response Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Always "ok" (reserved for future health states) |
| `version` | string | xg2g version string |
| `lastRun` | string (RFC3339) | Timestamp of last successful refresh |
| `channels` | number | Total number of channels in current playlist |

**Stability**: STABLE - This response structure will not change in backwards-incompatible ways.

**Example**:

```bash
curl -s http://localhost:8080/api/v1/status | jq .
```

---

### POST /api/v1/refresh

Triggers a manual refresh of the playlist and EPG data.

**Authentication**: Required (`X-API-Token` header)

**Request**:

```http
POST /api/v1/refresh HTTP/1.1
Host: localhost:8080
X-API-Token: your-secret-token
```

**Response (200 OK)**:

```json
{
  "version": "1.4.0",
  "lastRun": "2025-10-21T10:35:00Z",
  "channels": 150
}
```

**Response Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | xg2g version string |
| `lastRun` | string (RFC3339) | Timestamp of this refresh |
| `channels` | number | Total number of channels refreshed |

**Error Responses**:

**401 Unauthorized** - Missing or invalid API token:

```json
{
  "error": "Unauthorized: Missing API token"
}
```

**403 Forbidden** - Invalid API token:

```json
{
  "error": "Forbidden: Invalid API token"
}
```

**409 Conflict** - Another refresh is already in progress:

```json
{
  "error": "conflict",
  "detail": "A refresh operation is already in progress"
}
```

**Response Headers**:
- `Retry-After: 30` (suggests retry after 30 seconds)

**503 Service Unavailable** - Circuit breaker is open due to repeated failures:

```json
{
  "error": "unavailable",
  "detail": "Refresh temporarily disabled due to repeated failures"
}
```

**500 Internal Server Error** - Refresh operation failed:

```text
Internal server error
```

Note: Internal error details are never exposed to clients for security reasons.

**Stability**: STABLE

**Example**:

```bash
curl -X POST http://localhost:8080/api/v1/refresh \
  -H "X-API-Token: your-secret-token" \
  | jq .
```

---

## Legacy API Deprecation

The unversioned `/api/*` endpoints are deprecated and will be removed in v2.0.0.

**Deprecation Timeline**:

- **Deprecated as of**: v1.5.0
- **Sunset Date**: 2025-12-31T23:59:59Z
- **Removal**: v2.0.0 (Q1 2026)

**Migration Path**:

Replace all `/api/` URLs with `/api/v1/`:

- `/api/status` → `/api/v1/status`
- `/api/refresh` → `/api/v1/refresh`

**Deprecation Headers**:

Legacy endpoints return the following headers:

```http
Deprecation: true
Sunset: 2025-12-31T23:59:59Z
Link: </api/v1>; rel="successor-version"
Warning: 299 - "This API is deprecated. Use /api/v1 instead. Will be removed in version 2.0.0"
```

These headers are defined in [RFC 8594 (Sunset)](https://datatracker.ietf.org/doc/html/rfc8594) and [RFC 7234 (Warning)](https://datatracker.ietf.org/doc/html/rfc7234).

---

## Error Handling

All endpoints follow consistent error handling:

1. **HTTP Status Codes**: Appropriate status codes (400, 401, 403, 404, 409, 500, 503)
2. **JSON Errors**: Error responses are JSON-formatted where possible
3. **Security**: Internal error details are never exposed
4. **Logging**: All errors are logged server-side with full context

---

## Rate Limiting

Currently, xg2g does not enforce global rate limiting on API endpoints. However, the refresh endpoint has built-in concurrency protection:

- Only one refresh operation can run at a time
- Concurrent requests receive `409 Conflict`
- Circuit breaker opens after 3 consecutive failures

---

## Versioning Strategy

xg2g follows semantic versioning for API contracts:

- **Major version** (`/api/v1`, `/api/v2`): Breaking changes
- **Minor version**: Backwards-compatible additions
- **Patch version**: Backwards-compatible fixes

**Backwards Compatibility Guarantees**:

Within a major version (e.g., v1), the following are guaranteed:

✅ **Stable**:
- Response field names and types
- Response structure
- HTTP status codes for documented scenarios
- Authentication mechanisms

✅ **Safe to Add**:
- New optional request parameters
- New response fields (clients must ignore unknown fields)
- New HTTP headers

❌ **Breaking Changes** (require major version bump):
- Removing response fields
- Changing field types
- Renaming fields
- Changing authentication requirements
- Removing endpoints

---

## Client Recommendations

### 1. Use Versioned Endpoints

Always use `/api/v1/*` for new integrations:

```python
# Good
response = requests.get("http://localhost:8080/api/v1/status")

# Bad (deprecated)
response = requests.get("http://localhost:8080/api/status")
```

### 2. Check API Version Header

Validate the `X-API-Version` header to ensure compatibility:

```python
response = requests.get("http://localhost:8080/api/v1/status")
api_version = response.headers.get("X-API-Version")
assert api_version == "1", f"Unexpected API version: {api_version}"
```

### 3. Handle Unknown Fields

Be resilient to new fields being added:

```python
data = response.json()
# Only access fields you need
version = data["version"]
channels = data["channels"]
# Ignore unknown fields like data.get("newField")
```

### 4. Implement Retry Logic

Handle transient errors (409, 503) with exponential backoff:

```python
import time

for attempt in range(3):
    response = requests.post(
        "http://localhost:8080/api/v1/refresh",
        headers={"X-API-Token": token}
    )
    if response.status_code == 409:
        retry_after = int(response.headers.get("Retry-After", 30))
        time.sleep(retry_after)
        continue
    break
```

### 5. Monitor Deprecation Headers

Log warnings when receiving deprecation headers:

```python
if response.headers.get("Deprecation") == "true":
    sunset = response.headers.get("Sunset")
    successor = response.headers.get("Link")
    logging.warning(f"API deprecated, sunset: {sunset}, use: {successor}")
```

---

## Examples

### Shell (curl)

```bash
# Check status
curl -s http://localhost:8080/api/v1/status | jq .

# Trigger refresh
curl -X POST http://localhost:8080/api/v1/refresh \
  -H "X-API-Token: $XG2G_API_TOKEN" \
  | jq .
```

### Python (requests)

```python
import requests

BASE_URL = "http://localhost:8080"
API_TOKEN = "your-secret-token"

# Get status
response = requests.get(f"{BASE_URL}/api/v1/status")
data = response.json()
print(f"Channels: {data['channels']}")

# Trigger refresh
response = requests.post(
    f"{BASE_URL}/api/v1/refresh",
    headers={"X-API-Token": API_TOKEN}
)
if response.status_code == 200:
    print("Refresh successful")
elif response.status_code == 409:
    print("Refresh already in progress")
```

### Go (net/http)

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

type Status struct {
    Status   string `json:"status"`
    Version  string `json:"version"`
    Channels int    `json:"channels"`
}

func main() {
    resp, err := http.Get("http://localhost:8080/api/v1/status")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    var status Status
    if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
        panic(err)
    }

    fmt.Printf("Channels: %d\n", status.Channels)
}
```

---

## Support

- **GitHub Issues**: https://github.com/ManuGH/xg2g/issues
- **Documentation**: https://github.com/ManuGH/xg2g/docs
- **Changelog**: See CHANGELOG.md for version history

---

## Changelog

### v1.5.0 (TBD)

- ✨ Introduced versioned API endpoints (`/api/v1/*`)
- ⚠️ Deprecated unversioned `/api/*` endpoints (sunset: 2025-12-31)
- ✨ Added `X-API-Version` response header
- ✨ Added deprecation headers (RFC 8594)
- ✨ Feature flag support for API v2 preview (`XG2G_FEATURE_API_V2=true`)

### v1.4.0

- Initial stable API (unversioned)
