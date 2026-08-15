# ADR-032: Device Identity Convergence

**Status:** Accepted 2026-08-14. Phase 0 is cleared to start; no schema change yet.
**Date:** 2026-08-14
**Trigger:** Building the native iOS client's device auth (Phase 2B) surfaced that the pairing product path issues credentials with no cryptographic device binding at all.

## Context

The backend contains **two parallel device-auth subsystems**, in two separate
SQLite databases, each defining its own `devices` and `device_grants` tables.

| | `deviceauth.sqlite` | `identity.sqlite` |
| --- | --- | --- |
| Tables | `pairings`, `devices`, `device_grants`, `access_sessions`, `web_bootstraps` | `devices`, `device_grants`, `refresh_token_families`, `access_tokens`, `passkey_credentials`, … |
| Device key binding | **none** — `devices` has no JWK and no thumbprint column | `devices.public_key_jwk NOT NULL`, `devices.jwk_thumbprint NOT NULL UNIQUE` |
| Token binding | **none** — `access_sessions` has no `bound_jkt` | `access_tokens.bound_jkt NOT NULL` |
| Reached by | `/pairing/*`, `/auth/device/session` | `/auth/device/grant/*`, `/auth/device/refresh` |

The pairing path is the one the shipping Android client and the planned iOS
client actually use, and it is unbound end to end:

- `ExchangePairing` (`handlers_pairing.go:181`) passes only `PairingID` and
  `PairingSecret` to the service. It reads no `DPoP` header and no JWK.
- `CreateDeviceSession` (`handlers_deviceauth.go:47`), the refresh endpoint,
  passes only `DeviceGrantID` and `DeviceGrant`. Likewise unbound.
- `jkt` does not appear anywhere in the pairing path.

DPoP binding exists **only** in the passkey family, where
`DeviceGrantFinishRequest` carries `DeviceJWK` explicitly.

Two facts make this invisible in operation rather than loud:

1. The DPoP check in `rbac.go` is a *fallback branch*. When `ValidateProof`
   fails, control falls through to the other authentication mechanisms and the
   request still succeeds over a non-sender-constrained path.
2. The Android client sends `DPoP` headers on API and playback requests
   regardless. They are decorative: the tokens they accompany were never bound.
   (Its proofs are also DER-encoded where the server requires raw `R‖S`; tracked
   separately. Fixing that alone would not make the pairing path bound.)

Meanwhile the iOS client's 2A layer already has a non-exportable Secure Enclave
key, raw `R‖S` ES256 signing, and a `jkt` pinned byte-for-byte to
`identity.ComputeJWKThumbprint`. Wiring it to the pairing path as it stands
would produce proofs nobody reads — the same appearance-of-security this ADR
exists to remove.

## Decision

### Target invariant

> There is **one** canonical device identity, **one** cryptographically bound
> device grant type, and **one** DPoP-bound access token path. Pairing and
> passkey are two *approval front-ends* over the same identity substrate, not
> two stacks.

Concretely:

- `identity` is canonical for `Device`, `DeviceGrant`, `BoundJKT`, refresh
  families and DPoP access tokens.
- The pairing flow keeps its UX unchanged — code, QR payload, approval — but the
  **exchange terminates in `identity`** instead of the `deviceauth` grant model.
- `deviceauth.sqlite` retains only pairing-temporary state: `pairings` and
  `web_bootstraps`. No second durable device identity.
- The exchange requires `deviceJwk`. The server computes the thumbprint itself;
  **a client-supplied thumbprint is never trusted input.**
- Refresh and access-token issuance run exclusively through the bound path.
- There is no permanent "no `jkt` → bearer" fallback.

This is also what makes the intended long-term shape possible without a second
rebuild: replacing "confirm the code in the Web UI" with "confirm with a passkey
on your iPhone" then changes only the *approval* step, leaving the device-grant
and DPoP substrate untouched.

### Legacy grants are not migrated — they are expired

This is a schema-level conclusion, not a preference. A legacy unbound device
cannot be represented in `identity` at all:

- `devices.public_key_jwk NOT NULL` and `devices.jwk_thumbprint NOT NULL UNIQUE`
  — a legacy device has neither, and there is no placeholder that is not a lie.
- `access_tokens.bound_jkt NOT NULL` — same.
- Relaxing those columns would remove precisely the property the convergence is
  for.

Three further incompatibilities rule out a mechanical copy even if the above
were solved:

