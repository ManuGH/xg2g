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
| `api.disableLegacyTokenSources` | `XG2G_API_DISABLE_LEGACY_TOKEN_SOURCES` | `false` | Active | Advanced |
| `api.listenAddr` | `XG2G_LISTEN` | `:8088` | Active | Simple |
| `api.playbackDecisionKeyId` | `XG2G_PLAYBACK_DECISION_KID` | - | Active | Advanced |
| `api.playbackDecisionPreviousKeys` | `XG2G_PLAYBACK_DECISION_PREVIOUS_KEYS` | - | Active | Advanced |
| `api.playbackDecisionRotationWindow` | `XG2G_PLAYBACK_DECISION_ROTATION_WINDOW` | `1` | Active | Advanced |
| `api.playbackDecisionSecret` | `XG2G_PLAYBACK_DECISION_SECRET` | - | Active | Advanced |
| `api.token` | `XG2G_API_TOKEN` | - | Active | Simple |
| `api.tokenScopes` | `XG2G_API_TOKEN_SCOPES` | - | Active | Advanced |
| `api.tokens` | `XG2G_API_TOKENS` | - | Active | Advanced |

### breaker

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `breaker.consecutive_threshold` | - | `5` | Active | Advanced |
| `breaker.failures_threshold` | - | `7` | Active | Advanced |
| `breaker.min_attempts` | - | `10` | Active | Advanced |
| `breaker.window` | - | `5m` | Active | Advanced |

### engine

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `engine.cpuThresholdScale` | `XG2G_ENGINE_CPU_SCALE` | `1.5` | Active | Advanced |
| `engine.enabled` | `XG2G_ENGINE_ENABLED` | `true` | Active | Advanced |
| `engine.gpuLimit` | `XG2G_ENGINE_GPU_LIMIT` | `8` | Active | Advanced |
| `engine.idleTimeout` | `XG2G_ENGINE_IDLE_TIMEOUT` | `5m` | Active | Advanced |
| `engine.maxPool` | `XG2G_ENGINE_MAX_POOL` | `2` | Active | Advanced |
| `engine.mode` | `XG2G_ENGINE_MODE` | `standard` | Active | Advanced |
| `engine.tunerSlots` | `XG2G_TUNER_SLOTS` | - | Active | Advanced |

### enigma2

Aliases: `openWebIF.*` (compat; prefer `enigma2.*`).

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `enigma2.analyzeDuration` | `XG2G_E2_ANALYZE_DURATION` | `2000000` | Active | Advanced |
| `enigma2.authMode` | `XG2G_E2_AUTH_MODE` | `inherit` | Active | Advanced |
| `enigma2.backoff` | `XG2G_E2_BACKOFF` | `200ms` | Active | Advanced |
| `enigma2.baseUrl` | `XG2G_E2_HOST` | - | Active | Simple |
| `enigma2.fallbackTo8001` | `XG2G_E2_FALLBACK_TO_8001` | `true` | Active | Integrator |
| `enigma2.maxBackoff` | `XG2G_E2_MAX_BACKOFF` | `30s` | Active | Advanced |
| `enigma2.password` | `XG2G_E2_PASS` | - | Active | Simple |
| `enigma2.preflightTimeout` | `XG2G_E2_PREFLIGHT_TIMEOUT` | `10s` | Active | Advanced |
| `enigma2.probeSize` | `XG2G_E2_PROBE_SIZE` | `5M` | Active | Advanced |
| `enigma2.rateBurst` | `XG2G_E2_RATE_BURST` | - | Active | Advanced |
| `enigma2.rateLimit` | `XG2G_E2_RATE_LIMIT` | - | Active | Advanced |
| `enigma2.responseHeaderTimeout` | `XG2G_E2_RESPONSE_HEADER_TIMEOUT` | `10s` | Active | Advanced |
| `enigma2.retries` | `XG2G_E2_RETRIES` | `2` | Active | Advanced |
| `enigma2.streamPort` | `XG2G_E2_STREAM_PORT` | `8001` | Deprecated | Advanced |
| `enigma2.timeout` | `XG2G_E2_TIMEOUT` | `10s` | Active | Advanced |
| `enigma2.tuneTimeout` | `XG2G_E2_TUNE_TIMEOUT` | `10s` | Active | Advanced |
| `enigma2.useWebIFStreams` | `XG2G_E2_USE_WEBIF_STREAMS` | `true` | Active | Advanced |
| `enigma2.userAgent` | `XG2G_E2_USER_AGENT` | - | Active | Advanced |
| `enigma2.username` | `XG2G_E2_USER` | - | Active | Simple |

