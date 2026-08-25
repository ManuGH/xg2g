# ADR-032: Device Identity Convergence

**Status:** Accepted 2026-08-14. Phases 0–2 implemented. Phase 3 superseded by a
hard cutover (see *The cutoff never happened*). Phase 4 outstanding — it is the
only remaining work.
**Date:** 2026-08-14. Revised 2026-08-25 against the implemented state.
**Trigger:** Building the native iOS client's device auth (Phase 2B) surfaced that the pairing product path issues credentials with no cryptographic device binding at all.

## How to read this document

The **Context** below is a finding dated 2026-08-14, not a description of the
system today. It is kept verbatim because the decision only makes sense against
the state that provoked it; *What changed since* corrects each claim that has
since stopped being true.

The **Decision** and its target invariant still hold and are largely reached.
The **Migration Plan** does not: its phased cutoff was replaced by a hard
cutover once Phase 0 reported what the affected fleet actually was. Every phase
and sub-decision carries its status inline.

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

### What changed since

Each of the three findings above has been fixed, and the text is left standing
because a reader who meets the code first needs to know it once read otherwise.

- **The exchange is bound.** `PairingSecretRequest` declares `deviceJwk` as
  required with `additionalProperties: false`, and `ExchangePairing` passes it
  into the service
  (`internal/control/http/v3/handlers_pairing.go`). The server derives the
  thumbprint itself through `identity.ValidateEnrollmentJWK`
  (`internal/control/http/v3/pairing/service.go`); a client-supplied thumbprint
  is still never read.
- **`CreateDeviceSession` is gone**, along with `handlers_deviceauth.go` and
  `/auth/device/session`. Refresh runs only through `/auth/device/refresh`,
  which the contract declares with a required `DPoP` header, so its presence is
  enforced by the generated wrapper rather than by the handler remembering to
  check.
- **`jkt` is now central to the pairing path**, which is the whole point: it is
  what makes the grant the exchange issues the same kind of grant the passkey
  family issues.

What has *not* changed is the shape of the fall-through in `rbac.go`: a failed
`ValidateProof` still drops through to the remaining mechanisms. It no longer
lands anywhere unbound, because the credential class it used to land on —
`deviceauth` access sessions — is no longer issued by any production path. The
tables are read and counted, never written.

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

### One identity domain, several correct authentication mechanisms

The goal is **not** that every client authenticates the same way. Forcing a
browser through a device-key model, or reimplementing Secure Enclave semantics
in JavaScript, would be worse security theatre than the problem being fixed.

| Client | Identity | Authentication |
| --- | --- | --- |
| iOS / Android / tvOS | a registered *device* | device key + grant + DPoP |
| Safari / Chrome / Firefox | a *user* and their browser session | passkey/WebAuthn + HttpOnly, Secure, SameSite cookie |
| Web UI approving a new device | the user authorises a device | browser confirms the pairing; the device then holds its own DPoP grant |

Each layer answers a different question: a **passkey identifies the human**,
**pairing authorises a new device**, the **Secure Enclave / Android Keystore
identifies that device**, and **DPoP binds its tokens to that key**.

The separation is a feature of this design rather than a compromise. When the
native iOS app is paired, Safari can serve as the approval surface without
Safari ever seeing the app's device key, and without the app ever seeing the
browser's cookies:

```
native app (Secure Enclave key) ──pairing code──▶ Safari (passkey login)
                                                        │ approve
                                                        ▼
                                                   exchange
                                                        ▼
                                        identity device + DPoP grant
                                                        ▼
                                                 app authenticated
```

A PWA or installed web app is treated as a browser client until proven
otherwise; it is not the native app with a different wrapper.

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

## The cutoff never happened

Phases 1–3 below were written around a migration window: a contract-version
gate, a warning state, deadline headers, a configurable cutoff, and a legacy
path kept reachable until it expired. None of that shipped, and its absence is
a decision rather than an omission.

