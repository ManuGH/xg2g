# xg2g Configuration Guide

xg2g is a **12-Factor App** designed to be configured primarily via **Environment Variables**.
For complex setups or persisted configurations, a YAML file can be used.

## Deprecations

See `docs/DEPRECATION_POLICY.md` for the policy and removal targets.

Legacy environment variable aliases were removed in `v3.0.0`. Use canonical names instead.

### Strict Readiness Checks

A new mode `ReadyStrict` has been added.

- **Default (False)**: `/readyz` returns 200 if the process is running. Recommended for most users to prevent crash loops when receiver is rebooting.
- **Strict (True)**: `/readyz` returns 503 if the upstream enigma2 receiver is unreachable.
- Enable via ENV: `XG2G_READY_STRICT=true`

### Removed Configuration Keys

The following keys were removed in PR 4 (Config Cleanup) as they had no effect:

- `XG2G_INSTANT_TUNE` - Previously parsed but had no implementation
- `XG2G_SHADOW_INTENTS` - Unused shadow canary feature
- `XG2G_SHADOW_TARGET` - Dependent on `XG2G_SHADOW_INTENTS`

If these keys are set, they will be ignored with a warning logged.

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
| `XG2G_OWI_BASE` | Base URL of your receiver (required for streaming). If empty, xg2g starts in setup mode. | `http://192.168.1.50` |
| `XG2G_DATA` | Data directory for cache/logs. | `/app/data` |
| `XG2G_API_TOKEN` | Required token for API access. | `s3cr3t` |

#### Advanced

| Variable | Description | Default |
| :--- | :--- | :--- |
| `XG2G_BOUQUET` | Bouquets to load (comma-separated). Empty = all bouquets. | empty |
| `XG2G_EPG_DAYS` | Days of EPG to fetch. | `7` |
| `XG2G_LISTEN` | API server listen address. | `:8080` |
| `XG2G_READY_STRICT` | Enable strict upstream checking. | `false` |

**Note:** The binary defaults to `:8080`; deployment configs may set `XG2G_LISTEN=:8088`.

### 2. Configuration File (YAML)

For users who prefer a file-based config:

```yaml
# version/configVersion are ignored (fixed to v3)
# version: "3.0.0"
# configVersion: "3.0.0"
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

`version` and `configVersion` are ignored; the schema is fixed to v3. Use `XG2G_CONFIG_STRICT=false` to disable strict validation if needed.

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

- **Liveness Probe** (`/healthz`): Checks if the process is up; it may return 503 during startup or degraded states.
- **Readiness Probe** (`/readyz`): Checks if ready to serve traffic.
  - If `XG2G_READY_STRICT=false` (default): Returns 200 immediately.
  - If `XG2G_READY_STRICT=true`: Pings Enigma2 receiver (timeout 2s). Returns 503 if unreachable.

## V3 API (HLS) Setup

### Overview

V3 is the production streaming backend for xg2g and exposes HLS streaming capability under the `/api/v3` namespace. It utilizes a worker-based architecture to manage FFmpeg sessions and intents for robust playback.

### Base URL and Endpoints

The V3 API is strictly mounted under the `/api/v3` base path.

**Example Endpoints:**

- `/api/v3/sessions` (Session Management)
- `/api/v3/sessions/{sessionID}/hls/{filename}` (HLS Playback)

> **Note:** This mounting path is enforced by the V3 handler factory (`v3.NewHandler`) and cannot be changed.

### LAN Guard Defaults (Local Network Only)

The V3 API is restricted to LAN clients by default via **LAN Guard**. This creates a secure-by-default posture where sensitive streaming endpoints are not exposed to the public internet.

Requests from outside the LAN (including those traversing a reverse proxy) require:

1. Correct **IP allowlist** behavior (verifying the source IP).
2. Correct **Trusted Proxy** configuration (if behind a reverse proxy).

### Trusted Proxies (Reverse Proxy / X-Forwarded-For)

If you run xg2g behind a reverse proxy (e.g., Nginx, Traefik, Cloudflare Tunnel), you **must** configure trusted proxy CIDRs. This allows the server to correctly interpret the `X-Forwarded-For` header to determine the actual client IP.

**Configuration:** `XG2G_TRUSTED_PROXIES` (Env)

- **Format:** Comma-separated CIDRs. Whitespace and empty entries are ignored.
- **Example (Docker/Localhost):** `127.0.0.1/32,172.16.0.0/12`
- **Example (Cloudflare):** Trust only your specialized connector or ingress IP, not the entire list of Cloudflare IPs if possible.

> **Warning:** If trusted proxies are not configured, external requests will appear to come from the proxy's internal IP (often allowed), defeating LAN Guard security, OR legitimate remote access will be blocked if the proxy IP isn't trusted.

### Authentication and Admin Token Expectations

V3 endpoints require a valid token **AND** specific scopes.

- **Required Scopes:**
  - `v3:write` - For creating intents and starting streams.

**Example Request:**

```bash
curl -H "Authorization: Bearer <YOUR_API_TOKEN>" http://localhost:8080/api/v3/sessions
```

### Deployment

#### Docker (Recommended)

```bash
docker run -d \
  -e XG2G_OWI_BASE=http://192.168.1.50 \
  -e XG2G_ENGINE_ENABLED=true \
  -e XG2G_API_TOKEN=secret-admin-token \
  -p 8080:8080 \
  ghcr.io/manugh/xg2g:latest
