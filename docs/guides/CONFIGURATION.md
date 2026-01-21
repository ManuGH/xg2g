# xg2g Configuration Guide (Operator-Grade 2026)

This document defines the normative configuration standards for production-grade xg2g deployments.
We structure configuration into three distinct tiers.

## Philosophy

**"Operator-Grade 2026"** means:

- No implicit magic (Fail-Closed).
- Deterministic resource management.
- Traceability by default.
- Secure, scoped access.

> "Whatever is not explicitly set is considered suspicious."

---

## Configuration Options (Registry-Generated)

Configuration options are generated from the registry to prevent drift. Any manual edits
inside the generated block are overwritten by `cmd/configgen`.

Note: YAML compatibility keys under `openWebIF.*` map to the registry's `enigma2.*` entries.
Defaults and env bindings are listed under `enigma2.*` below.

Generated artifacts:
- `config.generated.example.yaml` is the canonical defaults projection (fully generated).
- `config.example.yaml` is a curated operator tutorial and may be selective.

<!-- BEGIN GENERATED CONFIG OPTIONS -->
## Registry Options (Generated)

This section is generated from `internal/config/registry.go`. Do not edit by hand.

### api

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `api.allowedOrigins` | `XG2G_ALLOWED_ORIGINS` | - | Active | Advanced |
| `api.listenAddr` | `XG2G_LISTEN` | `:8088` | Active | Simple |
| `api.token` | `XG2G_API_TOKEN` | - | Active | Simple |
| `api.tokenScopes` | `XG2G_API_TOKEN_SCOPES` | - | Active | Advanced |
| `api.tokens` | `XG2G_API_TOKENS` | - | Active | Advanced |

### engine

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `engine.cpuThresholdScale` | `XG2G_ENGINE_CPU_SCALE` | `1.5` | Active | Advanced |
| `engine.enabled` | `XG2G_ENGINE_ENABLED` | `false` | Active | Advanced |
| `engine.gpuLimit` | `XG2G_ENGINE_GPU_LIMIT` | `8` | Active | Advanced |
| `engine.idleTimeout` | `XG2G_ENGINE_IDLE_TIMEOUT` | `1m` | Active | Advanced |
| `engine.maxPool` | `XG2G_ENGINE_MAX_POOL` | `2` | Active | Advanced |
| `engine.mode` | `XG2G_ENGINE_MODE` | `standard` | Active | Advanced |
| `engine.tunerSlots` | `XG2G_TUNER_SLOTS` | - | Active | Advanced |

### enigma2

Aliases: `openWebIF.*` (compat; prefer `enigma2.*`).

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `enigma2.analyzeDuration` | - | `10000000` | Active | Advanced |
| `enigma2.authMode` | - | `inherit` | Active | Advanced |
| `enigma2.backoff` | `XG2G_OWI_BACKOFF_MS` | `200ms` | Active | Advanced |
| `enigma2.baseUrl` | `XG2G_OWI_BASE` | - | Active | Simple |
| `enigma2.fallbackTo8001` | `XG2G_E2_FALLBACK_TO_8001` | `false` | Active | Integrator |
| `enigma2.maxBackoff` | `XG2G_OWI_MAX_BACKOFF_MS` | `30s` | Active | Advanced |
| `enigma2.password` | `XG2G_OWI_PASS` | - | Active | Simple |
| `enigma2.preflightTimeout` | `XG2G_E2_PREFLIGHT_TIMEOUT` | `10s` | Active | Advanced |
| `enigma2.probeSize` | - | `32M` | Active | Advanced |
| `enigma2.rateBurst` | - | - | Active | Advanced |
| `enigma2.rateLimit` | - | - | Active | Advanced |
| `enigma2.responseHeaderTimeout` | - | `10s` | Active | Advanced |
| `enigma2.retries` | `XG2G_OWI_RETRIES` | `2` | Active | Advanced |
| `enigma2.streamPort` | `XG2G_STREAM_PORT` | `8001` | Deprecated | Advanced |
| `enigma2.timeout` | `XG2G_OWI_TIMEOUT_MS` | `10s` | Active | Advanced |
| `enigma2.tuneTimeout` | - | `10s` | Active | Advanced |
| `enigma2.useWebIFStreams` | `XG2G_USE_WEBIF_STREAMS` | `true` | Active | Advanced |
| `enigma2.userAgent` | - | - | Active | Advanced |
| `enigma2.username` | `XG2G_OWI_USER` | - | Active | Simple |

### epg

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `epg.days` | `XG2G_EPG_DAYS` | `14` | Active | Simple |
| `epg.enabled` | `XG2G_EPG_ENABLED` | `true` | Active | Simple |
| `epg.fuzzyMax` | `XG2G_FUZZY_MAX` | `2` | Active | Advanced |
| `epg.maxConcurrency` | `XG2G_EPG_MAX_CONCURRENCY` | `5` | Active | Advanced |
| `epg.retries` | `XG2G_EPG_RETRIES` | `2` | Active | Advanced |
| `epg.source` | `XG2G_EPG_SOURCE` | `per-service` | Active | Advanced |
| `epg.timeoutMs` | `XG2G_EPG_TIMEOUT_MS` | `5000` | Active | Advanced |
| `epg.xmltvPath` | `XG2G_XMLTV` | `xmltv.xml` | Active | Advanced |

