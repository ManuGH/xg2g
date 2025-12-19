# xg2g Configuration Guide (v2.1+)

xg2g is a **12-Factor App** designed to be configured primarily via **Environment Variables**.
For complex setups or persisted configurations, a YAML file can be used.

> [!IMPORTANT]
> **v2.1 Update: Strict Validation**
> xg2g now enforces **Strict Configuration Parsing**. Unknown keys in `config.yaml` will cause startup failure.
> Legacy keys (e.g. `receiver`, `xmltv`) are **no longer supported**. See the Migration Guide below.

## Migration from v2.0

If you are upgrading from v2.0 and using `config.yaml`, you must update your configuration keys.
Environment variables remain partially backward compatible (aliases are provided for common variables like `RECEIVER_IP`), but `config.yaml` usage is strict.

*Note: While legacy environment variables are currently accepted, they are deprecated and will be removed in v3.0.*

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
