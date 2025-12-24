# xg2g Configuration Guide (v2.1+; current: v2.1.0)

xg2g is a **12-Factor App** designed to be configured primarily via **Environment Variables**.
For complex setups or persisted configurations, a YAML file can be used.

> [!IMPORTANT]
> **v2.1 Update: Strict Validation**
> xg2g now enforces **Strict Configuration Parsing**. Unknown keys in `config.yaml` will cause startup failure.
> Legacy keys (e.g. `receiver`, `xmltv`) are **no longer supported**. See the Migration Guide below.

## Migration from v2.0

If you are upgrading from v2.0 and using `config.yaml`, you must update your configuration keys.
Environment variables remain partially backward compatible (aliases are provided for common variables like `RECEIVER_IP`), but `config.yaml` usage is strict.

*Note: Legacy environment variable aliases are still accepted, but they are deprecated, emit startup warnings, and will be removed in v2.2.*

### Removed / Renamed Keys

| Legacy Key (v2.0) | New Key (v2.1+) | Notes |
| :--- | :--- | :--- |
| `receiver` | `openWebIF` | Complete section rename |
| `xmltv` | `epg.xmltvPath` | Moved under `epg` section |
| `tuner.requests` | `epg.maxConcurrency` | Renamed for clarity |
| `epg.fuzzy` | `epg.fuzzyMax` | Renamed |
| `api.token` | `api.token` | (Unchanged) |

### Strict Readiness Checks

A new mode `ReadyStrict` has been added.

- **Default (False)**: `/readyz` returns 200 if the process is running. Recommended for most users to prevent crash loops when receiver is rebooting.
- **Strict (True)**: `/readyz` returns 503 if the upstream enigma2 receiver is unreachable.
- Enable via ENV: `XG2G_READY_STRICT=true`

---

## Configuration Methods

Precedence order (highest to lowest):

1. **Environment Variables** (e.g., `XG2G_OWI_BASE`)
2. **Configuration File** (YAML)
3. **Defaults**

### 1. Environment Variables (Recommended)

The simplest way to run xg2g is with a few environment variables.

#### Core

| Variable | Description | Example |
| :--- | :--- | :--- |
| `XG2G_OWI_BASE` | **Required**. Base URL of your receiver. | `http://192.168.1.50` |
| `XG2G_DATA` | Data directory for cache/logs. | `/app/data` |
| `XG2G_API_TOKEN` | Secure token for admin API. | `s3cr3t` |

#### Advanced

| Variable | Description | Default |
| :--- | :--- | :--- |
| `XG2G_BOUQUET` | Bouquets to load (comma-separated). | `Premium` |
| `XG2G_EPG_DAYS` | Days of EPG to fetch. | `7` |
| `XG2G_READY_STRICT` | Enable strict upstream checking. | `false` |

### 2. Configuration File (YAML)

For users who prefer a file-based config:

```yaml
version: "2.1"
openWebIF:
  baseUrl: "http://192.168.1.50"
  streamPort: 8001
epg:
  enabled: true
  days: 7
```

Load it with:

```bash
./xg2g-daemon --config /path/to/my-config.yaml
```

## Readiness Probes

If running in Kubernetes or Docker Compose with healthchecks:

- **Liveness Probe** (`/healthz`): Checks if process is not deadlocked. Always 200 OK.
- **Readiness Probe** (`/readyz`): Checks if ready to serve traffic.
  - If `XG2G_READY_STRICT=false` (default): Returns 200 immediately.
  - If `XG2G_READY_STRICT=true`: Pings Enigma2 receiver (timeout 2s). Returns 503 if unreachable.

## Security & Rate Limiting

### Authentication

xg2g supports multiple authentication methods:

1. **Bearer Token (Recommended)**: `Authorization: Bearer <token>`
2. **Session Cookie**: `xg2g_session` cookie
3. **Query Parameter (Deprecated)**: `?token=...` - **Disabled by default** due to security risks

**Security Warning**: Query parameter authentication logs tokens in:

- Proxy server access logs
- Browser history
- HTTP Referer headers