```

#### Docker Compose (.env)

```ini
XG2G_OWI_BASE=http://192.168.1.50
XG2G_ENGINE_ENABLED=true
XG2G_API_TOKEN=secret-admin-token
```

#### Bare Metal

```bash
export XG2G_OWI_BASE=http://192.168.1.50
export XG2G_ENGINE_ENABLED=true
export XG2G_API_TOKEN=secret-admin-token
./xg2g start
```

> **Note:** V3 worker is enabled by default in most deployments. Ensure it is enabled (`XG2G_ENGINE_ENABLED=true`) if you have overridden defaults.

### Verification

Perform these smoke checks to verify your V3 setup:

1. **Check Logs:** Look for "starting v3 worker..." in the startup logs.
2. **Health Check:** `curl http://localhost:8080/healthz` should return 200 OK.
3. **Session List:** `curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v3/sessions` should return a JSON array (empty `[]` is fine).
4. **Ready Check:** `curl http://localhost:8080/readyz?verbose=true` to see component status.

## Security & Rate Limiting

### Authentication

xg2g supports multiple authentication methods:

1. **Bearer Token (Recommended)**: `Authorization: Bearer <token>`
2. **Session Cookie**: `xg2g_session` cookie

### RBAC Scopes (v3)

v3 endpoints require scopes in addition to a valid token. Configure scopes with:

- `XG2G_API_TOKEN_SCOPES` for the primary token
- `XG2G_API_TOKENS` for additional scoped tokens (JSON recommended; simple format 'token=scopes' supported)

Tokens without scopes are rejected (fail-closed).

Example (JSON, recommended):

```
XG2G_API_TOKENS=[{"token":"read-token","scopes":["v3:read"]},{"token":"ops-token","scopes":["v3:read","v3:write"]}]
```

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
export XG2G_RATE_LIMIT_ENABLED=true
export XG2G_RATE_LIMIT_GLOBAL=100
export XG2G_RATE_LIMIT_AUTH=10
export XG2G_RATE_LIMIT_BURST=20
export XG2G_RATE_LIMIT_WHITELIST="192.168.1.0/24,10.0.0.0/8"
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
  - If set, `XG2G_OWI_BASE` has a scheme (`http://` or `https://`).
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
| `XG2G_STREAM_PORT` | `openWebIF.streamPort` | `8001` (Direct Mode Only) |
| `XG2G_BOUQUET` | `bouquets` | empty (all) |
| `XG2G_EPG_ENABLED` | `epg.enabled` | `true` |
| `XG2G_EPG_DAYS` | `epg.days` | `7` |
| `XG2G_API_TOKEN` | `api.token` | - |
| `XG2G_API_TOKEN_SCOPES` | `api.tokenScopes` | - |
| `XG2G_API_TOKENS` | `api.tokens` | - |
| `XG2G_READY_STRICT` | - | `false` |
| `XG2G_CONFIG_STRICT` | - | `true` |

