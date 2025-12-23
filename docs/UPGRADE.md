# Upgrade Guide

This document lists changes to the configuration format and environment variables, including removed legacy paths.

## Removed Legacy Configuration Keys

The following configuration keys were previously auto-migrated but have now been removed. Please update your `config.yaml` to use the new keys.

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

## Deprecated Environment Variable Aliases

Some legacy environment variable aliases are still accepted for backward compatibility (e.g., `RECEIVER_IP`, `RECEIVER_USER`, `RECEIVER_PASS`, `XG2G_API_ADDR`, `XG2G_METRICS_ADDR`, `XG2G_PICONS_BASE`, `XG2G_EPG_XMLTV_PATH`). These emit startup warnings and will be removed in **v2.2**. Use canonical names instead (e.g., `XG2G_OWI_BASE`, `XG2G_OWI_USER`, `XG2G_OWI_PASS`, `XG2G_LISTEN`, `XG2G_METRICS_LISTEN`, `XG2G_PICON_BASE`, `XG2G_XMLTV`).