### epg

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `epg.days` | `XG2G_EPG_DAYS` | `14` | Active | Simple |
| `epg.enabled` | `XG2G_EPG_ENABLED` | `true` | Active | Simple |
| `epg.fuzzyMax` | `XG2G_FUZZY_MAX` | `2` | Active | Advanced |
| `epg.maxConcurrency` | `XG2G_EPG_MAX_CONCURRENCY` | `1` | Active | Advanced |
| `epg.retries` | `XG2G_EPG_RETRIES` | `2` | Active | Advanced |
| `epg.source` | `XG2G_EPG_SOURCE` | `per-service` | Active | Advanced |
| `epg.timeoutMs` | `XG2G_EPG_TIMEOUT_MS` | `5000` | Active | Advanced |
| `epg.xmltvPath` | `XG2G_XMLTV` | `xmltv.xml` | Active | Advanced |

### ffmpeg

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `ffmpeg.bin` | `XG2G_FFMPEG_BIN` | `ffmpeg` | Active | Advanced |
| `ffmpeg.ffprobeBin` | `XG2G_FFPROBE_BIN` | - | Active | Advanced |
| `ffmpeg.killTimeout` | `XG2G_FFMPEG_KILL_TIMEOUT` | `5s` | Active | Advanced |
| `ffmpeg.vaapiDevice` | `XG2G_VAAPI_DEVICE` | `""` | Active | Advanced |

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
| `hls.segmentSeconds` | `XG2G_HLS_SEGMENT_SECONDS` | `6` | Active | Advanced |

### library

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `library.db_path` | - | - | Active | Advanced |
| `library.enabled` | - | `false` | Active | Advanced |
| `library.roots` | - | - | Active | Advanced |

### limits

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `limits.max_sessions` | `XG2G_MAX_SESSIONS` | `8` | Active | Advanced |
| `limits.max_transcodes` | `XG2G_MAX_TRANSCODES` | `2` | Active | Advanced |

### metrics

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `metrics.enabled` | - | `false` | Active | Advanced |
| `metrics.listenAddr` | `XG2G_METRICS_LISTEN` | `""` | Active | Advanced |

### network

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `network.lan.allow.cidrs` | `XG2G_LAN_ALLOW_CIDRS` | - | Active | Advanced |
| `network.outbound.allow.cidrs` | `XG2G_OUTBOUND_ALLOW_CIDRS` | - | Active | Advanced |
| `network.outbound.allow.hosts` | `XG2G_OUTBOUND_ALLOW_HOSTS` | - | Active | Advanced |
| `network.outbound.allow.ports` | `XG2G_OUTBOUND_ALLOW_PORTS` | - | Active | Advanced |
| `network.outbound.allow.schemes` | `XG2G_OUTBOUND_ALLOW_SCHEMES` | - | Active | Advanced |
| `network.outbound.enabled` | `XG2G_OUTBOUND_ENABLED` | `false` | Active | Advanced |

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

### server

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `server.idleTimeout` | `XG2G_SERVER_IDLE_TIMEOUT` | `2m` | Active | Advanced |
| `server.maxHeaderBytes` | `XG2G_SERVER_MAX_HEADER_BYTES` | `1048576` | Active | Advanced |
| `server.readTimeout` | `XG2G_SERVER_READ_TIMEOUT` | `1m` | Active | Advanced |
| `server.shutdownTimeout` | `XG2G_SERVER_SHUTDOWN_TIMEOUT` | `15s` | Active | Advanced |
| `server.writeTimeout` | `XG2G_SERVER_WRITE_TIMEOUT` | `0s` | Active | Advanced |

### sessions

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `sessions.expiry_check_interval` | - | `1m` | Active | Advanced |
| `sessions.heartbeat_interval` | - | `30s` | Active | Advanced |
| `sessions.lease_ttl` | - | `2h` | Active | Advanced |

### store

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `store.backend` | `XG2G_STORE_BACKEND` | `sqlite` | Active | Advanced |
| `store.path` | `XG2G_STORE_PATH` | `/var/lib/xg2g/store` | Active | Advanced |

### streaming

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `streaming.delivery_policy` | `XG2G_STREAMING_POLICY` | `universal` | Active | Simple |

### timeouts

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `timeouts.kill_grace` | - | `2s` | Active | Advanced |
| `timeouts.transcode_no_progress` | - | `30s` | Active | Advanced |
| `timeouts.transcode_start` | - | `15s` | Active | Advanced |

### tls

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `tls.cert` | `XG2G_TLS_CERT` | - | Active | Advanced |
| `tls.enabled` | `XG2G_TLS_ENABLED` | `false` | Active | Advanced |
| `tls.forceHTTPS` | `XG2G_FORCE_HTTPS` | `false` | Active | Advanced |
| `tls.key` | `XG2G_TLS_KEY` | - | Active | Advanced |

### verification

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `verification.enabled` | `XG2G_VERIFY_ENABLED` | `true` | Active | Advanced |
| `verification.interval` | `XG2G_VERIFY_INTERVAL` | `1m` | Active | Advanced |

### vod

| Path | Env | Default | Status | Profile |
| --- | --- | --- | --- | --- |
| `vod.analyzeDuration` | - | `50000000` | Active | Advanced |
| `vod.cacheMaxEntries` | - | `256` | Active | Advanced |
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