- **Grant model.** `deviceauth.device_grants` is a single rotating
  `grant_hash`. `identity` models refresh as a *family* with `generation` and
  replay detection (`refresh_token_families`). There is no 1:1 slot.
- **Owner identifier.** `deviceauth.devices.owner_id` holds a *username* —
  `ApprovePairing` falls back to `principal.ID`, and `auth.NewPrincipal` sets
  `ID` to the username (or a `t_<hash>` derivation when empty). `identity`
  requires `user_id` as a foreign key into `users(id)`. The mapping can fail.
- **Time representation.** `deviceauth` stores `*_ms INTEGER`; `identity` stores
  `TIMESTAMP`.

So: **no rows are transformed.** Devices re-pair to obtain a bound identity.
That is what makes this migration non-destructive, and it is why rollback below
is a configuration flip rather than a data restore.

## Migration Plan

Each phase is separately releasable and separately revertible.

### Phase 0 — visibility before change — **implemented**

Shipped as a read-only census: `UnboundInventoryReader.CountUnboundDeviceAuth`,
implemented for both the SQLite and memory backends and tested against identical
expectations, since a census taken in one environment must say something about
the other.

It needs no schema change and no flag. The deviceauth store has no binding
column at all, so today the legacy fleet *is* the entire fleet — counting is
sufficient.

Two views, one source:

- **Startup log** (`bootstrap`) — exactly one aggregated line per process start.
  Empty fleet at `info`, remaining fleet at `warn`, because a fleet with a
  deadline attached is not background information. Never per device, never
  polled.
- **Diagnostics snapshot** (`health.LifecycleRuntimeSnapshot.deviceAuth`), fed
  through `UnboundDeviceAuthCensusFunc` from the same reader. No second counting
  implementation exists to drift out of step.

The census records *last use* alongside the counts. A fleet whose newest use is
months old justifies a different cutoff window from one in daily use, and "never
used" stays distinguishable from "used at epoch 0".

The diagnostics path opens the database through
`OpenUnboundInventoryReader`, which deliberately **skips migrations**: a
preflight report must not upgrade the schema of a live installation as a side
effect.

Still open, deferred to Phase 1 where the OpenAPI contract is touched anyway:
the `/system/health` finding and per-device visibility. `SystemHealth` is a
generated type guarded by the `ui-contract` CI workflow, and **no device list
endpoint exists at all** — per-device visibility needs one built first.

#### Original scope

- Count legacy grants and sessions in `deviceauth.sqlite`.
- Expose the count in the device list, in `/system/health` as a finding while
  it is greater than zero, and in the diagnostics snapshot.
- Emit a log line on each legacy grant use, carrying device id and last-seen.
- **No behaviour change.** The gate for proceeding is knowing how many real
  devices are affected before anything is touched.

### Phase 1 — bound exchange, additive only

- `ApprovePairing` validates that `ownerId` resolves to an existing
  `identity.users(id)` and rejects an unknown owner instead of creating an
  orphan. (Today the field is accepted from the request body unchecked; that is
  admin-scoped and therefore not a privilege hole, but under this ADR it becomes
  a foreign key and must be verified rather than believed.)
- `PairingSecretRequest` gains a required `deviceJwk`, behind a contract-version
  gate.
- The exchange verifies **possession** of the key with a DPoP proof over the
  exchange request itself: the pairing secret carries authorization, the proof
  carries key possession. The server derives `jkt` from the proof's JWK.
- The exchange registers or looks up an `identity.Device`, then issues an
  `identity.DeviceGrant`, a refresh family entry, and a DPoP access token with
  `bound_jkt`.
- The unbound exchange remains reachable only for clients below the gate, and
  only until the cutoff.
- **No schema change to `deviceauth`. Only additive rows in `identity`.**

#### Phase 1 contract scope

Phase 1 is one coherent contract change, not a series of small patches:

1. Register `device_reauth_required` in the problem-code catalogue and the
   OpenAPI `ProblemCode` schema.
2. Fix the status code. `409 Conflict`, so a client cannot fall into refresh
   logic — the device state conflicts with the security requirement.
3. Define a **structured** pre-cutoff field — a device/auth state on successful
   responses or in session metadata. Ad-hoc headers are explicitly rejected.
4. Wire `deviceBindingPolicy()` to real configuration including the cutoff time.
5. Add `deviceJwk` to the pairing exchange contract, starting the convergence
   onto `identity`.
