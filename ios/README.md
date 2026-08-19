# iOS Client

Xcode project for the `xg2g` iOS client. Sibling of `android/`, same server, same
wire contract — but deliberately not a line-by-line port of it.

## Architecture

Native first. The target shape is:

```
SwiftUI app -> native screens -> native API/auth layer -> native playback layer -> backend
```

and explicitly **not** a `WKWebView` wrapper around the existing Web UI. A web
view may later appear as an isolated `WebContentView` for admin or rarely-used
configuration pages, but it is not the UI foundation.

This is a deliberate departure from `android/`, which hosts the Web UI and adds
native pieces around it. The cost is duplicated product logic, which
`android/README.md` names as something to avoid; it is accepted here to gain
player control, PiP, background audio, lock-screen/remote integration, native
gestures, predictable lifecycle handling, and a clean tvOS base.

One boundary is **not** negotiable: playback *decisions* stay server
authoritative. The client reports honest capabilities and obeys the decision
token; it never re-decides codec or container locally. Per
[ADR-026](../docs/ADR/026-native-webkit-hls-hevc-copy.md) the decision token's
`CapHash` covers `preferredHlsEngine`, and changing the engine after
`/live/stream-info` is rejected with `CLAIM_MISMATCH`. Native UI, yes; native
playback policy, no.

## Status

| Phase | Scope | State |
| --- | --- | --- |
| 1 | Project skeleton, server target resolution, unit tests, Swift 6 | done |
| 2A | `ServerOrigin`, `ServerAddress`, `ServerIdentity`, URL consolidation, attack cases | done |
| 2A | `CredentialStore`, `DeviceKeyStore`, `APIClient` | done — **2A closed** |
| 2B | Native device auth: pairing, session refresh, DPoP via Secure Enclave | open |
| 2C | Native app shell: SwiftUI navigation, app state, setup, base layout | open |
| 2D | First real screen (bouquets / channel list) against a live backend | open |
| 3 | Playback: `AVPlayer`, intents/sessions/heartbeat, PiP, background audio, now-playing — plus `ios_native` in the backend | open |
| 4 | EPG, timers, recordings | open |
| 5 | tvOS | open |
| 6 | Mac Catalyst | open |

The project builds in **Swift 6 language mode** with full strict concurrency,
adopted while the source surface was still small enough that the migration cost
nothing. Keep it that way rather than reverting to Swift 5 to silence an
isolation error.

## Build And Test

No project generator and no extra toolchain is required. The app target uses an
Xcode *synchronized folder group*, so new Swift files under `Xg2g/` are picked up
without touching `Xg2g.xcodeproj`.

```bash
xcodebuild -project ios/Xg2g.xcodeproj -scheme Xg2g \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' build
```

```bash
xcodebuild -project ios/Xg2g.xcodeproj -scheme Xg2g \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test
```

