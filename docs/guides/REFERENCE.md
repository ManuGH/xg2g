# xg2g Reference Guide

This document provides a technical reference for environment variables,
configuration files, and health endpoints.

> [!NOTE]
> For architectural details, see [ARCHITECTURE.md](../arch/ARCHITECTURE.md). For
> build/deploy facts, see [BUILD.md](../../BUILD.md).

## 1. Environment Variables

Precedence: **Environment Variables** > **Configuration File** > **Defaults**.

### Core Configuration

| Variable | Description | Example/Default |
| :--- | :--- | :--- |
| `XG2G_OWI_BASE` | Base URL of receiver (required) | `http://192.168.1.50` |
| `XG2G_DATA` | Data directory for cache/logs | `/tmp` |
| `XG2G_LOG_LEVEL` | Logging verbosity (debug, info, warn, error) | `info` |
| `XG2G_API_TOKEN` | Primary admin bearer token | - |
| `XG2G_API_TOKEN_SCOPES`| Scopes for primary token (CSV) | `v3:read,v3:write` |
| `XG2G_API_TOKENS` | Multi-token JSON list | `[{"token":"...","scopes":...}]` |

### V3 Streaming Engine

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_ENGINE_ENABLED` | `true` | Enable V3 worker/orchestrator |
| `XG2G_ENGINE_MODE` | `standard` | Mode: `standard` or `virtual` |
| `XG2G_STORE_BACKEND` | `memory` | Cache: `memory` or `sqlite` |
| `XG2G_HLS_ROOT` | `/var/lib/xg2g/hls` | Directory for HLS segments |
| `XG2G_STREAMING_POLICY`| `universal` | Only `universal` supported (ADR-00X) |
| `XG2G_TUNER_SLOTS` | (auto) | Range (e.g., `0-3`) or CSV (`0,1,2`) |

### Enigma2 Connectivity (V3)

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_E2_HOST` | (base) | Direct IP/URL for worker connectivity |
| `XG2G_E2_AUTH_MODE` | `inherit` | `inherit`, `none`, or `explicit` |
| `XG2G_E2_USER` | - | Enigma2 username |
| `XG2G_E2_PASS` | - | Enigma2 password |
| `XG2G_E2_TIMEOUT` | `10s` | HTTP timeout |
| `XG2G_E2_RETRIES` | `2` | Number of retry attempts |

### Feature Flags & Safety

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_READY_STRICT` | `false` | Enable upstream connectivity check |
| `XG2G_CONFIG_STRICT` | `true` | Fail startup on unknown YAML keys |
| `XG2G_TRUSTED_PROXIES`| - | CSV of CIDRs for `X-Forwarded-For` trust |
| `XG2G_RECORDINGS_STABLE_WINDOW` | `10s` | File stability wait duration |

## 2. Configuration File (YAML)

Default location: `config.yaml`. Strict validation is enabled by default.

```yaml
openWebIF:
  baseUrl: "http://192.168.1.50"
  streamPort: 8001
enigma2:
  authMode: "inherit"
epg:
  enabled: true
  days: 7
recording_playback:
  playback_policy: auto
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/recordings
```

## 3. Health Endpoints

Endpoints return JSON and do not require authentication.

| Endpoint | Method | Success | Failure | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| `/healthz` | GET | `200` | - | Liveness: Process is alive |
| `/readyz` | GET | `200` | `503` | Readiness: Dependencies available |

> [!TIP]
> Use `/readyz?verbose=true` for detailed component status.