6. Only then wire the iOS `RequestAuthorizer` for real.

##### Pre-cutoff state metadata

Defined once as reusable OpenAPI header components, produced in exactly one
place — the auth middleware — so no endpoint invents its own warning field:

```
XG2G-Device-State: bound
XG2G-Device-State: legacy_unbound
XG2G-Device-Repair-Required-By: 2026-09-01T00:00:00Z
```

**Two scalar headers, not one compound value.** A value such as
`legacy_unbound; repairRequiredBy=…` reads compactly but is a private
mini-grammar: OpenAPI can only describe it as a regex, and every client has to
implement a parser for it. That is a string-based shadow API, which is precisely
what defining a contract type is meant to remove. Split, each header is a scalar
the contract expresses natively — `DeviceBindingState` as a closed enum and the
deadline as `format: date-time` — and no client parses anything.

The Go constants and the contract enum are tied together by a test that reads
`api/openapi.yaml`, in the same way `problemcode` guards its own registry.
Without it the semantics drift straight back into strings.

It is **state metadata, not a warning error**. The request succeeded; the header
is idempotent, carries no retry semantics, and a client that ignores it behaves
exactly as before. Its only purpose is to let a client move its own state
machine to "re-pair by <date>" *before* the cutoff, rather than inferring its
situation from a 401 or 409 afterwards. On iOS that means the session lifecycle
can hold a real `repairRecommended(deadline)` state and later switch to
`repairRequired(deadline)`, with no guessing from status codes.

The value is derived **solely** from the `devicebinding.Decision` already taken
during authentication. `auth.AuthenticatedDevice` carries that decision even
when it allowed the request, precisely so the transport never evaluates the
policy a second time — a second evaluation could disagree with the one that
later produces the 409, which is the same class of bug as two URL normalizers.

Two deliberate silences:

- **No header at all when no device-binding decision took part.** An admin token
  or a web session is not a paired device, and reporting `bound` for it would
  assert a property that was never established. Absence means "not a
  paired-device session", never "unbound".
- **No deadline when no cutoff is configured.** The state is still reported; the
  date is not invented.

Remaining for the contract: declaring the header in `api/openapi.yaml` as a
reusable response header so it is specified rather than merely implemented.

**`misconfigured_token` must not be mapped to `401` either.** A valid token that
its own configuration grants no scopes is a server-side deployment fault. Behind
a 401 it reads to the client as "your credentials are broken" and sends the
investigation to entirely the wrong place. It belongs on a 5xx or its own
internal problem code. The typed outcome exists precisely so this information
survives the HTTP boundary; mapping every non-success back onto 401 would throw
it away again at the last step.

### Phase 2 — bound refresh

- `/auth/device/session` requires a DPoP proof whose thumbprint matches the
  grant's `bound_jkt`.
- Unbound refresh continues to work until the cutoff, marked `legacy_unbound`
  and logged on every use.

### Phase 3 — enforcement

- At the cutoff, unbound exchange and unbound refresh return a problem document
  (`auth/device_binding_required`) rather than degrading.
- Legacy rows stay readable for audit but issue nothing.

### Phase 4 — removal

Only after the bound chain is proven end to end on both clients: remove the
`deviceauth` device/grant/session tables and their code path. `deviceauth.sqlite`
keeps `pairings` and `web_bootstraps`.

## Rollback And Recovery

- **Phases 1–3 rewrite no existing rows.** Rollback is flipping the contract
  gate off; there is nothing to restore. Identity rows written by phases 1–2 are
  inert while the gate is off, because the legacy path does not consult them.
- **Phase 4 is the only destructive step** and ships as its own release, never
  bundled with 1–3. It is preceded by a backup of both SQLite files with
  recorded checksums and a rehearsed restore procedure.
- **User-visible recovery** from any failure in phases 1–3 is *re-pair* — the
  same action as the normal migration path. That is a direct argument for making
  the pairing UX good *before* the cutoff, not after.
- Each phase carries its own health finding, so a partially rolled-out fleet is
  observable rather than inferred.

## Operational Visibility

`legacy_unbound` must not be only a database flag, or the legacy path survives
indefinitely by being invisible. It must appear in:

- the device list (per-device field, not a footnote),
- `/system/health` as a finding while the count is greater than zero,
- the diagnostics snapshot,
- a log line on every legacy grant use.

## Consequences

