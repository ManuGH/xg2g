# ADR-006: Configuration Naming Policy (Pre-Release)

## Status

Accepted (Pre-Release Finalization)

## Context

The codebase contains "migration-era" configuration keys (`XG2G_V3_*`) and redundant naming patterns. Since the product has **not yet been publicly released**, we enforce a strict "Canonical Only" policy to avoid long-term technical debt.

## Policy

### 1. Canonical Names Only

Version numbers (`v3`) and legacy prefixes MUST NOT appear in configuration keys (Env or YAML). Code must only read the canonical domain-based keys.

### 2. No Aliases / Migration

We do NOT support legacy keys. There is no migration phase, no deprecation warnings, and no support for mixing old and new keys.

### 3. Fail-Fast Guardrail

To prevent configuration errors (users setting an old key and thinking it works), the application MUST detect legacy `XG2G_V3_*` environment variables on startup and **exit immediately with a fatal error**.

## Canonical Naming Matrix

| Category | Canonical Key (Final) | Notes |
| :--- | :--- | :--- |
| **Engine** | `XG2G_ENGINE_ENABLED` | |
| **Engine** | `XG2G_ENGINE_MODE` | `standard` or `virtual` |
| **Engine** | `XG2G_ENGINE_IDLE_TIMEOUT` | |
| **Store** | `XG2G_STORE_BACKEND` | `memory` or `bolt` |
| **Store** | `XG2G_STORE_PATH` | |
| **HLS** | `XG2G_HLS_ROOT` | |
| **HLS** | `XG2G_DVR_WINDOW` | |
| **Enigma2** | `XG2G_E2_HOST` | |
| **Enigma2** | `XG2G_E2_USER` | |
| **Enigma2** | `XG2G_E2_PASS` | |
| **Enigma2** | `XG2G_E2_TIMEOUT` | |
| **Enigma2** | `XG2G_E2_RESPONSE_HEADER_TIMEOUT` | |
| **Enigma2** | `XG2G_E2_RETRIES` | |
| **Enigma2** | `XG2G_E2_BACKOFF` | |
| **Enigma2** | `XG2G_E2_MAX_BACKOFF` | |
| **Enigma2** | `XG2G_E2_RATE_LIMIT` | |
| **Enigma2** | `XG2G_E2_RATE_BURST` | |
| **Enigma2** | `XG2G_E2_USER_AGENT` | |
| **FFmpeg** | `XG2G_FFMPEG_BIN` | |
| **FFmpeg** | `XG2G_FFMPEG_KILL_TIMEOUT` | |
| **Global** | `XG2G_CONFIG_STRICT` | |
| **Global** | `XG2G_LISTEN` | |
| **Global** | `XG2G_TRUSTED_PROXIES` | |
| **Global** | `XG2G_API_TOKEN` | |
| **Global** | `XG2G_API_TOKEN_SCOPES` | |

## Implementation Plan (PR 3)

1. **Refactor Config**: Update `internal/config` structs and tags to match canonical names.
2. **Guardrail**: Implement a check in `internal/config/runtime_env.go` that scans `os.Environ()` for `XG2G_V3_` and `log.Fatal` if found.
3. **Documentation**: Rewrite `CONFIGURATION.md` to document ONLY the canonical keys.