---

## v3 Configuration

The v3 streaming backend is the **production streaming system** (enabled by default). Most settings are configured via **environment variables**. The `enigma2` block in `config.yaml` can be used to tune the v3 Enigma2 client (timeouts, retries, rate limits, auth).

| ENV Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_ENGINE_ENABLED` | `true` | Enable v3 worker/store (enabled by default in docker-compose) |
| `XG2G_ENGINE_MODE` | `standard` | Worker mode (`standard` or `virtual`) |
| `XG2G_STORE_BACKEND` | `memory` | Store backend (`memory` or `bolt`) |
| `XG2G_STORE_PATH` | `/var/lib/xg2g/v3-store` | Store path for bolt backend |
| `XG2G_HLS_ROOT` | `/var/lib/xg2g/hls` | Root for HLS segments (Recommended: `/data/hls` for Docker) |
| `XG2G_E2_HOST` | (inherits from `XG2G_OWI_BASE`) | Enigma2 Receiver URL for V3 worker (auto-inherits if not set) |
| `XG2G_E2_USER` | - | Enigma2 username for v3 worker |
| `XG2G_E2_PASS` | - | Enigma2 password for v3 worker |
| `XG2G_E2_TIMEOUT` | `10s` | Enigma2 HTTP request timeout |
| `XG2G_E2_RESPONSE_HEADER_TIMEOUT` | `10s` | Enigma2 response header timeout |
| `XG2G_E2_RETRIES` | `2` | Enigma2 request retries |
| `XG2G_E2_BACKOFF` | `200ms` | Enigma2 retry backoff |
| `XG2G_E2_MAX_BACKOFF` | `2s` | Enigma2 max retry backoff |
| `XG2G_E2_RATE_LIMIT` | `10` | Enigma2 request rate limit (req/sec) |
| `XG2G_E2_RATE_BURST` | `20` | Enigma2 request burst capacity |
| `XG2G_E2_USER_AGENT` | `xg2g-engine` | Enigma2 User-Agent |
| `XG2G_RECORDINGS_STABLE_WINDOW` | `10s` | Time to wait before considering recording stable (safeguard for Safari endlist issues) |
| `XG2G_TUNER_SLOTS` | (auto) | Tuner slots to use (CSV or Ranges, e.g., `0,1` or `0-3`) |
| `XG2G_FFMPEG_BIN` | `ffmpeg` | Path to ffmpeg binary |
| `XG2G_E2_AUTH_MODE` | `inherit` | E2 authentication mode: `inherit`, `none`, `explicit` |
| `XG2G_CONFIG_STRICT` | `true` | Enforce strict v3 config validation (override for migration) |
| `XG2G_STREAMING_POLICY` | `universal` | Streaming delivery policy (ADR-00X: only `universal` is supported) |

### Streaming Delivery Policy (ADR-00X)

xg2g uses a **single, server-defined streaming delivery policy**: `universal`.

**Guarantees**:

- **Video**: H.264 (AVC)
- **Audio**: AAC
- **Container**: fMP4
- **Delivery**: HLS
- **Compatibility**: Safari/iOS Tier-1 compliant (GOP/segment constraints enforced server-side)

**Non-Goals**:

- No HEVC/x265 by default
- No User-Agent detection
- No client-side fallbacks or auto-switching

> [!IMPORTANT]
> **Universal is the only supported delivery path.** Browser-specific playback issues are bugs in the pipeline, not a reason to introduce new policies or UI fallbacks.

**Environment Variable**:

```bash
export XG2G_STREAMING_POLICY=universal  # Default (only supported value)
```

**YAML Configuration**:

```yaml
streaming:
  delivery_policy: universal  # Only supported value
