# Verified Technical Guarantees

**Status:** Code-verified claims that can be stated without qualification.

Last verified: 2026-01-06

---

## Configuration Governance

✅ **Strict Validation with Fail-Start**

- Invalid config → application refuses to start
- CIDR validation blocks forbidden networks (`0.0.0.0/0`, `::/0`)
- E2 Auth Mode validation enforces pair consistency
- TLS validation matrix (cert/key must be paired or both empty)

✅ **No Zombie Configuration**

- Removed: `InstantTuneEnabled`, `ShadowIntentsEnabled`, `ShadowTarget`
- All config keys are actively used or explicitly documented as removed

✅ **Deterministic Resolution**

- E2 Auth Mode: `inherit`/`none`/`explicit` with clear semantics
- TLS: Autogeneration when enabled without cert/key
- ALLOWED_ORIGINS: ENV override policy (ENV replaces YAML if set)

---

## Security (Fail-Closed)

✅ **Token-Based Authentication**

- Multi-token support with scopes (`v3:read`, `v3:write`, `v3:admin`)
- Fail-closed: No tokens configured → deny all
- Constant-time comparison (timing attack prevention)

✅ **Rate Limiting**

- Global and per-IP limits
- CIDR whitelist with strict validation
- Forbidden networks blocked at startup

✅ **Network Security**

- Trusted proxies with CIDR validation
- CORS with ENV override support
- TLS support with autogeneration option

---

## Observability

✅ **Prometheus Metrics Exist**

- `/internal/metrics/` with business, transcoder, circuit breaker metrics
- Metrics endpoint available (`/metrics`)
- Structured logging (zerolog)

✅ **Health & Readiness**

- Health checks implemented
- Readiness checks with `READY_STRICT` mode

---

## API Contract

✅ **WebUI Exists**

- React 19 + TypeScript frontend
- Located in `/webui/`

✅ **Structured Errors**

- RFC 7807-style error responses
- Machine-readable error codes
- Request ID propagation

---

## Architecture (Structurally Verified)

✅ **Event-Driven Design**

- Intent-based API (documented in ARCHITECTURE.md)
- Session lifecycle FSM (documented in V3_FSM.md)
- Event bus architecture

✅ **HLS Streaming**

- HLS root configuration
- DVR window support
- Recording playback

---

## Non-Goals (Explicitly Documented)

✅ **Clearly Defined Scope**

- Not a multi-tenant SaaS
- Not a full TV server replacement
- Not a DRM/OTT system
- Not a generic transcoding cluster

See: `ARCHITECTURE.md` for full non-goals list.

---

## What This Means

These guarantees are **code-backed** and can be stated in:

- README.md
- Documentation
- Marketing materials
- Support responses

Without qualification or "planned" disclaimers.
