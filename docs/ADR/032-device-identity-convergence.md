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

### Phase 0 — visibility before change

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
