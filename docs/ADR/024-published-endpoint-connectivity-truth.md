# ADR-024: Published Endpoint Connectivity Truth

**Status:** Accepted
**Date:** 2026-04-09

## Context

Self-hosted deployments frequently run `xg2g` behind Docker, Compose, reverse
proxies, split DNS, and mixed local/remote topologies. In that environment,
client-side endpoint guessing becomes a liability:

- container IPs leak into clients
- reverse-proxy vs direct-origin drift appears
- TV/native/web build inconsistent fallback behavior
- clients start carrying topology policy that belongs on the server

Per [ADR-005](005-Architecture-Invariants.md), connectivity policy must remain
backend truth. The client may test candidates, but it may not invent them.

## Decision

### 1. Backend publishes canonical endpoints

The backend publishes the complete ordered set of allowed connection candidates.

Each published endpoint contains at least:

- `url`
- `kind`
  - `public_https`
  - `local_https`
  - `local_http`
- `priority`
- `tls_mode`
- `allow_pairing`
- `allow_streaming`
- `allow_web`
- `allow_native`
- `advertise_reason`

These published endpoints are the only allowed reachability candidates.

### 2. Clients may test, but not invent

Clients may:

- test published endpoints in backend-defined priority order
- cache the last successful published endpoint locally
- report success/failure telemetry back to the backend

Clients may not:

- derive new origins from request headers
- scan the local subnet for candidate servers
- infer Docker hostnames or container IPs
- rewrite ports or upgrade/downgrade schemes heuristically
- synthesize browser-only or TV-only fallback origins outside backend truth

### 3. Reverse-proxy-first is the default policy

The normative production default is HTTPS behind an explicitly published origin.

Rules:

- Default published endpoints are HTTPS only.
- `local_http` is explicit opt-in.
- Internal Docker names, bridge IPs, and container addresses are never published.
- `host.docker.internal` is never a published endpoint.
- Request-header-derived origin guesswork is not a source of truth.

### 4. Connectivity reports are telemetry only

Client reports do not modify backend truth directly.

Connectivity reporting exists to support:

- diagnostics
- endpoint health telemetry
- future operator tooling

It does not authorize clients to mutate, reprioritize, or create endpoint truth.

### 5. Endpoint usage is capability-scoped

The backend may publish different endpoint sets or flags based on effective
policy, deployment mode, or client posture.

Examples:

- allow web bootstrap only on HTTPS endpoints
- allow streaming on local-only endpoints
- allow pairing on a restricted subset of endpoints

## Consequences

### Positive

- Container/runtime topology does not leak into client contracts.
- Android, WebUI, and TV all consume the same reachability truth.
- Reverse-proxy and TLS policy become enforceable instead of advisory.

### Negative

- Operators must configure published endpoints explicitly.
- Local "it worked because the client guessed right" behavior is removed.

## Guardrails

Never publish:

- container IPs
- Docker service names
- `localhost` for non-local clients
- `host.docker.internal`
- origins reconstructed from untrusted forwarded headers without allowlist validation

## References

- [ADR-005](005-Architecture-Invariants.md)
- [ADR-023](023-device-enrollment-session-model.md)
- [Proposed Device/OpenAPI Contract](/Users/manuel/StudioProjects/xg2g/openapi/device-enrollment-connectivity.proposed.yaml)
