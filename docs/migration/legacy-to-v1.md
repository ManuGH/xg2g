# Migration Guide: Legacy Root Endpoints → API v1

**Status:** Active
**Deprecation Date:** 2025-09-15
**Sunset Date:** 2026-03-15 (6 months)

## Overview

xg2g is introducing versioned API endpoints under the `/api/v1/` prefix to provide better stability guarantees and prepare for future enhancements. Legacy root-level endpoints (`/lineup.json`, `/discover.json`) are being deprecated in favor of versioned paths.

## What's Changing

### Affected Endpoints

| Legacy Path (Deprecated) | New Path (Recommended) | Status |
|--------------------------|------------------------|--------|
| `/lineup.json` | `/api/v1/lineup.json` | 308 Redirect |
| `/discover.json` | `/api/v1/discover.json` | 308 Redirect |
| `/lineup_status.json` | `/api/v1/lineup_status.json` | 308 Redirect |

### Stream Endpoints (No Change)

These endpoints remain at root level for HDHomeRun compatibility:
- `/auto/<channel>` - Smart stream detection
- `/proxy/<channel>` - Direct proxy mode
- `/transcode/<channel>` - Forced transcoding

## Migration Steps

### Option A: Follow Redirects (Easiest)

Most HTTP clients automatically follow `308 Permanent Redirect` responses. No code changes required.

**Verification:**
```bash
curl -L http://your-xg2g-server/lineup.json
# -L flag follows redirects automatically
```

**Client Support:**
- ✅ curl with `-L` flag
- ✅ Python `requests` library (default behavior)
- ✅ Go `http.Client` (default behavior)
- ✅ JavaScript `fetch()` (default behavior)
- ⚠️ Some legacy clients may need manual URL updates

### Option B: Update URLs Explicitly (Recommended)

Update your client code to use the new `/api/v1/` prefix directly.

**Before:**
```python
# Python example
import requests
response = requests.get("http://192.168.1.100:8080/lineup.json")
```

**After:**
```python
import requests
response = requests.get("http://192.168.1.100:8080/api/v1/lineup.json")
```

**Benefits:**
- Eliminates redirect overhead (saves ~10ms per request)
- Avoids deprecation warnings in logs
- Future-proof for when redirects are removed

## Client Examples

### cURL

```bash
# Legacy (works via redirect)
curl http://192.168.1.100:8080/lineup.json

# Recommended (direct)
curl http://192.168.1.100:8080/api/v1/lineup.json
```

### Python

```python
import requests

# Recommended
BASE_URL = "http://192.168.1.100:8080/api/v1"

def get_lineup():
    response = requests.get(f"{BASE_URL}/lineup.json")
    response.raise_for_status()
    return response.json()

def get_device_info():
    response = requests.get(f"{BASE_URL}/discover.json")
    response.raise_for_status()
    return response.json()
```

### Go

```go
package main

import (
    "encoding/json"
    "net/http"
)

const baseURL = "http://192.168.1.100:8080/api/v1"

func getLineup() ([]LineupEntry, error) {
    resp, err := http.Get(baseURL + "/lineup.json")
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var lineup []LineupEntry
    if err := json.NewDecoder(resp.Body).Decode(&lineup); err != nil {
        return nil, err
    }
    return lineup, nil
}
```

### JavaScript (Browser)

```javascript
// Recommended
const BASE_URL = 'http://192.168.1.100:8080/api/v1';

async function getLineup() {
    const response = await fetch(`${BASE_URL}/lineup.json`);
    if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
    }
    return response.json();
}

async function getDeviceInfo() {
    const response = await fetch(`${BASE_URL}/discover.json`);
    if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
    }
    return response.json();
}
```

## Response Format (Unchanged)

The response schemas remain **100% compatible**. Only the URL path changes.

**Example: `/api/v1/lineup.json`**
```json
[
  {
    "GuideNumber": "1",
    "GuideName": "Das Erste HD",
    "URL": "http://192.168.1.100:8080/auto/1:0:19:283D:3FB:1:C00000:0:0:0:"
  },
  {
    "GuideNumber": "2",
    "GuideName": "ZDF HD",
    "URL": "http://192.168.1.100:8080/auto/1:0:19:2B66:3F3:1:C00000:0:0:0:"
  }
]
```

## Testing Your Migration

### 1. Check for Deprecation Headers

```bash
curl -v http://192.168.1.100:8080/lineup.json 2>&1 | grep -E "(Deprecation|Sunset|Location)"
```

**Expected Output:**
```
< HTTP/1.1 308 Permanent Redirect
< Location: /api/v1/lineup.json
< Deprecation: true
< Sunset: Wed, 15 Mar 2026 00:00:00 GMT
```