```

**Breaking Change** (v3.1+):

The old `XG2G_STREAM_PROFILE` environment variable has been **removed**. If this variable is still set, the application will **fail to start** with the following error:

```
XG2G_STREAM_PROFILE removed. Use XG2G_STREAMING_POLICY=universal (ADR-00X)
```

**Migration**:

| Old (v3.0) | New (v3.1+) | Action |
|---|---|---|
| `XG2G_STREAM_PROFILE=auto` | `XG2G_STREAMING_POLICY=universal` | Update env var |
| `XG2G_STREAM_PROFILE=safari` | `XG2G_STREAMING_POLICY=universal` | Update env var |
| `XG2G_STREAM_PROFILE=safari_hevc_hw` | `XG2G_STREAMING_POLICY=universal` | Update env var |

**E2 Auth Mode (`XG2G_E2_AUTH_MODE`):**

- `inherit` (default): If `XG2G_E2_USER`/`XG2G_E2_PASS` are empty, copy `XG2G_OWI_USER`/`XG2G_OWI_PASS` to E2 credentials.
- `none`: Enforce no E2 authentication. E2 credentials must not be set (fail-start if set).
- `explicit`: No inheritance. E2 credentials are used as explicitly set (or remain empty).

Fail-start rules: Invalid mode, `none` + E2 creds set, partial credentials (user without pass or vice versa), or `inherit` + OWI partial will cause startup failure.

Examples:

```yaml
# inherit: E2 inherits OWI credentials
openWebIF: { username: "root", password: "pw" }
enigma2: { authMode: "inherit" }

# none: E2 uses no auth
openWebIF: { username: "root", password: "pw" }
enigma2: { authMode: "none" }