Phase 0 is what changed the answer. It was built to make the legacy fleet
visible before anything was touched, and what it reported was that the fleet
was **stale**: no active grants, last use months old. A transition period
protects the devices that are still in use. There were none. What it would have
cost was real and permanent-until-removed: a cutoff policy with reload
monotonicity rules, a third `allow_with_repair_warning` outcome threaded
through the domain, deadline headers in the contract, client state machines for
`repairRecommended` versus `repairRequired`, and later the work of taking all
of it out again.

So the cutover is hard. A credential is bound to a device key or it is not, and
an unbound one is refused. This is stated once, in the package that owns the
decision (`internal/domain/devicebinding`), and the rest follows from it:

- `Evaluate(state)` yields **two** outcomes — `allow` and
  `deny_repair_required`. It takes no policy and no clock, because with no
  cutoff there is nothing for either to decide. The three-outcome table further
  down is superseded.
- `device_reauth_required` was never registered as a problem code and no status
  code was chosen for it. Both were Phase 1 work items *for the warning path*,
  and there is no warning path.
- `XG2G-Device-State` shipped anyway, and is worth keeping: it reports `bound`
  or `legacy_unbound` as state metadata on successful responses, produced once
  in the auth middleware, so a client can show "this device is paired" without
  reading it out of a status code. Its deadline companion header does not
  exist — there is no deadline to report.

The Phase 0 census stays in place. It is now a regression detector rather than a
countdown: a number that should be zero and stay zero.

**Still open in the contract.** `DeviceBindingState` and the `Xg2gDeviceState`
header component are declared in `api/openapi.yaml`, but no operation
references the header in its responses. It is implemented and emitted; it is
not yet specified where a client would look for it. That is the one part of the
Phase 1 contract scope that remains genuinely unfinished rather than
superseded.

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

### Phase 1 — bound exchange — **implemented, without the gate**

Shipped as the contract's only shape rather than behind a version gate: with the
fleet stale there was no client below the gate to keep serving. `deviceJwk` is
required, the owner is resolved against `identity.users(id)`, and the exchange
terminates in an `identity` device, grant, refresh family and DPoP access
token. The bullets below describe the gated variant that was planned.

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

### Phase 2 — bound refresh — **implemented**

- `/auth/device/session` requires a DPoP proof whose thumbprint matches the
  grant's `bound_jkt`.
- Unbound refresh continues to work until the cutoff, marked `legacy_unbound`
  and logged on every use.

### Phase 3 — enforcement — **superseded**

There is no cutoff to enforce at. Unbound credentials are refused from the
moment the bound path shipped, which is what the hard cutover means.

- At the cutoff, unbound exchange and unbound refresh return a problem document
  (`auth/device_binding_required`) rather than degrading.
- Legacy rows stay readable for audit but issue nothing.

### Phase 4 — removal — **outstanding**

The only remaining work in this ADR. `deviceauth` still creates `devices`,
`device_grants` and `access_sessions`, and the code path that reads them is
still compiled in. Nothing writes them: the production writers are gone, and
what is left are readers and the census. Removing them is a destructive step
and ships on its own, as described below.

Only after the bound chain is proven end to end on both clients: remove the
`deviceauth` device/grant/session tables and their code path. `deviceauth.sqlite`
keeps `pairings` and `web_bootstraps`.

## Rollback And Recovery

- **Phases 1–2 rewrite no existing rows.** They only add rows in `identity`.
  ~~Rollback is flipping the contract gate off~~ — there is no gate; with the
  hard cutover, rollback is reverting the release. Nothing has to be restored
  either way, because nothing was overwritten.
- **Phase 4 is the only destructive step** and ships as its own release, never
  bundled with 1–2. It is preceded by a backup of both SQLite files with
  recorded checksums and a rehearsed restore procedure. It has not shipped.
- **User-visible recovery** from any failure is *re-pair* — the same action as
  the normal migration path. Without a cutoff window this is no longer an
  argument for fixing the pairing UX *first*; it is the only recovery there is,
  which is a stronger reason to keep it good.
- Each phase carries its own health finding, so a partially rolled-out fleet is
  observable rather than inferred. With the fleet at zero, the Phase 0 census is
  the finding that matters: it should read zero and keep reading zero.

## Operational Visibility