### 2. Verify Redirect Works

```bash
curl -L http://192.168.1.100:8080/lineup.json -o /tmp/legacy.json
curl http://192.168.1.100:8080/api/v1/lineup.json -o /tmp/v1.json
diff /tmp/legacy.json /tmp/v1.json
# Should show no differences (exit code 0)
```

### 3. Update and Compare Response Times

```bash
# Measure redirect overhead
time curl -L http://192.168.1.100:8080/lineup.json > /dev/null

# Measure direct request
time curl http://192.168.1.100:8080/api/v1/lineup.json > /dev/null
```

**Expected:** Direct v1 request should be ~10ms faster.

## HDHomeRun Client Compatibility

If you're using HDHomeRun-compatible clients (Plex, Emby, Channels DVR):

1. **No immediate action required** - Clients follow redirects automatically
2. **Update base URL when convenient:**
   - Old: `http://192.168.1.100:8080`
   - New: `http://192.168.1.100:8080` (root stays same)
   - Discovery endpoints redirect transparently

3. **Stream URLs unchanged:**
   - `/auto/`, `/proxy/`, `/transcode/` remain at root level
   - HDHomeRun compatibility maintained

## Timeline

| Date       | Event | Details |
|------------|-------|---------|
| 2025-09-15 | v1 Launch | `/api/v1/` endpoints active, legacy redirects enabled |
| 2025-10-01 | Deprecation Notice | Sunset header added, 6-month warning |
| 2026-01-15 | Migration Review | Check usage metrics, extend sunset if needed |
| 2026-02-15 | Final Warning | Email notifications to known clients |
| 2026-03-15 | Sunset | Redirects removed, legacy paths return `410 Gone` |

## After Sunset (2026-03-15)

Legacy endpoints will return:

```http
HTTP/1.1 410 Gone
Content-Type: application/json

{
  "error": "This endpoint has been sunset",
  "sunset_date": "2026-03-15T00:00:00Z",
  "replacement": "/api/v1/lineup.json",
  "migration_guide": "https://docs.xg2g.io/migration/legacy-to-v1"
}
```

## Monitoring Deprecation Headers

Add logging to detect when your client hits deprecated endpoints:

**Python Example:**
```python
import requests

response = requests.get("http://192.168.1.100:8080/lineup.json")

# Check for deprecation
if 'Deprecation' in response.headers:
    sunset = response.headers.get('Sunset', 'unknown')
    print(f"WARNING: This endpoint is deprecated. Sunset: {sunset}")
    print(f"Update URL to: {response.headers.get('Location')}")
```

**Go Example:**
```go
resp, _ := http.Get("http://192.168.1.100:8080/lineup.json")
defer resp.Body.Close()

if resp.Header.Get("Deprecation") == "true" {
    log.Printf("WARNING: Deprecated endpoint. Sunset: %s",
        resp.Header.Get("Sunset"))
    log.Printf("Migrate to: %s", resp.Header.Get("Location"))
}
```

## FAQ

### Q: Why are you making this change?

**A:** Versioned APIs provide:
- Clear compatibility guarantees
- Ability to introduce breaking changes in v2 without disrupting existing clients
- Better alignment with REST best practices
- Easier monitoring and deprecation management

### Q: Will this break my existing setup?

**A:** No. The server automatically redirects legacy endpoints to v1. Most clients follow redirects transparently.

### Q: Do I need to change my HDHomeRun client configuration?

**A:** No. HDHomeRun clients will continue working via automatic redirects. Stream URLs (`/auto/`, `/proxy/`) remain unchanged.

### Q: What happens if I don't migrate by the sunset date?

**A:** Requests to legacy endpoints will fail with `410 Gone`. Update your client to use `/api/v1/` before 2026-03-15.

### Q: Can the sunset date be extended?

**A:** Yes. If significant traffic still uses legacy endpoints by 2026-01-15, we may extend the sunset window by 3 months.

### Q: Will there be a v2 API?

**A:** Possibly. If we introduce breaking changes (e.g., different response schemas), they will be released as `/api/v2/` while v1 remains supported during a transition period.

## Support

If you encounter migration issues:

1. **Check the logs:** Enable debug logging with `XG2G_LOG_LEVEL=debug`
2. **Test redirects:** Use `curl -v` to inspect response headers
3. **File an issue:** https://github.com/ManuGH/xg2g/issues
4. **Community:** Discuss in GitHub Discussions

## Related Documentation

- [API Versioning Policy](../architecture/api-versioning-policy.md)
- [API Versioning Implementation Guide](../architecture/api-versioning-implementation.md)
- [Configuration Reference](../guides/configuration.md)