# explicit: separate E2 credentials
openWebIF: { username: "root", password: "pw" }
enigma2: { authMode: "explicit", username: "e2user", password: "e2pass" }
```

Configuration is fixed to v3. Use `XG2G_CONFIG_STRICT=false` to override strict validation during migration.
When `XG2G_ENGINE_MODE=virtual` and effective tuner slots are empty, a single virtual slot `[0]` is used.
If `XG2G_TUNER_SLOTS` is invalid or empty, it is ignored with a warning and existing config is preserved.

## Removed in v3

- The legacy `/stream/*` route has been removed and now returns 404. Use the v3 HLS/session endpoints (for example, `/api/v3/sessions/{sessionID}/hls/{filename}`) or the HDHomeRun endpoints when applicable.
- Proxy environment variables (`XG2G_PROXY_*`) are no longer supported.

## Recording Playback

xg2g supports **local-first playback** for recordings stored on NAS, NFS
mounts, or local disks. This eliminates network streaming overhead and improves
playback performance when the xg2g server has direct filesystem access to
recordings.

### How It Works

1. **ServiceRef Extraction**: Enigma2 recordings use service reference format
   (`1:0:0:0:0:0:0:0:0:0:/path/to/file.ts`). xg2g automatically extracts the
   filesystem path (everything after the last colon).
2. **Path Mapping**: Configure receiver paths → local filesystem paths
3. **Existence Check**: Verify the recording file exists locally
4. **Stability Gate**: Ensure the file isn't being actively written
   (ongoing recordings)
5. **Playback Decision**: Use local file if stable, otherwise fall back to
   Receiver HTTP streaming

### Benefits

- **Zero network overhead**: Direct file access instead of HTTP streaming
  from receiver
- **Better performance**: Lower latency, no transcoding overhead for local
  playback
- **Automatic fallback**: Gracefully uses Receiver HTTP if local file
  unavailable or unstable
- **Safe for ongoing recordings**: Stability check prevents streaming files
  being written

### Configuration

#### Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `XG2G_RECORDINGS_POLICY` | `auto` | Playback policy: `auto` (local-first with fallback), `local_only`, `receiver_only` |
| `XG2G_RECORDINGS_STABLE_WINDOW` | `10s` | Duration to check file size stability (prevents streaming files being written) |
| `XG2G_RECORDINGS_MAP` | - | Path mappings: `/receiver/path=/local/path;/other=/mount` (semicolon-separated) |

#### YAML Configuration

```yaml
recording_playback:
  playback_policy: auto  # auto, local_only, receiver_only
  stable_window: 10s     # File stability check duration
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/recordings/movies
    - receiver_root: /media/net/movie
      local_root: /media/nfs-recordings
```

### Example Scenarios

#### Scenario 1: Local Disk Recordings

Receiver records to internal HDD, mounted via NFS on xg2g server:

**Environment Variables:**

```bash
XG2G_RECORDINGS_POLICY=auto
XG2G_RECORDINGS_STABLE_WINDOW=10s
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/nfs-recordings
```

**YAML:**

```yaml
recording_playback:
  playback_policy: auto
  stable_window: 10s
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/nfs-recordings
```

#### Scenario 2: Multiple Storage Locations

Receiver has multiple recording locations (HDD + NAS):

**Environment Variables:**

```bash
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/local-hdd;/media/net/movie=/mnt/nas-recordings
```

**YAML:**

```yaml
recording_playback:
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/local-hdd
    - receiver_root: /media/net/movie
      local_root: /mnt/nas-recordings
```

#### Scenario 3: Docker with Bind Mount

Receiver records to `/media/hdd/movie`, bind-mounted into container:

**docker-compose.yml:**

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    volumes:
      - /path/on/host/recordings:/recordings:ro  # Read-only bind mount
    environment:
      XG2G_RECORDINGS_MAP: /media/hdd/movie=/recordings
```

### Path Mapping Rules

- **Absolute paths required**: Both receiver and local paths must be absolute
  (start with `/`)
- **Longest prefix wins**: For overlapping paths (e.g., `/media/hdd/movie` vs
  `/media/hdd/movie2`), longest match is used
- **Security validation**: Path traversal (`..`) is blocked, paths are
  normalized
- **Case sensitive**: Path matching is case-sensitive (POSIX semantics)

### Stability Window

The `stable_window` prevents streaming files that are currently being written
(e.g., ongoing recordings).

**How it works:**

1. Check file size at T=0
2. Wait `stable_window` duration
3. Check file size again
4. If sizes match → file is stable → use local playback
5. If sizes differ → file is being written → fall back to Receiver

**Tuning guidance:**

| Use Case | Recommended Window | Rationale |
| :--- | :--- | :--- |
| Fast local disk (SSD/NVMe) | `1s` - `2s` | Minimal write delay |
| NAS/NFS with good network | `2s` - `5s` | Account for network write latency |
| Slow NAS or WAN-backed storage | `5s` - `10s` | Allow for buffering and sync delays |
| Receiver writes in large chunks | `10s+` | Wait for write bursts to complete |

**Trade-offs:**

- **Shorter window**: Faster playback start, risk of streaming unstable files
- **Longer window**: Safer stability detection, slower playback start for new
  recordings

### Fallback Behavior

Local playback will **automatically fall back to Receiver HTTP streaming**
when:

- No path mapping configured for the recording's receiver path
- Recording's receiver path is not absolute (relative paths not supported)
- Mapped local file does not exist
- Local file is unstable (size changing during stability window)

Fallback is **transparent** - no errors, no user intervention required. Debug
logs indicate when fallback occurs.

### Observability

All playback decisions are logged:

```json
{
  "level": "info",
  "recording_id": "...",
  "source_type": "local",
  "receiver_ref": "/media/hdd/movie/recording.ts",
  "source": "/mnt/recordings/recording.ts",
  "msg": "recording playback source selected"
}
```

**Fallback logs** (debug level):

```json
{
  "level": "debug",
  "local_path": "/mnt/recordings/recording.ts",
  "stable_window": "2s",
  "msg": "file unstable, falling back to receiver"
}
```

### Troubleshooting

**Problem**: Recordings always use Receiver HTTP, never local

**Solutions:**

1. Check path mapping matches receiver's exact path (case-sensitive)
2. Verify local file exists: `ls -lh /mnt/recordings/`
3. Ensure paths are absolute in both receiver and local roots
4. Check xg2g has read access to local files
5. Enable debug logging: `XG2G_LOG_LEVEL=debug`

**Problem**: Playback stutters or starts slowly

**Solutions:**

1. Reduce `stable_window` if files are on fast storage
2. Check NFS/CIFS mount performance:
   `dd if=/mnt/recordings/test.ts of=/dev/null bs=1M count=100`
3. Consider `receiver_only` policy if network to receiver is faster than local
   disk

**Problem**: Ongoing recordings fail to play

**Solutions:**

1. This is expected - unstable files fall back to Receiver (safer)
2. Increase `stable_window` if receiver writes in large bursts
3. Use Receiver HTTP for in-progress recordings (automatic fallback)

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
