# Public Exposure Security Contract

This document defines what public reachability is allowed to mean for `xg2g`.

The Public Deployment Contract decides how `xg2g` is reachable. The Public
Exposure Security Contract decides what exposed clients may attempt once that
reachability exists.

## Exposure Policy Matrix

Every v3 operation must have an explicit exposure policy in code. The policy
defines:

- exposure class: `read`, `write`, `admin`, `device`, `pairing`, `session`, `health`, `system`
- auth kind: bearer scope, pairing secret, device grant, bootstrap token, or none
- browser trust model
- rate-limit class
- audit requirement
- sensitive error redaction requirement

Routes without an exposure policy are rejected at router construction time.
Unscoped routes must have dedicated abuse controls and audit events.

The code also validates semantic duties at router construction time:

- non-bearer operations must be explicitly allowlisted as unscoped
- pairing and device operations must use dedicated rate-limit classes
- pairing, device, write, and admin operations must emit audit events
- admin operations must require `v3:admin`
- write operations must require `v3:write`
- unauthenticated operations must not be browser-reachable
- unauthenticated operations must not be `GET`-cacheable

## Pairing And DeviceAuth

Pairing and DeviceAuth are the primary public abuse surface.

The following flows have dedicated rate-limit classes and audit requirements:

- `StartPairing`
- `GetPairingStatus`
- `ExchangePairing`
- `CreateDeviceSession`
- `CreateWebBootstrap`
- `CompleteWebBootstrap`

The policy is intentionally endpoint-class based instead of ad hoc per-handler
logic. Public brute-force and replay attempts must hit deterministic limits
before the handler can issue or consume trust material.

Replay contract:

- Pairing exchange is single-use. Once consumed, the same pairing secret cannot
  issue a second device grant or access session.
- Web bootstrap is single-use. Once consumed, the same bootstrap token cannot
  mint another browser cookie session.
- Device grant refresh claims the grant in the store before issuing access
  material. If rotation is due, the old grant is revoked during that claim so
  parallel refresh attempts with the old grant have only one winner.

## Public Secret Policy

Public profiles require production-strength secrets:

- at least one scoped API token is required
- API tokens must be at least 32 non-default characters
- legacy token sources must be disabled
- public streaming endpoints require a playback decision secret of at least 32 non-default characters

Violations reject startup under public profiles.

## Browser Trust

Public profiles must not use wildcard browser origins.

When a public web endpoint is published, its origin must be present in
`allowedOrigins`. Otherwise web bootstrap is blocked by the deployment
contract, because browser state-changing requests cannot be trusted through a
proxy/tunnel without explicit origin truth.

## Audit Events

Security exposure middleware emits structured audit logs for sensitive
operations without logging tokens or request bodies. Events include:

- event type `security.exposure.audit.v1` or `security.exposure.rate_limited.v1`
- schema `xg2g.public_exposure.v1`
- severity
- operation id
- exposure class
- auth kind
- rate-limit class
- browser trust policy
- HTTP status
- outcome
- decision
- request id
- client IP as resolved by trusted proxy policy
- duration

Sensitive exposure responses also receive `Cache-Control: no-store` so pairing,
device, bootstrap, write, and admin responses are not cached by browser or proxy
layers.

Pairing and DeviceAuth services also emit domain-level audit events for trust
state transitions such as start, approve, exchange, refresh, and web bootstrap
completion.

## Verification

Run:

```bash
go test ./internal/control/authz ./internal/config ./internal/domain/connectivity ./internal/health ./internal/control/http/v3
```

For a live public deployment, also run:

```bash
make verify-public-deployment XG2G_BASE_URL=https://tv.example.net XG2G_API_TOKEN=...
```

The live smoke validates public endpoint selection and effective HTTPS. The
exposure contract validates that sensitive public attempts are classified,
limited, and audited.
