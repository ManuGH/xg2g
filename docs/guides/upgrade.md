# Upgrade Guide

This document records historical configuration migrations and deprecations.

## History

### Legacy Configuration Migration (v2.x)

This section lists changes to the configuration format and environment variables, including removed legacy paths.

#### HLS Root Standardization (v3.2)

The HLS segment directory has been standardized to `hls`. A [Migration Guide](migration-hls-root.md) is available.

| Section | Legacy Key | New Key | Notes |

| :--- | :--- | :--- | :--- |
| `openWebIF` | `base`, `baseURL` | `baseUrl` | |
| `openWebIF` | `user` | `username` | |
| `openWebIF` | `pass` | `password` | |
| `openWebIF` | `stream_port` | `streamPort` | |
| `openWebIF` | `useWebIF` | `useWebIFStreams` | |
| `openWebIF` | `timeoutMs`, `timeout_ms` | `timeout` | Value converted to duration string (e.g., `5000` -> `5000ms`) |
| `openWebIF` | `backoffMs`, `backoff_ms` | `backoff` | Value converted to duration string |
| `openWebIF` | `maxBackoffMs`, `max_backoff_ms` | `maxBackoff` | Value converted to duration string |
| `api` | `addr`, `apiAddr` | `listenAddr` | |
| `metrics` | `addr`, `metricsAddr` | `listenAddr` | |
| `epg` | `xmltv` | `xmltvPath` | |
| Root | `xmltv` | `epg.xmltvPath` | Moved to `epg` section |

#### Removed Environment Variable Aliases (v3.0.0)

Legacy environment variable aliases are no longer accepted as of v3.0.0 (e.g., `RECEIVER_IP`, `RECEIVER_USER`, `RECEIVER_PASS`, `XG2G_API_ADDR`, `XG2G_METRICS_ADDR`, `XG2G_PICONS_BASE`, `XG2G_EPG_XMLTV_PATH`). Use canonical names instead (e.g., `XG2G_OWI_BASE`, `XG2G_OWI_USER`, `XG2G_OWI_PASS`, `XG2G_LISTEN`, `XG2G_METRICS_LISTEN`, `XG2G_PICON_BASE`, `XG2G_XMLTV`).

#### Query Token Authentication Removed (v3.0.0)

Authentication via `?token=...` was removed in v3.0.0 due to security risks. Use `Authorization: Bearer <token>` or the `xg2g_session` cookie instead.