`legacy_unbound` must not be only a database flag, or the legacy path survives
indefinitely by being invisible. It must appear in:

- the device list (per-device field, not a footnote),
- `/system/health` as a finding while the count is greater than zero,
- the diagnostics snapshot,
- a log line on every legacy grant use.

## Device Self-Revocation

A device ends its own enrollment through `POST /api/v3/auth/device/revoke`,
authenticated by its live DPoP-bound credential.

**The endpoint accepts no device identifier.** The calling device is read from
the authenticated principal, where it arrives via the binding
`ValidateDPoPAccessToken` enforces (the access token's `bound_jkt` must equal
the proof's thumbprint). A body field would be a request to trust the caller
about who it is; removing the field is a stronger guarantee than validating one,
because there is then nothing left to get wrong.

The device identity travels on `auth.Principal.DeviceID`, set only for
device-bound credentials. A handler cannot re-derive it by validating the proof
a second time: `jti` is replay-cached, so the second validation is
indistinguishable from an attack and is refused.

Revocation retires every credential the device holds — access tokens, refresh
token families, device grants — in one transaction. A partial revocation is the
worst available outcome, because it reports success to a client that is about to
destroy the only key that could have revoked it. The `devices` row survives, so
the revocation stays auditable and the enrolled public key cannot be silently
reused by a new grant.

Removing *another* device remains a household operation with a different
authority. This endpoint cannot express it.

### Client ordering

Remote first, local second — the same rule that keeps `CredentialStore` from
offering a local `revoke`:

1. call the endpoint with the live credential,
2. wait for 204, or for 401 meaning the server has already forgotten this device,
3. only then clear credentials and destroy the device key.

Anything else — a transport failure, a 5xx — destroys nothing and is safe to
retry. Destroying the key first would leave a grant that is valid on the server
and no longer revocable from that device, curable only by an admin.

## Consequences

- The iOS client can be built against the bound contract from the start; its
  `RequestAuthorizer` becomes a real DPoP authorizer rather than a seam.
- Android must ship its DER→raw signature fix before it can pair under the new
  contract. Until it does, its devices remain legacy and must re-pair with a
  fixed build. Its DPoP interceptor becomes meaningful for the first time.
  **Shipped** — `auth/ES256Signature.kt` converts the DER `SEQUENCE { r, s }`
  the Keystore emits into the raw 64-byte `R‖S` a spec-compliant verifier
  requires.
- `storageinventory.go` currently describes `deviceauth.sqlite` as holding
  "device binding state", which is not true today. After phase 4 it will be.

## Decisions

### Cutoff window: 14 days, shortenable to 7 — **superseded**

The reasoning anticipated its own end: *"If Phase 0 shows the affected fleet is
only a handful of the maintainer's own devices, the window shortens to 7 days."*
Phase 0 showed less than that — a fleet with no active grants at all — and the
window shortened to zero. No cutoff is configured because none is needed.

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
7. ~~Start the cutoff clock (14 days, or 7 per Phase 0).~~ — not run; Phase 0
   found nothing to wait for.
8. ~~Disable the legacy path.~~ — folded into step 6: with no window, activating
   the bound path *is* disabling the unbound one.
9. Destructive removal of the old tables and code — separate, later release.
   **This is Phase 4 and is still outstanding.**

iOS development continues in parallel throughout; only the parts that depend on
the final exchange contract wait for step 3.

### `device_reauth_required` semantics — decided centrally — **revised**

The placement holds and is implemented: the decision lives in
`internal/domain/devicebinding`, transports map it and do not make it. The
outcome set does not hold — there are two, not three, because the middle one
existed only for the cutoff window. `Evaluate` also takes neither policy nor
clock. The table below is the superseded version.

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

### Legacy clients must be told, not silently cut off — **superseded**

This argued that an expiry surfacing as an anonymous `401` is indistinguishable
from a login defect. That is still true, and it is why `XG2G-Device-State`
shipped: a client can see `legacy_unbound` on a successful response and say so
plainly, rather than inferring it from a failure. What did not ship is the
advance warning with a deadline, because there was no window during which a
legacy device kept working — Phase 0 found none still in use to warn.

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