- The iOS client can be built against the bound contract from the start; its
  `RequestAuthorizer` becomes a real DPoP authorizer rather than a seam.
- Android must ship its DER→raw signature fix before it can pair under the new
  contract. Until it does, its devices remain legacy and must re-pair with a
  fixed build. Its DPoP interceptor becomes meaningful for the first time.
- `storageinventory.go` currently describes `deviceauth.sqlite` as holding
  "device binding state", which is not true today. After phase 4 it will be.

## Decisions

### Cutoff window: 14 days, shortenable to 7

Fourteen days from the release in which bound pairing becomes available. During
that window existing `legacy_unbound` devices keep working visibly, and every
legacy code path is marked as such. If Phase 0 shows the affected fleet is only
a handful of the maintainer's own devices, the window shortens to 7 days.

A 30- or 60-day window is explicitly rejected: that is the length at which a
transitional path stops being transitional.

### Rollout order: the Android fix comes before the binding gate

The backend must not reach a state where new DPoP-bound grants are assumed in
production while the shipping Android client demonstrably still emits DER
signatures the server cannot verify. That would be a deliberate platform break
called a migration.

1. Merge the Android DER→JOSE signature fix and verify it on the production path.
2. **Phase 0** — count the legacy fleet and make it visible.
3. Implement backend B2 behind a feature/contract gate (gate off).
4. Finish iOS 2B against the new bound path.
5. Move Android onto the same identity/DPoP path.
6. Activate the gate: no new unbound grants from this point.
7. Start the cutoff clock (14 days, or 7 per Phase 0).
8. Disable the legacy path.
9. Destructive removal of the old tables and code — separate, later release.

iOS development continues in parallel throughout; only the parts that depend on
the final exchange contract wait for step 3.

### `device_reauth_required` semantics — decided centrally

The decision lives in `internal/domain/devicebinding`, a pure domain package
with no HTTP, no store and no clock of its own. It is **one** decision, not a
per-endpoint one: otherwise six months from now some endpoints answer 409,
others 401, and others still quietly accept legacy credentials. Transports map
this decision; they do not make it.

`Evaluate(state, policy, now)` yields exactly three outcomes:

| Outcome | Meaning |
| --- | --- |
| `allow` | Bound device. Proceed. |
| `allow_with_repair_warning` | Legacy, before the cutoff. The request **succeeds**, and the response must carry that this device will stop working — so it learns its fate before it fails, not by failing. |
| `deny_repair_required` | Legacy, at or after the cutoff. Refuse; re-pairing required. |

**Denial must not be modelled as `401`.** A client seeing 401 concludes "token
missing or stale, refresh and retry". Here the session is not accidentally
broken: the device is knowingly in an expiring security model, and no amount of
refreshing will help. `409 Conflict` fits better — the device's current state
conflicts with the new security requirement — but the exact number matters less
than the invariant: **a client must never feed this into refresh/retry logic;
it must enter a re-pair-required state.**

Two safety properties are encoded and tested:

- **An unconfigured cutoff can never deny.** A zero `CutoffAt` means "no cutoff
  configured", never "cutoff at the Unix epoch". A policy nobody set up must not
  lock out every device on the first request.
- **An unknown state is never promoted to bound.** Anything not explicitly bound
  is treated as legacy. Assuming security that was never established is the one
  direction this must not fail in.

Deferred to Phase 1, where the OpenAPI contract is opened anyway: registering
the `device_reauth_required` problem code (the `problemcode` package is
contract-tested against the spec), choosing the status code, and adding the
structured pre-cutoff warning field. Inventing an ad-hoc response header for the
warning is explicitly rejected — the warning belongs in the contract as an
explicit device-state field.

### Legacy clients must be told, not silently cut off

A device that will stop working has to learn that **before** the cutoff, through
an explicit `reauth_required` / `repair_required` signal on a legacy path — not
through a silent `401` once the window closes.

This is a correctness requirement, not polish. An expiry that surfaces as an
anonymous authentication failure is indistinguishable from a login defect, and a
security migration that looks like a random outage loses exactly the trust it was
meant to build. Phase 0 supplies the device inventory that makes targeted
notification possible; the signal itself ships with Phase 2, so it is live for
the whole cutoff window rather than at its end.

### `legacy_unbound` is never represented in `identity`

Confirmed as designed. Where the target tables require JWK, thumbprint and
`bound_jkt`, "not migratable" is a feature. Re-pairing explicitly beats
simulating security with placeholder values.
