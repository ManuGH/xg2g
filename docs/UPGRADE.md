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

## Removed Environment Variable Overrides

Matching environment variables for the above legacy keys (e.g., `XG2G_OPENWEBIF_BASE`, `XG2G_OPENWEBIF_TIMEOUT_MS`) are also no longer supported. Use the standard mapping for new keys (e.g., `XG2G_OPENWEBIF_BASEURL`, `XG2G_OPENWEBIF_TIMEOUT`).
