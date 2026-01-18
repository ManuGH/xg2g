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

## 1. Best-Practice 2026 (Mandatory)

*These settings are considered mandatory for a clean, professional operation.*

### Core / Observability

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_LOG_LEVEL` | `info` | Debug is for temporary diagnosis only. |
| `XG2G_LOG_SERVICE` | `xg2g` (or instance ID) | Required for log aggregation and attribution. |
| `XG2G_DATA` | `/var/lib/xg2g` | `/tmp` is not acceptable for persistence. |

### Networking & API

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_LISTEN` | `:8088` | Standard port convention. |
| `XG2G_TRUSTED_PROXIES` | *<CIDRs>* | Required if behind a Reverse Proxy (Zero Trust). |
| `XG2G_ALLOWED_ORIGINS` | *<Domains>* | Explicit CORS policy required. No `*`. |

### Authentication

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_API_TOKENS` | `[{"token":"...","scopes":["..."]}]` | **JSON Array**. Single token (`XG2G_API_TOKEN`) is legacy. |
| **Scopes** | *Explicit* | Never implicit. 2026 = Multi-Token + Scope-Isolation. |

### Enigma2 / Upstream

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_OWI_BASE` | *<URL>* | Explicit upstream target. |
| `XG2G_E2_AUTH_MODE` | `explicit` | No "inherit" magic. Fail-closed if auth fails. |
| `XG2G_E2_TIMEOUT` | *<Duration>* | Fail-closed network assumptions. |
| `XG2G_E2_RETRIES` | *<Int>* | Deterministic retry behavior. |
| `XG2G_E2_BACKOFF` | *<Duration>* | Prevent thundering herd on upstream. |

### Engine & State

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_ENGINE_ENABLED` | `true` | The engine is the core product. |
| `XG2G_ENGINE_MODE` | `standard` | `virtual` is for testing only. |
| `XG2G_ENGINE_IDLE_TIMEOUT`| *<Duration>* | Deterministic resource release. |
| `XG2G_STORE_BACKEND` | `bolt` | Memory store is for testing only. |
| `XG2G_STORE_PATH` | `/var/lib/xg2g/store` | Directory is preferred (Bolt uses `state.db` inside); file path is also supported. |

---

## 2. Sensible Defaults (Standard)

*Healthy defaults that are usually correct but can be adjusted.*

### Streaming & HLS

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_HLS_ROOT` | `${XG2G_DATA}/hls` | Defaults to a `hls` subdir under `XG2G_DATA`. |
| `XG2G_HLS_DVR_WINDOW` | `45m` | Default DVR window. |
| `XG2G_FFMPEG_BIN` | `ffmpeg` | Set only if the binary is not in `PATH`. |

### EPG

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_EPG_ENABLED` | `true` | Standard UX expectation. |
| `XG2G_EPG_DAYS` | `14` | Two-week default window. |
| `XG2G_EPG_SOURCE` | `per-service` | Most compatible fetch strategy. |
| `XG2G_FUZZY_MAX` | `2` | Reasonable tolerance for channel matching. |

### Metrics & Limits

| Variable | Value | Reason |
|----------|-------|--------|
| `XG2G_METRICS_LISTEN` | *<host:port>* (e.g. `:9091`) | Setting this enables metrics; otherwise metrics are disabled. |
| `XG2G_RATE_LIMIT_ENABLED`| `true` | Essential for shared/public instances. |
| `XG2G_RATE_LIMIT_GLOBAL` | `100` | Default global cap for DDoS protection. |

---

## 3. Advanced / Situational (Opt-in)

*Enable only with specific justification.*

### TLS / HTTPS

*Recommendation: Terminate TLS at the Reverse Proxy (e.g., Caddy, Traefik, Nginx).*

| Variable | Value | Description |
|----------|-------|-------------|
| `XG2G_TLS_ENABLED` | `true` | Internal TLS termination (if no proxy). |
| `XG2G_TLS_CERT` | *Path* | Manual certificate. |
| `XG2G_TLS_KEY` | *Path* | Manual key. |

### Overrides (Risky)

| Variable | Description |
|----------|-------------|
| `XG2G_TUNER_SLOTS` | **Danger.** Prefer Auto-Discovery. Overrides can break scheduling. |
| `XG2G_USE_WEBIF_STREAMS` | Affects upstream transcoding logic. |
| `XG2G_LOG_LEVEL=debug` | **Diagnosis Only.** Never for permanent production. |

---

## Summary

To be "Operator-Grade" in 2026 means:

1. **Explicit Auth** (Tokens + Scopes).
2. **Persistent State**.
3. **Deterministic Engine**.
4. **Fail-Closed Streaming**.
5. **Full Traceability**.
