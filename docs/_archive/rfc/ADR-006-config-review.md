# ADR-006: Config Surface Review & Zombie Purge

## Status

- **Date**: 2026-01-06
- **Status**: Draft / In Progress
- **Context**: Post-PR 3 (Canonical Formatting), Pre-PR 4 (Cleanup)

## Goal

To rigorously classify every supported configuration key, determine its behavior, and decide its fate (Keep / Remove / Internal).

## Inventory & Decisions

### 1. Feature Flags / Zombies

| Key | Code Effect | Logic Path | Zombie? | Decision |
| :--- | :--- | :--- | :--- | :--- |
| `XG2G_INSTANT_TUNE` | Parsed, exposed in SysInfo | **None** (UNIMPLEMENTED) | 🧟 **YES** | **REMOVE** (Placebo; purge from parsing, structs, API, docs) |
| `XG2G_SHADOW_INTENTS` | Parsed | **None** (UNUSED) | 🧟 **YES** | **REMOVE** (Unreferenced; purge complete chain) |
| `XG2G_SHADOW_TARGET` | Parsed | **None** (UNUSED) | 🧟 **YES** | **REMOVE** (Dangling without ShadowIntents) |
| `XG2G_READY_STRICT` | `health.SetReadyStrict(true)` | Enforces OWI connectivity check at startup | No | **KEEP** (Critical for orchestration readiness) |
| `XG2G_CONFIG_STRICT` | `loader.Load()` | Fails if legacy/unknown keys found | No | **KEEP** (Enforces clean config hygiene) |

### 2. Security & API

| Key | Parsed From | Default | Effective Behavior | Fail-open/closed | Abuse Case | Decision | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `XG2G_API_TOKEN` | ENV + YAML | `""` | Single-user token; requires `TOKEN_SCOPES`. | **Fail-closed** (Refuses startup if tokens missing, refuses req if headers missing) | Empty scopes reject at startup. | **KEEP / REFINE** | Enforce non-empty scopes for single token. |
| `XG2G_API_TOKEN_SCOPES`| ENV + YAML | `""` | Scopes for single token. | **Fail-closed** (Startup error if `TOKEN` set but scopes empty) | None (validated). | **KEEP** | |
| `XG2G_API_TOKENS` | ENV + YAML | `""` | Multi-user tokens (JSON/KV). | **Fail-closed** (Startup error if invalid or empty scopes) | None (validated). | **KEEP** | |
| `XG2G_TRUSTED_PROXIES` | ENV (only) | `""` | Sets Gin trusted proxies for XFF trust. | **Fail-safe** (Defaults to none; XFF ignored) | **HIGH**: `0.0.0.0/0` passes validation -> trusts all XFF -> Auth bypass risk. | **KEEP / HARDEN** | **BLOCKER FIX**: Explicitly reject `0.0.0.0/0` and `::/0` in validation. |
| `XG2G_RATE_LIMIT_ENABLED`| ENV + YAML | `true` | Enables/Disables limiter middleware. | **Fail-safe** (Enabled by default) | Disabling allows DoS. | **KEEP** | |
| `XG2G_RATE_LIMIT_GLOBAL` | ENV + YAML | `100` | Global RPS limit. | N/A | Low limit = DoS? | **KEEP** | |
| `XG2G_RATE_LIMIT_AUTH` | ENV + YAML | `10` | Auth endpoint RPM limit. | N/A | | **KEEP** | |
| `XG2G_RATE_LIMIT_BURST` | ENV + YAML | `20` | Burst capacity. | N/A | | **KEEP** | |
| `XG2G_RATE_LIMIT_WHITELIST`| ENV + YAML | `""` | Bypasses limits for IPs/CIDRs. | **Fail-safe** (Empty default) | `0.0.0.0/0` bypasses all limits. | **KEEP / HARDEN** | **BLOCKER FIX**: Reject `0.0.0.0/0` and `::/0`. Strict IP/CIDR validation. |
| `XG2G_TLS_CERT` | ENV | `""` | Path to Cert. | **Fail-Start** (Abort if Key missing) | | **KEEP** | |
| `XG2G_TLS_KEY` | ENV | `""` | Path to Key. | **Fail-Start** (Abort if Cert missing) | | **KEEP** | |
| `XG2G_FORCE_HTTPS` | ENV | `false` | Redirects HTTP->HTTPS. | **Fail-Open** (Default false) | Infinite loop if behind termination? | **KEEP** | Clarify termination scenario in docs. |
| `XG2G_ALLOWED_ORIGINS` | YAML (Only) | `""` | CORS Allow-Origin header. | **Inconsistent** (ENV logic missing in `mergeEnvConfig`) | | **IMPLEMENT** | Add ENV support to `mergeEnvConfig`. |
| `XG2G_TLS_ENABLED` | **ENV (main.go)**| `false`| Triggers auto-cert generation if no certs provided. | **Inconsistent** (Not in Config struct) | | **REFINE** | Move to `AppConfig`. Fail-start if enabled but certs missing/autogen failed. |

