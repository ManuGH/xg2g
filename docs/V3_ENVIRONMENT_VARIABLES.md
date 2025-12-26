# V3 Worker Environment Variables Reference

This document lists all environment variables for the xg2g V3 Worker/Control Plane.

## Core V3 Settings

### `XG2G_V3_WORKER_ENABLED`
- **Type:** Boolean
- **Default:** `true`
- **Description:** Enable the V3 worker/orchestrator for HLS streaming
- **Example:** `XG2G_V3_WORKER_ENABLED=true`

### `XG2G_V3_WORKER_MODE`
- **Type:** String
- **Default:** `standard`
- **Options:**
  - `standard` - Production mode with real hardware
  - `virtual` - Testing/development mode with mocked tuners
- **Description:** Worker operation mode
- **Example:** `XG2G_V3_WORKER_MODE=standard`

### `XG2G_V3_CONFIG_STRICT`
- **Type:** Boolean
- **Default:** `false`
- **Description:** Enable strict configuration validation (requires `ConfigVersion` field)
- **Example:** `XG2G_V3_CONFIG_STRICT=false`

## State Store Configuration

### `XG2G_V3_STORE_BACKEND`
- **Type:** String
- **Default:** `memory`
- **Options:**
  - `memory` - In-memory store (data lost on restart)
  - `bolt` - BoltDB persistent store
  - `badger` - BadgerDB persistent store
- **Description:** State store backend type
- **Example:** `XG2G_V3_STORE_BACKEND=memory`

### `XG2G_V3_STORE_PATH`
- **Type:** String
- **Default:** `/var/lib/xg2g/v3-store`
- **Description:** Directory path for persistent store (bolt/badger only)
- **Example:** `XG2G_V3_STORE_PATH=/data/v3-store`

## Tuner Configuration

### `XG2G_V3_TUNER_SLOTS`
- **Type:** Comma-separated integers
- **Default:** Empty (auto-detect or virtual mode default `0`)
- **Description:** Available tuner slot IDs for the worker
- **Examples:**
  - `XG2G_V3_TUNER_SLOTS=0,1,2` - Use 3 tuners (slots 0, 1, 2)
  - `XG2G_V3_TUNER_SLOTS=0` - Use single tuner (slot 0)
  - `XG2G_V3_TUNER_SLOTS=` - Auto-detect (standard mode) or default to `0` (virtual mode)

### `XG2G_V3_E2_HOST`
- **Type:** URL
- **Default:** Inherits from `XG2G_OWI_BASE` if empty
- **Description:** Enigma2 receiver host URL for tuning operations
- **Example:** `XG2G_V3_E2_HOST=http://10.10.55.64`

### `XG2G_V3_TUNE_TIMEOUT`
- **Type:** Duration
- **Default:** `10s`
- **Description:** Maximum time to wait for tuner to lock signal
- **Example:** `XG2G_V3_TUNE_TIMEOUT=15s`

## FFmpeg Configuration

### `XG2G_V3_FFMPEG_BIN`
- **Type:** String
- **Default:** `ffmpeg`
- **Description:** Path to FFmpeg binary
- **Example:** `XG2G_V3_FFMPEG_BIN=/usr/local/bin/ffmpeg`

### `XG2G_V3_FFMPEG_KILL_TIMEOUT`
- **Type:** Duration
- **Default:** `5s`
- **Description:** Graceful shutdown timeout for FFmpeg processes
- **Example:** `XG2G_V3_FFMPEG_KILL_TIMEOUT=10s`

### `XG2G_V3_HLS_ROOT`
- **Type:** String
- **Default:** `/var/lib/xg2g/v3-hls`
- **Description:** Root directory for HLS output (playlists and segments)
- **Example:** `XG2G_V3_HLS_ROOT=/data/stream/encoded`

### `XG2G_V3_IDLE_TIMEOUT`
- **Type:** Duration
- **Default:** `2m`
- **Description:** Stop idle V3 sessions after no HLS playlist requests (0 disables)
- **Example:** `XG2G_V3_IDLE_TIMEOUT=90s`

## Shadow Testing (Advanced)

### `XG2G_V3_SHADOW_INTENTS`
- **Type:** Boolean
- **Default:** `false`
- **Description:** Enable shadow mode to mirror intents to another instance
- **Example:** `XG2G_V3_SHADOW_INTENTS=true`

### `XG2G_V3_SHADOW_TARGET`
- **Type:** URL
- **Default:** Empty
- **Description:** Target URL for shadow intent forwarding
- **Example:** `XG2G_V3_SHADOW_TARGET=http://backup-server:8080`

## Complete Example Configuration

### Docker Compose

```yaml
environment:
  # V3 Worker Configuration
  - XG2G_V3_WORKER_ENABLED=true
  - XG2G_V3_WORKER_MODE=standard
  - XG2G_V3_CONFIG_STRICT=false

  # State Store
  - XG2G_V3_STORE_BACKEND=bolt
  - XG2G_V3_STORE_PATH=/data/v3-store

  # Tuner Setup
  - XG2G_V3_TUNER_SLOTS=0,1,2,3
  - XG2G_V3_E2_HOST=http://10.10.55.64
  - XG2G_V3_TUNE_TIMEOUT=10s

  # FFmpeg
  - XG2G_V3_FFMPEG_BIN=ffmpeg
  - XG2G_V3_FFMPEG_KILL_TIMEOUT=5s
  - XG2G_V3_HLS_ROOT=/data/stream/encoded

  # Shadow Testing (optional)
  - XG2G_V3_SHADOW_INTENTS=false
  - XG2G_V3_SHADOW_TARGET=
```

### Shell Export

```bash
export XG2G_V3_WORKER_ENABLED=true
export XG2G_V3_WORKER_MODE=standard
export XG2G_V3_TUNER_SLOTS=0,1
export XG2G_V3_E2_HOST=http://192.168.1.100
export XG2G_V3_HLS_ROOT=/var/lib/xg2g/v3-hls
```

## Related Documentation

- [Build & Deployment Guide](../BUILD.md)
- [V3 Architecture](../internal/v3/doc.go)
- [Configuration Schema](../internal/config/schema.json)

## Troubleshooting

### Worker Not Starting
- Check `XG2G_V3_WORKER_ENABLED=true`
- Verify `XG2G_V3_E2_HOST` is reachable
- Ensure `XG2G_V3_HLS_ROOT` directory exists and is writable

### Tuner Not Found
- Set `XG2G_V3_TUNER_SLOTS` explicitly (e.g., `0,1`)
- Verify Enigma2 receiver is accessible at `XG2G_V3_E2_HOST`
- Check logs for tuner lease acquisition errors

### Playlist 404 Errors
- **Fixed in latest version!** The orchestrator now waits for the playlist file to exist before marking session as READY
- Verify `XG2G_V3_HLS_ROOT` is mounted correctly
- Check FFmpeg logs for encoding errors
- Ensure sufficient disk space in HLS root directory

### State Loss on Restart
- Use `XG2G_V3_STORE_BACKEND=bolt` for persistence
- Ensure `XG2G_V3_STORE_PATH` is mounted to a persistent volume
- Check directory permissions for store path
