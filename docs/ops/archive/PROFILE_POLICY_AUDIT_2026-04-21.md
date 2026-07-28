# Profile / Policy Audit (2026-04-21)

## Goal

Document the remaining places where `xg2g` still mixes:

- user-facing profile names
- codec selection
- packaging/container policy
- hardware/backend policy
- client compatibility exceptions
- runtime recovery/hardening behavior

The HEVC iPhone fallback was already moved out of profile selection and into an
explicit compatibility policy. This note captures the next hotspots that still
need the same treatment.

## What Was Already Improved

The iOS native HEVC compatibility fallback is no longer hidden inside the
profile picker.

Current state:

- codec/host selection may still choose `safari_hevc_hw`
- the iPhone safeguard now demotes full VAAPI HEVC to an explicit encode-only
  compatibility path instead of silently falling all the way back to CPU

Relevant code:

- `backend/internal/control/http/v3/autocodec/auto.go`
  - `ApplyClientCompatibilityProfileID(...)`
  - `ApplyClientCompatibilityPolicy(...)`

The native packaging rules are also no longer spread inline across start and
playback-info handling.

Current state:

- profile-resolution UA rules are centralized
- iPhone AV1 start-time packaging safeguard is centralized
- native fMP4 packaging preference is centralized
- the desktop Safari AV1 MPEG-TS exception is centralized

Relevant code:

- `backend/internal/control/http/v3/clientpolicy/policy.go`
  - `ResolveProfileUserAgent(...)`
  - `ApplyStartPackagingPolicy(...)`
  - `WantsFMP4Packaging(...)`
  - `AllowExperimentalNativeAV1TransportStream(...)`

The client-feedback recovery path is also no longer assembled inline inside the
HTTP handler.

Current state:

- handler selects the next recovery plan via a named plan type
- repair / Safari dirty / Safari TS recovery plans carry explicit plan identity
  and machine-readable plan reason
- the chosen recovery plan is persisted into fallback trace entries

Relevant code:

- `backend/internal/control/http/v3/playback_feedback_fallback.go`
  - `nextPlaybackFeedbackPlan(...)`
  - `buildPlaybackFeedbackRepairPlan(...)`
  - `buildSafariFeedbackBrowserTSPlan(...)`
  - `buildSafariFeedbackRepairTSPlan(...)`
  - `buildSafariFeedbackDirtyPlan(...)`

The FFmpeg-side runtime hardening path is also no longer assembled inline
inside `plan_builder.go`.

Current state:

- `FinalizePlan(...)` now applies named runtime-hardening plans
- Safari remux, Safari HQ, AV1 progressive deinterlace, and copy-bitstream
  hardening are explicit policy steps

Relevant code:

- `backend/internal/infra/media/ffmpeg/runtime_hardening.go`
  - `applyRuntimeHardeningPlan(...)`
  - `evaluateSafariRuntimeRemuxHardening(...)`
  - `evaluateSafariRuntimeHQHardening(...)`
  - `evaluateProgressiveHardwareDeinterlaceHardening(...)`
  - `evaluateSafariCopyBitstreamHardening(...)`

The playback-info transport rewrites are also no longer assembled inline in
`playback_info.go`.

Current state:

- playback-info applies a named transport-policy step after auto-codec alignment
- live native fMP4, recording native fMP4, and direct-stream transport rewrites
  are explicit plans

Relevant code:

- `backend/internal/control/http/v3/recordings/playback_transport_policy.go`
  - `applyPlaybackTransportPolicy(...)`
  - `resolveLiveNativeTransportPlan(...)`
  - `resolveRecordingNativeTransportPlan(...)`
  - `rewriteDecisionToDirectStream(...)`

This is better because the auto-selector now says what it actually wants, and
the client safeguard is visible as a separate policy layer.

## Remaining Mixed Responsibilities

### 1. Runtime hardening is explicit but still adapter-local

File:

- `backend/internal/infra/media/ffmpeg/runtime_hardening.go`

Current state:

- runtime hardening is now resolved through named hardening plans
- the generic planner no longer assembles those mutations inline

Problem:

- the hardening-plan identity still lives only inside the adapter package
- traces and upstream policy layers still have to infer the chosen hardening
  path from mutated profile fields
- plan ordering is explicit now, but not yet shared as a first-class
  cross-layer contract

Should become:

- first-class runtime-hardening plan identity/reason in trace and policy
  vocabulary
- planner consumes an already-explained hardening choice instead of being the
  only place that knows it

### 2. Transport policy is explicit but still playback-info-local

File:

- `backend/internal/control/http/v3/recordings/playback_transport_policy.go`

Current state:

- late transport rewrites are now resolved through named plans
- `playback_info.go` no longer carries inline packaging/direct-stream mutation

Problem:

- plan identity still lives only inside the playback-info package
- the transport decision still runs after the base decision engine instead of
  being represented as a first-class stage in the broader playback contract
- traces still expose the result, not the named transport plan that caused it

Should become:

- first-class transport-policy identity/reason in trace and policy vocabulary
- eventual convergence with the decision layer so transport shape is explained
  upstream, not only post-processed downstream

### 3. `profiles.Resolve(...)` still remains too overloaded

File:

- `backend/internal/pipeline/profiles/resolve.go`

Current behavior:

- a single profile ID still carries:
  - user-facing intent
  - codec
  - container
  - packaging bias
  - hardware preference
  - sometimes Safari-specific behavior

Problem:

- names such as `safari_hevc_hw` or `av1_hw` are still too semantically dense
- even after moving one compatibility rule out, profile names still imply too
  much about the final runtime shape

Should become:

- public intent layer:
  - `direct`
  - `compatible`
  - `quality`
  - `repair`
- internal policy layers:
  - selected codec
  - selected transport/packaging
  - selected hardware backend
  - client compatibility policy
  - runtime hardening policy

## Recommended Refactor Order

### Step 1

Promote runtime-hardening plan identity into trace / cross-layer policy
vocabulary.

### Step 2

Promote transport-policy plan identity into trace / cross-layer policy
vocabulary.

### Step 3

Shrink `profiles.Resolve(...)` into a narrower constructor fed by explicit
policy outputs instead of using profile IDs as a bundle of hidden meaning.

## Maintainer Assessment

The current system is no longer "broken by default", but it still has too many
policy decisions encoded as:

- profile-name semantics
- inline field rewrites
- late post-processing mutations

That means:

- debugging stays harder than necessary
- traceability is weaker than it should be
- regressions will continue to cluster around Safari/iPhone-specific behavior

The next highest-value cleanup is not another codec tweak. It is lifting the
remaining runtime-hardening and transport-policy plan identities into shared
trace/policy vocabulary, then shrinking `profiles.Resolve(...)`.
