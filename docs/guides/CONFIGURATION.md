# xg2g Configuration Guide

xg2g is a **12-Factor App** designed to be configured primarily via **Environment Variables**.
For complex setups or persisted configurations, a YAML file can be used.

## Deprecations

See `docs/DEPRECATION_POLICY.md` for the policy and removal targets.

Legacy environment variable aliases are deprecated and will be removed in `v3.0.0`. Use canonical names instead.

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
configVersion: "3.0.0" # required when XG2G_V3_CONFIG_STRICT=true
openWebIF:
  baseUrl: "http://192.168.1.50"
  streamPort: 8001
enigma2:
  # Optional v3 worker overrides (defaults to openWebIF.baseUrl)
  # baseUrl: "http://192.168.1.50"
  # username: "root"
  # password: "password"
  # timeout: "10s"
epg:
  enabled: true
  days: 7
```

`configVersion` is used for v3 strict validation. `version` remains for schema compatibility.

Load it with:

```bash
./xg2g-daemon --config /path/to/my-config.yaml
```

Migrate a config file (scaffolding):

```bash
./xg2g-daemon config migrate --file /path/to/my-config.yaml --to 3.0.0 --write
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

### RBAC Scopes (v3)

v3 endpoints require scopes in addition to a valid token. Configure scopes with:

- `XG2G_API_TOKEN_SCOPES` for the primary token
- `XG2G_API_TOKENS` for additional scoped tokens

See `docs/guides/RBAC.md` for scope definitions and endpoint mappings.

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
| `XG2G_API_TOKEN_SCOPES` | `api.tokenScopes` | - |
| `XG2G_API_TOKENS` | `api.tokens` | - |
| `XG2G_READY_STRICT` | - | `false` |
| `XG2G_CONFIG_VERSION` | `configVersion` | - |
| `XG2G_V3_CONFIG_STRICT` | - | `false` |

---

## v3 Configuration

The v3 streaming backend is the **production streaming system** (enabled by default). Most settings are configured via **environment variables**. The `enigma2` block in `config.yaml` can be used to tune the v3 Enigma2 client (timeouts, retries, rate limits, auth).

| ENV Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_V3_WORKER_ENABLED` | `true` | Enable v3 worker/store (enabled by default in docker-compose) |
| `XG2G_V3_WORKER_MODE` | `standard` | Worker mode (`standard` or `virtual`) |
| `XG2G_V3_STORE_BACKEND` | `memory` | Store backend (`memory` or `bolt`) |
| `XG2G_V3_STORE_PATH` | `/var/lib/xg2g/v3-store` | Store path for bolt backend |
| `XG2G_V3_HLS_ROOT` | `/var/lib/xg2g/v3-hls` | HLS output root for v3 sessions |
| `XG2G_V3_E2_HOST` | (inherits from `XG2G_OWI_BASE`) | Enigma2 Receiver URL for V3 worker (auto-inherits if not set) |
| `XG2G_V3_E2_USER` | - | Enigma2 username for v3 worker |
| `XG2G_V3_E2_PASS` | - | Enigma2 password for v3 worker |
| `XG2G_V3_E2_TIMEOUT` | `10s` | Enigma2 HTTP request timeout |
| `XG2G_V3_E2_RESPONSE_HEADER_TIMEOUT` | `10s` | Enigma2 response header timeout |
| `XG2G_V3_E2_RETRIES` | `2` | Enigma2 request retries |
| `XG2G_V3_E2_BACKOFF` | `200ms` | Enigma2 retry backoff |
| `XG2G_V3_E2_MAX_BACKOFF` | `2s` | Enigma2 max retry backoff |
| `XG2G_V3_E2_RATE_LIMIT` | `10` | Enigma2 request rate limit (req/sec) |
| `XG2G_V3_E2_RATE_BURST` | `20` | Enigma2 request burst capacity |
| `XG2G_V3_E2_USER_AGENT` | `xg2g-v3` | Enigma2 User-Agent |
| `XG2G_V3_TUNER_SLOTS` | (auto) | Tuner slots to use (JSON array, e.g., `[0,1]`) |
| `XG2G_V3_FFMPEG_BIN` | `ffmpeg` | Path to ffmpeg binary |
| `XG2G_CONFIG_VERSION` | `3.0.0` | Config schema version for v3 strict validation |
| `XG2G_V3_CONFIG_STRICT` | `true` | Enforce strict v3 config validation (override for migration) |

`configVersion` defaults to `3.0.0`, so v3 strict validation is enabled by default. Use `XG2G_V3_CONFIG_STRICT=false` to override during migration.

## History

### v2.1 Strict Validation

xg2g enforces strict configuration parsing. Unknown keys in `config.yaml` cause startup failure.
Legacy keys (e.g., `receiver`, `xmltv`) are no longer supported.

### Migration from v2.0

If you are upgrading from v2.0 and using `config.yaml`, you must update your configuration keys.
Environment variable aliases were removed in v3.0.0; use canonical names only.

Removed / renamed keys:

| Legacy Key (v2.0) | New Key (v2.1+) | Notes |
| :--- | :--- | :--- |
| `receiver` | `openWebIF` | Complete section rename |
| `xmltv` | `epg.xmltvPath` | Moved under `epg` section |
| `tuner.requests` | `epg.maxConcurrency` | Renamed for clarity |
| `epg.fuzzy` | `epg.fuzzyMax` | Renamed |
| `api.token` | `api.token` | (Unchanged) |