No signing identity is required: the simulator signs ad-hoc ("Sign to Run
Locally") on its own. Running on a physical device needs an Apple ID configured
once in Xcode under *Settings → Accounts*; a free account is enough for 7-day
builds.

> **Do not add `CODE_SIGNING_ALLOWED=NO`.** It disables signing entirely, and an
> unsigned app has no Keychain entitlement — every `SecItem` call then fails
> with `errSecMissingEntitlement` (`-34018`). It looks harmless because pure
> logic tests still pass; `SecItemKeychainBackendTests` is what catches it.

## Layout

- `Xg2g/` application sources (synchronized group — no project edit to add files)
- `Xg2gTests/` unit tests (synchronized group)
- `Support/Info.plist` bundle configuration for **Release**
- `Support/Info-Debug.plist` bundle configuration for **Debug**

## App Transport Security

A self-hosted xg2g server is usually reached over the LAN, so development needs
cleartext to a local address. That exemption is confined to Debug by build
configuration, not by convention: `INFOPLIST_FILE` differs per configuration, so
a Release build reads a plist that does not contain the key at all and therefore
cannot ship the exemption. It is `NSAllowsLocalNetworking`, never
`NSAllowsArbitraryLoads`.

`NSLocalNetworkUsageDescription` stays in both, because reaching a LAN server
triggers the local-network permission prompt even over HTTPS.

Verify against the built bundles rather than the sources:

```bash
plutil -extract NSAppTransportSecurity xml1 -o - \
  "$(ls -d ~/Library/Developer/Xcode/DerivedData/Xg2g-*/Build/Products/Release-iphonesimulator)/Xg2g.app/Info.plist"
```

A Release bundle must answer `No value at that key path`.

## URL And Identity Layer

Five types, each with exactly one job. There is no second implementation of any
of them — a duplicated normalizer is how the Android client ended up with two
disagreeing opinions about one URL.

| Type | Holds | Job |
| --- | --- | --- |
| `ServerOrigin` | scheme, host, effective port | Canonical origin. Comparing two values **is** the same-origin check |
| `ServerAddress` | origin + deployment root path | Derives every endpoint; owns containment |
| `ServerIdentity` | `.address` or `.instance` | What credentials are bound to |
| `URLPath` | — | Percent-encoded path canonicalization |
| `DeepLinkParser` | — | Reads `xg2g://` onboarding links. No URL semantics of its own |

### Invariant: endpoints are derived from the root, downward

**Every endpoint is derived from the stored deployment root. The API is never
reconstructed from the UI path, and the UI is never reconstructed from the API
path.** Anything that needs a new endpoint adds a derivation on `ServerAddress`;
nothing joins onto another endpoint's URL.

### The root is stored, not the Web UI path

The backend mounts `/api/v3` and `/ui` as **siblings** at the deployment root.
This client therefore persists the *root* and derives `apiBaseURL` and
`webUIURL` downward from it. Nothing is ever reconstructed sideways out of
another endpoint's path.

Android persists the UI base (`https://host/ui/`) instead and has to
reconstruct the API from it. Its two transports once disagreed about how:
one replaced the path with `/api/v3/`, the other appended segments and so
requested `https://host/ui/api/v3/...`, reaching the SPA handler rather than
the API router. Both now go through the origin-rooted `apiV3Url`, but only
because a shared helper was introduced — the reconstruction itself is what
made two answers possible. Storing the root removes the entire class of
mistake instead of settling it.

### Two parsers, one boundary

`parseUserEntered` is for text a human typed and performs exactly **one**
repair: supplying a missing `https://`. A declared scheme is validated, never
rewritten — `ftp://host` is rejected rather than turned into something else.

`parseTrusted` is for persisted values and trusted backend responses and repairs
nothing; a scheme-less string is refused.

They are deliberately two functions rather than one with a flag, so no later
internal call can take the convenient route and have a machine-supplied string
reinterpreted instead of validated.

Where the input is genuinely ambiguous — a user pasting `https://host/ui/` out
of their browser — the parser does not guess. `ServerAddress.rootCandidates`
hands the alternatives to the setup flow, which resolves it by asking the server
rather than by assuming.

### Credential binding: never silently weakened

`ServerIdentity` has two cases and they are not interchangeable. `.instance` is
the **stronger** binding: it distinguishes two deployments sharing an origin and
survives a hostname change. `.address` is what can be known before pairing, and
against a backend that does not mint an instance identifier — which is the case
today, so `.instance` is currently unreachable in practice. See *Backend Notes*.

`ServerIdentity.reconcile(bound:observedInstance:)` is the only way to change a
binding, and **no outcome returns an identity weaker than the one bound**:

| Bound | Server reports | Outcome |
| --- | --- | --- |
| `.address` | nothing | unchanged |
| `.address` | an ID | **upgraded** — caller must re-key stored credentials |
| `.instance` | the same ID | unchanged |
| `.instance` | a *different* ID | **conflict** — different installation, credentials must not be reused |
| `.instance` | nothing | **binding kept**, explicitly not a downgrade |

The last row is the reason the type exists. A missing value is not evidence of a
different server, so treating it as one would let a transient failure
permanently weaken a binding while looking like a successful fallback. An
exhaustive test asserts that no input to `reconcile` lowers the strength.

`InstanceID` is validated on the way in — length bounds and an
`[A-Za-z0-9._-]` charset — because a server-supplied string that becomes part of
a Keychain account key must not be able to carry separators or escape the key
format.

### URL containment: never decode

`ServerAddress.contains(_:)` is the single containment check, and it **never
decodes**. `%2e%2e`, `%2f` and `%5c` are not traversal for `URLSession` or for
the server, so treating them as traversal here would be a second parser opinion
— exactly the differential this layer exists to prevent. Being "more careful"
than the network stack is not safer, it is merely different, and different is
the bug.

`Xg2gTests/URLContainmentAttackTests.swift` asserts twice per case: that
`URLRequest` carries the URL byte-for-byte unchanged, and what the verdict is.
The first assertion is what keeps the second honest — if Foundation ever starts
rewriting one of these, the test fails before the verdict silently becomes a
lie. Measured today: Foundation rewrites none of them.

Covered: real dot-segment traversal (rejected), harmless `.` and in-root `..`
(accepted), encoded slash and backslash as non-separators, encoded dots in
lower, upper and mixed case, double-encoded dots, prefix-confusion siblings
(`/xg2gX` against root `/xg2g/`), scheme/port/host mismatches, equivalent origin
spellings, userinfo disguise, and Unicode hosts.

Treat this layer as **closed**. It should not be reopened at each auth step; new
endpoints are added by derivation on `ServerAddress`, not by new URL handling.

### Deep links configure less than on Android

Only the `xg2g://` scheme can configure a server, and its `base_url` goes
through the **strict** parser as a deployment root. An `https://` link
configures nothing at all.

This is a **security invariant**, not a convenience: *only* `xg2g://` may change
server configuration. It is deliberately narrower than Android, which accepts an
https deep link as a base URL behind an `isServerSwitch` confirmation — leaving a
dialog as the only thing between an arbitrary web page and the choice of host
this client sends credentials to. If universal links are added later they may
open content within an already-bound server; they must never bind a new one.

#### `base_url` carries a UI URL, not a root

On the wire, `base_url` is the **Web UI URL**. The link is minted by the Web UI,
not the backend: `apps/webui/src/components/Settings.tsx` builds
`new URL('/ui/', endpoint)`, yielding `https://host/ui/`.

Rather than let that spread, the legacy transport form is accepted at the edge
and converted to a deployment root exactly once, inside
`DeepLinkParser.configuredAddress`. Past that boundary nothing deals in UI URLs.
A link that already carries a root passes through unchanged, so both forms work
without callers knowing which they got. Compatibility at the edge, one model in
the core.

There is a second link shape the backend itself mints —
`xg2g://pair?pairing_id=…&user_code=…` from
`backend/internal/control/http/v3/pairing/service.go` — which carries no
`base_url` and is handled in 2B.

### Credential keys are a storage concern, not a domain concern

`ServerIdentity` deliberately does **not** know how to serialize itself.
`CredentialKeyEncoding` owns that, so domain logic never depends on an Apple
storage format and a storage change cannot become a domain change. The format is
versioned; changing it is a migration with a reader for the old shape, not an
edit.

All secrets live under one owned Keychain service. That is what makes a
wipe-on-reinstall possible at all: at first launch after a reinstall the app does
not yet know which identities were stored, so it can only purge by service rather
than by enumerating them.

## Credential Storage

`CredentialStore` exposes **only** typed, domain-level operations — "the grant
for this identity", "this session was refreshed". **No caller ever sees a
storage key or a namespace string.** Without that rule `CredentialKeyEncoding`
protects nothing: the representation leaks out through whoever composes a key by
hand, and the format becomes load-bearing everywhere.

Keychain items are written `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`
and non-synchronizable. *AfterFirstUnlock* rather than *WhenUnlocked* because
refresh and heartbeat must survive a locked screen once background audio exists;
*ThisDeviceOnly* because a device-bound grant that rides an iCloud Keychain sync
or an encrypted backup onto a second device is no longer device-bound. Both
attributes are **read back from the Keychain** in
`SecItemKeychainBackendTests` — a security property asserted in a comment is not
asserted at all.

### Fresh-install purge

The Keychain outlives app deletion; `UserDefaults` does not. `prepareForLaunch()`
uses that asymmetry: a missing installation marker means a fresh install looking
at a previous installation's credentials, so everything under this app's
Keychain service is purged before anything is read. Purging by *service* is not
laziness — at first launch the app does not know which identities were ever
stored, so enumerating them is impossible. Reads and writes throw
`.notPrepared` until this has run.

### Three distinct lifecycle operations

| Operation | Effect |
| --- | --- |
| `endSession` — log out | Drops the session, keeps the pairing |
| `forgetServer` | Drops every credential for that identity, keeps the device key |
| *revoke device* | **Not offered here** |

Revocation is remote-first: revoke server-side, wait for success or a defined
terminal failure, and only then destroy local material. A local-first `revoke`
on the store would be an attractive nuisance — the key needed to authenticate
the revocation would already be gone, leaving a grant that is still valid on the
server and no longer revocable from this device. That sequencing belongs to the
auth coordinator, which owns the network call.

Nothing here touches the DPoP device key; that is `DeviceKeyStore`'s property
with its own lifecycle, so clearing credentials can never destroy a
cryptographic identity.

### Migration writes before it deletes

`migrate(from:to:)` re-keys credentials after an identity upgrade. New entries
are written before old ones are removed, so an interruption leaves a harmless
duplicate rather than no credentials at all.

## Device Key

`DeviceKeyStore` owns the device's cryptographic identity, kept strictly apart
from `CredentialStore`: a grant is something the server issued, a key is
something this device *is*. Clearing credentials never destroys it.

Three invariants:

1. **Provenance is in the API.** `hardwareBacked` and `software` are distinct,
   and every attestation carries one. A caller is never handed "a key".
2. **A software key never silently substitutes for a hardware key.** It exists
   only under an explicit `DeviceKeyPolicy.allowSoftware(reason:)`. That case
   sits behind `#if targetEnvironment(simulator)`, so a real iOS build cannot
   construct it — a development convenience cannot become a production
   weakening later, because the code allowing it is not compiled into the
   product. Provenance is re-checked on **every** signature, not just at
   creation, so a key made under a permissive policy stops working when the
   policy tightens.
3. **The private key never leaves the type.** The surface is `sign(_:)`. No
   `SecKey`, no key material, no Security-framework object is handed out.

This is deliberately not a "DPoP provider": only the non-exportable key lives in
the Secure Enclave. JWT assembly, `jti`, `htm`/`htu`, `ath` and any nonce
handling are ordinary software concerns in a layer above.

### Signatures are raw R‖S, not DER

`SecKeyCreateSignature` returns ASN.1 DER (68–72 bytes for P-256). JWS ES256
requires the raw concatenation `R ‖ S`, 32 bytes each, and the server enforces
exactly that — `verifyES256Signature` in
`backend/internal/control/http/v3/dpop/dpop.go` rejects any other length
outright. `ECDSASignature.rawFromDER` converts, including stripping DER's sign
padding and **left-padding a short R or S**; forgetting the pad yields a 63-byte
signature that fails for roughly one signature in 256. Tests assert the length
over many signatures and verify the result against the reported public key.

The JWK thumbprint is pinned against an independently computed oracle rather
than described, because member order or whitespace drift changes `jkt` and makes
the server reject every proof.

### Measured: the simulator has a Secure Enclave

On Apple Silicon under Xcode 26, the iOS simulator **does** provide a Secure
Enclave — `kSecAttrTokenIDSecureEnclave` key creation succeeds. The widely
repeated "no Secure Enclave on the simulator" is out of date.

That has a testing consequence worth keeping in mind: gating the
"software key is rejected under a hardware policy" test on real availability
made it silently no-op here. `SecureEnclaveDeviceKeyStore` therefore takes an
injectable `secureEnclaveProbe` so a software key can be forced on any host. The
seam cannot weaken anything — it only decides what kind of key is *created*,
while the policy remains the sole gate on what may be *used*.

## API Client

`HTTPAPIClient` owns requests, JSON, status codes, timeouts and error mapping —
and nothing else. It sits on the finished pieces: `ServerAddress` supplies the
URLs, `CredentialStore` supplies credentials through typed operations,
`DeviceKeyStore` signs bytes. It never learns a credential key name, a Secure
Enclave detail, or a playback decision.

Paths are **relative to the API base** (`"services/bouquets"`). Callers never
build absolute URLs, so the deployment root stays the single source of truth.

### Four distinct failure categories

Collapsing failures into one "network error" is what makes bugs like the Android
one invisible, so the categories stay separate:

| Category | Meaning |
| --- | --- |
| `.transport` | Nothing came back — offline, timeout, TLS, cannot connect, cancelled |
| `.problem` | The API answered with an RFC 7807 document, `requestId` preserved |
| `.http` | An error status *without* a problem document — usually a proxy or gateway answered |
| `.unexpectedPayload` | HTTP succeeded, the body is not the promised shape |

`.unexpectedPayload` exists specifically for the Android failure mode: the
request reached the SPA and got HTML back **with status 200**. That is neither a
transport nor an HTTP error, and it is undiagnosable if lumped in with either.
The category carries the content type, a body preview and the expected type, so
one log line identifies it.

`isRetryable` follows from the category rather than from a guess: a wrong body
or a wrong URL will be equally wrong next time, a gateway error may not be.

### The API scope is tighter than the deployment root

Containment for API calls is checked against `ServerAddress.apiScope`, not the
deployment root. A path like `api/v3/../../admin` resolves back *inside* the
deployment and passes a root-level check while having left the API entirely.
Both boundaries are asserted in `ServerAddressTests`.

### Timestamps: RFC 3339 with *optional* fractional seconds

Go's `time.Time` marshals as RFC 3339 **Nano** — fractional seconds appear only
when non-zero, so the same field is `…T12:00:00Z` one moment and
`…T12:00:00.529Z` the next. Foundation's `.iso8601` strategy rejects the
fractional form, which would make token-expiry decoding fail intermittently,
roughly whenever the server's clock lands on a non-integral second. The decoder
accepts both. `Date.ISO8601FormatStyle` is used rather than
`ISO8601DateFormatter` because the former is a value type and therefore
`Sendable`.

## CI Requirement

At least one simulator test must keep performing **real** `SecItem` operations.
`SecItemKeychainBackendTests` and `DeviceKeyStoreTests` are that test. Pure logic
tests stay green with a broken Keychain setup, so without them a build can look
entirely healthy while the whole auth stack is inoperative — which is exactly
what `CODE_SIGNING_ALLOWED=NO` caused before it was removed.

There is no iOS workflow in `.github/workflows` yet; adding one needs a macOS
runner.

### Host encoding: punycode only

**Punycode is the only accepted non-ASCII domain representation.** A Unicode
host such as `münchen.example` is *rejected*; its punycode form
`xn--mnchen-3ya.example` is accepted. This is a security decision, not a gap:
Foundation exposes no UTS-46 mapping to rely on, and an incorrect IDNA mapping
is a homograph problem rather than a formatting problem. Treat a rejection here
as intended behaviour.

Also refused by `ServerOrigin` / `ServerAddress`, in both parsers: user info
(`https://trusted.example@attacker.example/` reads as one host and resolves to
another), query strings and fragments on a server address, hosts carrying
`/ ? # @ \` or whitespace, and ports outside 1–65535. IPv6 literals are
compressed per RFC 5952 via `Network.IPv6Address`, and a zone index is dropped
because it is device-local and would split one server into two identities.

## Deliberate Divergences From The Android Client

The wire contract is kept identical so one server serves both clients: same
query keys, same `/ui/` base path derivation, same override precedence, and the
same two-step expiry parsing (bare number = epoch millis, otherwise RFC 1123).

These behaviours are **not** carried over:

| Android | Why not | iOS instead |
| --- | --- | --- |
| `ServerSettingsStore` keeps `auth_token` in plain SharedPreferences while `DeviceAuthStore` next to it uses EncryptedSharedPreferences | Two credential paths, only one hardened | Keychain for anything token-shaped; `UserDefaults` only for the server URL |
| `isUnderBasePath` compares raw paths | `java.net.URI.rawPath` does not resolve dot segments, so `/ui/../admin` passes a check meant to confine navigation to `/ui/` | Dot segments resolved before any comparison |
| `isSameOrigin` dereferences a possibly-null host | Throws on a host-less URL instead of answering `false` | Optionals handled; host-less ⇒ `false` |
| `isSameOrigin` accepts any matching scheme | It is an origin gate | Restricted to `http`/`https` |
| `NativePlaybackCapabilities.container` is hardcoded `["hls","fmp4","mpegts","ts","mp4"]` | Codecs are probed but containers are asserted. On Apple this is actively wrong: per [ADR-026](../docs/ADR/026-native-webkit-hls-hevc-copy.md) the native HLS path does not play HEVC in MPEG-TS | Capabilities derived from AVFoundation; never claim `mpegts` + `hevc` together |
| `dev`/`staging`/`prod` flavors with `usesCleartextTraffic` | Gradle idiom with no iOS counterpart | One narrow ATS exemption (`NSAllowsLocalNetworking`), not `NSAllowsArbitraryLoads` |
| `ExternalBrowserPolicy` | Works around Android TV browser stubs | Dropped |

One hazard is iOS-specific and has no Android equivalent: `URLComponents` is far
more permissive than `java.net.URI`. Prefixing `https://` onto an input that
already declares a foreign scheme yields a URL whose *host is the scheme* and
whose real host has been demoted into the path — `ftp://demo.example/ui/` became
`https://ftp/demo.example/ui/`. `normalizeServerURL` therefore validates a
declared scheme instead of overwriting it, keyed on `://` rather than on a bare
colon so `demo.example:8080` still works. Both cases are covered by tests.

## Verified Against Android

`Xg2gTests/ServerTargetResolverTests.swift` mirrors every case in
`android/app/src/test/java/io/github/manugh/xg2g/android/ServerTargetResolverTest.kt`
one-for-one, then adds the hardening cases above. Keep both suites in step when
the contract changes.

## Backend Notes

- Device auth needs **no** backend change. Despite its name,
  `backend/internal/control/http/v3/handlers_android_auth.go` is platform
  neutral: `platform` and `deviceName` are request fields and `"android"` is
  merely the default for an empty value. iOS sends `platform: "ios"`.
- The backend issues no DPoP nonces, so the client implements none.
- **There is no stable instance identifier today.** No `instanceId`,
  `installationId`, `serverId` or `deploymentId` exists anywhere in
  `backend/internal`; `/system/info` describes the Enigma2 receiver, not the
  xg2g installation; there is no JWKS or exposed server key. `ExchangePairing`
  returns `deviceId` (this device *at* that server), `deviceGrantId`,
  `accessSessionId`, `endpoints[]`, `policyVersion` — nothing identifying the
  installation. `endpoints[]` is endpoint aliasing, which answers "where else am
  I reachable", not "am I the same installation".

  So `ServerIdentity.instance` is currently unreachable. Making it real needs a
  backend change: an opaque random identifier minted once at first start,
  persisted, and readable **before** pairing — otherwise the client cannot form
  the namespace on first contact. That is a separate decision; the client side
  is already shaped so it becomes a migration rather than a redesign.
- A native iOS client family (`ios_native`) is **not** yet registered. The
  backend currently knows only `ios_safari_native`, the browser path. Phase 3
  needs it added in `playbackprofile/client_matrix.go`,
  `playbackprofile/client_family.go`, `playbackcompat/policy.go`,
  `recordings/capability_registry.go` and the alias map in
  `pipeline/profiles/resolve.go`.