**Migration**: Use `Authorization` header instead. To temporarily re-enable (not recommended):

```bash
export XG2G_ALLOW_QUERY_TOKENS=true  # Will be removed in v3.0
```

### Rate Limiting

Protect your API from abuse with built-in rate limiting:

```yaml
# config.yaml
api:
  rateLimit:
    enabled: true
    global: 100          # Requests per second (all endpoints)
    auth: 10             # Requests per minute (auth-required endpoints)
    burst: 20            # Burst capacity
    whitelist:           # IPs/CIDRs exempt from rate limiting
      - 192.168.1.0/24   # Local network
      - 10.0.0.0/8       # Private network
      - 172.20.0.5       # Specific trusted IP
```

**Environment Variables**:

```bash
export XG2G_RATELIMIT_ENABLED=true
export XG2G_RATELIMIT_GLOBAL=100
export XG2G_RATELIMIT_AUTH=10
export XG2G_RATELIMIT_BURST=20
export XG2G_RATELIMIT_WHITELIST="192.168.1.0/24,10.0.0.0/8"
```

**CIDR Notation Support**:

- **Single IP**: `192.168.1.100`
- **IPv4 Subnet**: `192.168.0.0/16`, `10.0.0.0/8`
- **IPv6 Subnet**: `2001:db8::/32`

Example whitelist configuration:

```yaml
api:
  rateLimit:
    whitelist:
      - 127.0.0.1           # Localhost
      - 192.168.1.0/24      # Home network
      - 10.0.0.0/8          # Corporate VPN
      - 2001:db8::/48       # IPv6 subnet
```

## Troubleshooting

**"field ... not found in type config.FileConfig"**

- You have legacy keys in your YAML file. Convert them to the new schema.
- Check the logs for specific error messages (e.g. `check_config_failed`).

**"startup checks failed"**

- Validations are now stricter. Ensure:
  - `XG2G_OWI_BASE` has a scheme (`http://` or `https://`).
  - TLS cert/key pair is complete if enabled.
  - Recording roots are absolute paths.

---

## Complete Reference (Environment Variables)

| ENV Variable | Config Key | Default |
| :--- | :--- | :--- |
| `XG2G_OWI_BASE` | `openWebIF.baseUrl` | - |
| `XG2G_OWI_USER` | `openWebIF.username` | - |
| `XG2G_OWI_PASS` | `openWebIF.password` | - |
| `XG2G_DATA` | `dataDir` | `/tmp` |
| `XG2G_LOG_LEVEL` | `logLevel` | `info` |
| `XG2G_STREAM_PORT` | `openWebIF.streamPort` | `8001` |
| `XG2G_BOUQUET` | `bouquets` | `Premium` |
| `XG2G_EPG_ENABLED` | `epg.enabled` | `true` |
| `XG2G_EPG_DAYS` | `epg.days` | `7` |
| `XG2G_API_TOKEN` | `api.token` | - |
| `XG2G_READY_STRICT` | - | `false` |

---

## v3 (Experimental/Preview)

The v3 control plane is experimental/preview and exposed under `/api/v3/*`. It is configured via **environment variables only** (no `config.yaml` support yet).

| ENV Variable | Purpose |
| :--- | :--- |
| `XG2G_V3_WORKER_ENABLED` | Enable v3 worker/store |
| `XG2G_V3_WORKER_MODE` | Worker mode (`standard` or `virtual`) |
| `XG2G_V3_STORE_BACKEND` | Store backend (`memory` or `bolt`) |
| `XG2G_V3_STORE_PATH` | Store path for bolt backend |
| `XG2G_V3_HLS_ROOT` | HLS output root for v3 sessions (Default: `/var/lib/xg2g/v3-hls`) |
| `XG2G_V3_E2_HOST` | Enigma2 Receiver URL for Worker (Default: `http://localhost`) |
| `XG2G_V3_TUNER_SLOTS` | Tuner slots to use (JSON array `[0,1]`) |
| `XG2G_V3_FFMPEG_BIN` | Path to ffmpeg binary (Default: `ffmpeg`) |