### ffmpeg

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `ffmpeg.bin` | `XG2G_FFMPEG_BIN` | `ffmpeg` | Active | Advanced |
| `ffmpeg.killTimeout` | - | `5s` | Active | Advanced |

### hdhr

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `hdhr.baseUrl` | - | - | Active | Advanced |
| `hdhr.deviceId` | - | - | Active | Advanced |
| `hdhr.enabled` | - | `false` | Active | Advanced |
| `hdhr.firmwareName` | - | - | Active | Advanced |
| `hdhr.friendlyName` | - | - | Active | Advanced |
| `hdhr.modelNumber` | - | - | Active | Advanced |
| `hdhr.plexForceHls` | - | - | Active | Advanced |
| `hdhr.tunerCount` | - | - | Active | Advanced |

### hls

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `hls.dvrWindow` | `XG2G_HLS_DVR_WINDOW` | `45m` | Active | Advanced |
| `hls.root` | `XG2G_HLS_ROOT` | - | Active | Advanced |

### library

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `library.db_path` | - | - | Active | Advanced |
| `library.enabled` | - | `false` | Active | Advanced |
| `library.roots` | - | - | Active | Advanced |

### metrics

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `metrics.enabled` | - | `false` | Active | Advanced |
| `metrics.listenAddr` | `XG2G_METRICS_LISTEN` | `""` | Active | Advanced |

### picons

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `picons.baseUrl` | `XG2G_PICON_BASE` | - | Active | Simple |

### rateLimit

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `rateLimit.auth` | - | `10` | Active | Advanced |
| `rateLimit.burst` | - | `20` | Active | Advanced |
| `rateLimit.enabled` | - | `true` | Active | Advanced |
| `rateLimit.global` | - | `100` | Active | Advanced |
| `rateLimit.whitelist` | `XG2G_RATE_LIMIT_WHITELIST` | - | Active | Advanced |

### recording_playback

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `recording_playback.mappings` | - | - | Active | Advanced |
| `recording_playback.playback_policy` | - | `auto` | Active | Advanced |
| `recording_playback.stable_window` | - | `10s` | Active | Advanced |

### root

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `bouquets` | `XG2G_BOUQUET` | - | Active | Simple |
| `configStrict` | `XG2G_CONFIG_STRICT` | `true` | Active | Advanced |
| `dataDir` | `XG2G_DATA` | `/tmp` | Active | Simple |
| `logLevel` | `XG2G_LOG_LEVEL` | `info` | Active | Simple |
| `logService` | `XG2G_LOG_SERVICE` | - | Active | Advanced |
| `readyStrict` | `XG2G_READY_STRICT` | `false` | Active | Advanced |
| `recording_roots` | - | - | Active | Advanced |
| `trustedProxies` | `XG2G_TRUSTED_PROXIES` | - | Active | Advanced |
| `version` | `XG2G_VERSION` | - | Active | Simple |

### sessions

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `sessions.expiry_check_interval` | - | `1m` | Active | Advanced |
| `sessions.heartbeat_interval` | - | `30s` | Active | Advanced |
| `sessions.lease_ttl` | - | `2h` | Active | Advanced |

### store

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `store.backend` | `XG2G_STORE_BACKEND` | `memory` | Active | Advanced |
| `store.path` | `XG2G_STORE_PATH` | `/var/lib/xg2g/store` | Active | Advanced |

### streaming

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `streaming.delivery_policy` | `XG2G_STREAMING_POLICY` | `universal` | Active | Simple |

### tls

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `tls.cert` | `XG2G_TLS_CERT` | - | Active | Advanced |
| `tls.enabled` | `XG2G_TLS_ENABLED` | `false` | Active | Advanced |
| `tls.forceHTTPS` | `XG2G_FORCE_HTTPS` | `false` | Active | Advanced |
| `tls.key` | `XG2G_TLS_KEY` | - | Active | Advanced |

### vod

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `vod.analyzeDuration` | - | `50000000` | Active | Advanced |
| `vod.cacheTTL` | - | `24h` | Active | Advanced |
| `vod.maxConcurrent` | - | `2` | Active | Advanced |
| `vod.probeSize` | - | `50M` | Active | Advanced |
| `vod.stallTimeout` | - | `1m` | Active | Advanced |

<!-- END GENERATED CONFIG OPTIONS -->

## Summary

To be "Operator-Grade" in 2026 means:

1. **Explicit Auth** (Tokens + Scopes).
2. **Persistent State**.
3. **Deterministic Engine**.
4. **Fail-Closed Streaming**.
5. **Full Traceability**.
