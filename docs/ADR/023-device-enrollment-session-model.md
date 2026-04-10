# ADR-023: Device Enrollment and Session Model

**Status:** Accepted
**Date:** 2026-04-09

## Context

`xg2g` currently mixes multiple authentication shapes across Android, WebView,
and server APIs:

- manual token entry in TV/Web flows
- transient intent-carried auth state in Android
- browser-local token storage in the WebUI
- no first-class backend device registry

That shape is operationally convenient but architecturally weak. It couples
identity, session, and UI bootstrapping, and it invites drift between native
and web clients.

Per [ADR-005](005-Architecture-Invariants.md), the backend must remain the only
source of truth for policy and state transitions. Device identity and session
truth are therefore backend concerns, not client heuristics.

## Decision

### 1. Pairing is enrollment only

Pairing exists only to enroll a new device into backend truth.

Pairing does **not** create a special long-lived authentication mode.

The normative lifecycle is:

`Pairing -> Device Grant -> Access Session`

Never:

`Pairing -> Permanent special-case login`

### 2. Backend-owned auth primitives

The backend owns four distinct primitives:

1. `pairing_attempt`
   - Short-lived enrollment object.
   - Carries `pairing_id`, `user_code`, `pairing_secret`, `expires_at`.
   - Exists only to bridge untrusted device enrollment and authenticated user approval.

2. `device_record`
   - Canonical backend identity for an enrolled device.
   - Contains at least:
     - `device_id`
     - `owner_id`
     - `device_name`
     - `device_type`
     - `policy_profile`
     - `capabilities`
     - `created_at`
     - `last_seen_at`
     - `revoked_at`

3. `device_grant`
   - Long-lived but revocable credential bound to one `device_id`.
   - May bootstrap or renew sessions.
   - Must not be treated as general-purpose full API authorization.
   - Must be rotatable per device.

4. `access_session`
   - Short-lived effective authorization context.
   - Represents the current user/device/policy truth.
   - Native clients may consume it as bearer-style authorization.
   - Web clients consume a derived cookie-backed session, not the native access token itself.

### 3. WebView uses a derived web session

Native-to-web handoff must not leak the native access token into browser
JavaScript, URL parameters, or replayable bootstrap artifacts.

The only allowed pattern is:

1. Native client holds a valid `access_session`.
2. Native client requests a one-time `web_bootstrap_grant`.
3. WebView opens a same-origin bootstrap URL.
4. Backend validates the bootstrap grant, issues an HttpOnly web session cookie,
   redirects into the requested page, and invalidates the bootstrap grant.

Prohibited:

- embedding the native access token in the page URL
- exposing the native access token to browser JavaScript
- durable JS-readable bootstrap tokens

### 4. Clients are session consumers only

Clients may:

- start pairing
- exchange an approved pairing for a device grant
- renew short-lived access sessions
- request a web bootstrap
- consume published endpoint truth

Clients may not:

- invent alternate durable auth schemes
- persist privileged server-wide tokens as the normal product flow
- infer effective policy outside the backend

### 5. Revocation and recovery semantics

The backend must support:

- per-device revoke
- per-device grant rotation
- session invalidation independent of pairing
- re-enrollment after revoke

Client recovery rules:

1. Expired access session -> renew from device grant.
2. Rejected device grant -> transition device to unpaired / re-enroll required.
3. Revoked device -> hard-stop existing sessions and require explicit re-enrollment.

### 6. Scope and policy truth

Scopes, capability flags, UI eligibility, and route permissions attach to the
effective backend session/policy truth, not to the pairing code or to client
storage state.

Pairing can select an initial `policy_profile`, but pairing itself is not the
policy system.

## Consequences

### Positive

- Identity, session, and reachability are explicitly decoupled.
- Native and web flows share one backend session truth.
- Devices become visible, revocable, and auditable.
- TV can avoid raw token entry as a steady-state UX.

### Negative

- Enrollment and session flows require new backend domain objects.
- Existing manual token paths become migration shims, not the target model.
- Web bootstrap introduces strict same-origin and short-lived grant requirements.

## Non-Goals

- Building a second independent auth universe beside the main API/session model
- Treating pairing as an implicit trusted mode
- Allowing browser JavaScript to become a privileged store for native auth material

## References

- [ADR-005](005-Architecture-Invariants.md)
- [ADR-024](024-published-endpoint-connectivity-truth.md)
- [Proposed Device/OpenAPI Contract](/Users/manuel/StudioProjects/xg2g/openapi/device-enrollment-connectivity.proposed.yaml)
- [Android Device State Machine](/Users/manuel/StudioProjects/xg2g/docs/arch/ANDROID_DEVICE_SESSION_STATE_MACHINE.md)
