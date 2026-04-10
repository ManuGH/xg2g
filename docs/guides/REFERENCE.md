# xg2g Reference Guide

This document provides a technical reference for environment variables,
configuration files, and health endpoints.

> [!NOTE]
> For architectural details, see [ARCHITECTURE.md](../arch/ARCHITECTURE.md). For
> build/deploy facts, see [DEPLOYMENT.md](../ops/DEPLOYMENT.md).

## 1. Environment Variables

Precedence: **Environment Variables** > **Configuration File** > **Defaults**.

### Core Configuration

| Variable | Description | Example/Default |
| :--- | :--- | :--- |
| `XG2G_E2_HOST` | Base URL of receiver (required) | `http://192.168.1.50` |
| `XG2G_DATA` | Data directory for cache/logs | `/tmp` |
| `XG2G_LOG_LEVEL` | Logging verbosity (debug, info, warn, error) | `info` |
| `XG2G_API_TOKEN` | Primary admin bearer token | - |
| `XG2G_API_TOKEN_SCOPES`| Scopes for primary token (CSV) | `v3:read,v3:write` |
| `XG2G_API_TOKENS` | Multi-token JSON list | `[{"token":"...","scopes":...}]` |
| `XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES` | Disable legacy `X-API-Token` header/cookie auth vectors | `false` |

### V3 Streaming Engine

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_ENGINE_ENABLED` | `true` | Enable V3 worker/orchestrator |
| `XG2G_ENGINE_MODE` | `standard` | Mode: `standard` or `virtual` |
| `XG2G_STORE_BACKEND` | `sqlite` | State store: `sqlite` or `memory` |
| `XG2G_STORE_PATH` | `/var/lib/xg2g/store` | Store path (sqlite) |
| `XG2G_HLS_ROOT` | `${XG2G_DATA}/hls` | Directory for HLS segments |
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
| `XG2G_E2_BACKOFF` | `200ms` | Initial retry backoff |
| `XG2G_E2_MAX_BACKOFF` | `30s` | Max retry backoff duration (canonical) |
| `XG2G_E2_STREAM_PORT` | `8001` | Deprecated direct stream port override (canonical) |
| `XG2G_E2_USE_WEBIF_STREAMS` | `true` | Prefer `/web/stream.m3u` URL path |
| `XG2G_E2_RESPONSE_HEADER_TIMEOUT` | `10s` | HTTP response header timeout |
| `XG2G_E2_TUNE_TIMEOUT` | `10s` | Tune timeout before fallback/error |
| `XG2G_E2_AUTH_MODE` | `inherit` | Auth behavior (`inherit|none|explicit`) |
| `XG2G_E2_RATE_LIMIT` | - | Optional per-session rate limit |
| `XG2G_E2_RATE_BURST` | - | Optional burst for rate limiting |
| `XG2G_E2_USER_AGENT` | - | Optional User-Agent override |
| `XG2G_E2_ANALYZE_DURATION` | `2000000` | FFmpeg analyze duration |
| `XG2G_E2_PROBE_SIZE` | `5M` | FFmpeg probe size |
| `XG2G_E2_FALLBACK_TO_8001` | `true` | Fallback to legacy port 8001 |
| `XG2G_E2_PREFLIGHT_TIMEOUT` | `10s` | TS preflight timeout |

Legacy receiver env aliases are no longer accepted at startup. Use the canonical `XG2G_E2_*` surface only.

### Feature Flags & Safety

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_READY_STRICT` | `false` | Enable upstream connectivity check |
| `XG2G_CONFIG_STRICT` | `true` | Fail startup on unknown YAML keys |
| `XG2G_TRUSTED_PROXIES`| - | CSV of CIDRs for `X-Forwarded-For` trust |
| `XG2G_RECORDINGS_STABLE_WINDOW` | `10s` | File stability wait duration |

### Published Endpoint Truth

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `XG2G_CONNECTIVITY_PROFILE` | `lan` | Policy bundle: `lan`, `reverse_proxy`, `tunnel`, or `vps` |
| `XG2G_CONNECTIVITY_ALLOW_LOCAL_HTTP` | `false` | Explicit opt-in for `local_http` published endpoints |
| `XG2G_PUBLISHED_ENDPOINTS` | `[]` | JSON array of backend-published endpoint candidates for Android/Web/TV clients |

## 2. Configuration File (YAML)

Default location: `config.yaml`. Strict validation is enabled by default.

```yaml
enigma2:
  baseUrl: "http://192.168.1.50"
  streamPort: 8001
  authMode: "inherit"
epg:
  enabled: true
  days: 7
recording_playback:
  playback_policy: auto
  mappings:
    - receiver_root: /media/hdd/movie
      local_root: /mnt/recordings
connectivity:
  profile: reverse_proxy
  allowLocalHTTP: false
  publishedEndpoints:
    - url: "https://tv.example.net"
      kind: public_https
      priority: 10
      allowPairing: true
      allowStreaming: true
      allowWeb: true
      allowNative: true
      advertiseReason: "public reverse proxy"
```

Public deployment profiles are policy bundles, not hidden runtime modes:

- `lan`: local-first setup, public origin not required
- `reverse_proxy`: public HTTPS is terminated by an explicit reverse proxy; `trustedProxies` is mandatory
- `tunnel`: public HTTPS is terminated by a tunnel ingress; `trustedProxies` is mandatory
- `vps`: xg2g terminates public TLS directly; `tls.enabled` is mandatory

The operator-grade diagnostics endpoint is `GET /api/v3/system/connectivity`.
It returns the effective profile, blocking findings, selected published
endpoints, trusted proxy truth, and request-scoped header evaluation.

## 3. Health Endpoints

Endpoints return JSON and do not require authentication.

| Endpoint | Method | Success | Failure | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| `/healthz` | GET | `200` | - | Liveness: Process is alive |
| `/readyz` | GET | `200` | `503` | Readiness: Dependencies available |
| `/api/v3/system/connectivity` | GET | `200` | `503` | Authenticated connectivity contract diagnostics (`v3:admin`) |

> [!TIP]
> Use `/readyz?verbose=true` for detailed component status.