### 3. Engine & Source

| Key | Parsed From | Default | Effective Behavior | Fail-open/closed | Abuse Case | Decision | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `XG2G_ENGINE_ENABLED` | ENV + YAML | `true` | Controls v3 worker start. | **N/A** (Feature Flag) | Resource exhaustion if enabled blindly? | **KEEP** | |
| `XG2G_ENGINE_MODE` | ENV + YAML | `standard`| `standard` vs `virtual`. | **Fail-Start** (Validated enum) | | **KEEP** | |
| `XG2G_TUNER_SLOTS` | ENV + YAML | `auto` | List of tuner indexes. | **Fail-Safe** (Auto / [0] if virtual) | | **KEEP** | Check for extreme ranges (MEMORY/CPU). |
| `XG2G_E2_HOST` | ENV + YAML | `XG2G_OWI_BASE` | Enigma2 API base URL. | **Fail-Start** (Inherits or fails if missing) | | **KEEP** | |
| `XG2G_E2_USER` | ENV + YAML | `XG2G_OWI_USER` | Enigma2 Username. | **Inherit** (Explicit opt-out needed?) | | **KEEP** | controlled by `E2_AUTH_MODE` |
| `XG2G_E2_PASS` | ENV + YAML | `XG2G_OWI_PASS` | Enigma2 Password. | **Inherit** (Explicit opt-out needed?) | | **KEEP** | controlled by `E2_AUTH_MODE` |
| `XG2G_USE_WEBIF_STREAMS`| ENV + YAML | `true` | Uses `/web/stream.m3u` (Port 80). | **Fail-Safe** (Receiver handles port) | | **KEEP** | Best practice default. |
| `XG2G_STREAM_PORT` | ENV + YAML | `8001` | **Direct Mode Only**: Port for TS. | **Fail-Safe** (Ignored if WebIF=true) | Wrong port = No stream. | **KEEP** | Confirmed "Ignored if WebIF used". |
| `XG2G_E2_AUTH_MODE` | **New** | `inherit` | Controls auth inheritance logic. | **Fail-Start** (Validated enum) | | **IMPLEMENT** | Enum: `inherit`, `none`, `explicit`. Solves opt-out gap. |

### 4. Specification: `XG2G_E2_AUTH_MODE` (PR 4)

**Definition**
- **ENV**: `XG2G_E2_AUTH_MODE`
- **YAML**: `enigma2.authMode`
- **Values**: `inherit` (default), `none`, `explicit` (case-insensitive)

**Logic**

1. **Resolving Effective Credentials**:
    - **inherit**: If `Enigma2.Username/Password` are empty, inherit from `OWIUsername/Password`. If `Enigma2` creds are set, they take precedence.
    - **none**: Force effective Enigma2 credentials to empty (no auth).
    - **explicit**: Use only `Enigma2` credentials from config. If empty, use no auth (with warning).

2. **Validation Rules (Fail-Start)**:
    - **Invalid Enum**: Reject unknown values.
    - **Pair Consistency**: Reject if only one of User/Pass is set (applies to both OWI inheritance and explicit E2 config).
    - **Conflict (Mode=None)**: Reject if `XG2G_E2_AUTH_MODE=none` BUT `XG2G_E2_USER/PASS` are set.
    - **Empty Explicit**: If `explicit` and no creds set -> **Log Warning** (allow start).

3. **Precedence**:
    - `Defaults` -> `YAML` -> `ENV` -> `ResolveEffective()` -> `Validate()`
