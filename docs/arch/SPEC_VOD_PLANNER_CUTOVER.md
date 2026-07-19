# Spec: VOD/Recording Path — Planner-Authoritative Cutover (Phase 1)

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-19
Prerequisite: PR #679 (`fix(playback): verify client capability claims`) must be merged to `main` before Step 1 begins.

---

## 1. Context and Problem Statement

The live playback path is planner-authoritative since the cutover in #666
(`1cb8adb3`). The recording/VOD path already carries a planner-issued
`TargetPlaybackProfile` end-to-end, but three edge components still make or
accept playback decisions on their own. This spec closes those gaps. It does
NOT delete the legacy decision engine (`internal/control/recordings/decision`)
— that is Phase 2 and gated on shadow-divergence criteria (Section 8).

### Current data flow (verified against source, 2026-07-19)

1. The planner issues a playlist URL via `RecordingPlaylistURL()`
   (`backend/internal/control/http/v3/recordings/cache.go`), embedding the
   target profile as `?variant=<hash>&target=<base64url(canonical JSON)>`.
2. The client echoes this URL back. `serveHLSPlaylist()`
   (`backend/internal/control/http/v3/recordings_hls.go`) decodes `target`
   via `DecodeTargetProfileQuery()` **without any integrity check**.
3. The artifacts resolver
   (`backend/internal/control/http/v3/recordings/artifacts/resolver.go`)
   triggers `EnsureSpecWithTargetProfile()` on the `vod.Manager`, which spawns
   an in-memory build goroutine. There is no job table and no queue; the
   effective retry loop is the client polling `PREPARING` responses
   (`Retry-After: 5s`) plus the `FAILED → MarkPreparingIfState → triggerBuild`
   reconcile path.

### The three policy leaks

| # | Location | Leak |
|---|----------|------|
| L1 | `backend/internal/control/vod/policy.go` — `DecideProfile()` | Builder-owned codec policy ("Chrome First"). Zero non-test callers; dead code. A parallel coarse `vod.Profile` (Default/High) still travels through all builder signatures alongside the real target. |
| L2 | `resolver.go` (`recordingTarget()`, ~line 329) and `serveHLSPlaylist()` (`detectClientProfile(r)`) | When `target` is absent, the edge synthesizes one from a coarse profile string / User-Agent sniffing. The edge is re-deciding. |
| L3 | `DecodeTargetProfileQuery()` | The target profile round-trips through the untrusted client with no signature. A client can mint arbitrary targets; each unique target hashes to a new variant, and each variant triggers a full FFmpeg build (variant explosion / resource abuse). |

---

## 2. Invariants (apply to every step)

- **I1 — Planner decides, builder executes.** No component downstream of the
  planner may select codecs, containers, resolutions, or transcode/copy modes.
- **I2 — Fail closed at the component, self-heal through the planner.** A
  component that detects an inconsistent input aborts with a typed error. It
  never substitutes its own decision. Recovery happens exclusively by
  invalidating upstream truth so the next planner pass re-plans against
  corrected reality.
- **I3 — Intent is content-addressed.** The variant key MUST be derived
  server-side from the canonical target (`RecordingTargetVariantHash`). A
  client-supplied variant that does not match the recomputed hash is rejected.
- **I4 — Paths are not intent.** `workDir`, `outputTemp`, `finalPath` remain
  owned by the `vod.Manager`. The planner says *what*, never *where*.
- **I5 — No behavior change without a test proving the old behavior gone.**
  Every removed fallback gets a regression test asserting the new fail-closed
  response.

---

## 3. Step 1 — Remove the legacy dual-path from the VOD builder

**Goal:** `TargetPlaybackProfile` becomes the only decision input the builder
accepts.

**Changes:**

1. Delete `DecideProfile()` from `backend/internal/control/vod/policy.go` and
   its tests in `policy_test.go`. Verify zero remaining references
   (`grep -rn "DecideProfile" backend/`).
2. Collapse the builder API: `EnsureSpec` / `StartBuild` merge into their
   `WithTargetProfile` variants. Rename the surviving methods to `EnsureSpec` /
   `StartBuild` (drop the suffix). `targetProfile == nil` returns a typed
   error (`ErrMissingTarget`); no silent default.
3. Remove `vod.Profile` from the public builder API. Where an internal
   coarse mode is still needed (e.g., ffmpeg arg assembly), derive it
   internally from `TargetPlaybackProfile.Video.Mode` / `Audio.Mode`. The
   type may survive as an unexported implementation detail; it must not
   appear in any exported signature of `vod.Manager`.

**Acceptance criteria:**

- `go build ./...` and full test suite green.
- `grep -rn "DecideProfile" backend/` → no matches.
- No exported `vod.Manager` method accepts `vod.Profile`.
- New test: `EnsureSpec` with nil target returns `ErrMissingTarget` and does
  not create a job or touch the filesystem.

---

## 4. Step 2 — Remove edge fallbacks (fail closed at the HTTP boundary)

**Goal:** A request without a valid planner-issued target never reaches the
builder.

**Changes:**

1. In `recordingTarget()` (`artifacts/resolver.go`): remove the
   `recordingTargetProfile(profile)` synthesis. `target == nil` →
   `ArtifactError{Code: CodeInvalid}` with a detail directing the client to
   the playback-info endpoint (which issues signed URLs).
2. Remove `detectClientProfile(r)` usage from `serveHLSPlaylist()`
   (`recordings_hls.go`). The `profile` query parameter becomes vestigial:
   accepted for URL compatibility, ignored for decisions.
3. **Internal caller migration (required, easy to miss):**
   `backend/internal/control/http/v3/playback_info_runtime_state.go:60` calls
   `resolver.ResolvePlaylist(ctx, recordingID, "", "", nil)` with a nil
   target. This call site must be updated to pass the planner-resolved target
   for the requesting client, or use a dedicated internal state-query path
   that does not trigger builds. Do not let it regress into the removed
   fallback.
4. **Rollout guard:** gate the strict behavior behind a config flag
   `recordings.strict_target_required` (default `false` on merge, flipped to
   `true` after one deploy cycle confirms no legitimate client hits the
   fallback). Log at WARN with a dedicated metric counter
   (`xg2g_recordings_target_fallback_total`) while the flag is off.

**Acceptance criteria:**

- With flag on: playlist request without `target` → 4xx with actionable
  detail; no build job created (assert via `ActiveJobIDs()`).
- With flag off: legacy behavior preserved, WARN log + metric incremented.
- `playback_info_runtime_state.go` no longer passes nil target.
- Regression test for each removed fallback path (I5).

---

## 5. Step 3 — Sign the target profile ("receipt light", HMAC)

**Goal:** The target profile survives the round-trip through the untrusted
client tamper-proof. This closes L3 and aligns with PR #679 on the live path.

**Design:**

- Wire format for the `target` query parameter:
  `base64url(canonicalJSON) + "." + base64url(HMAC-SHA256(key, canonicalJSON))`.
- Canonical bytes already exist: `CanonicalizeTarget()` + `CanonicalJSON()`.
  The signature MUST cover exactly those bytes — sign after canonicalization,
  verify before unmarshal.
- Key: server-side secret from config (`recordings.target_signing_key`),
  loaded at bootstrap. Support an optional secondary key
  (`target_signing_key_previous`) accepted for verification only, to allow
  rotation without invalidating in-flight URLs.
- Issuance: `EncodeTargetProfileQuery()` signs. Verification:
  `DecodeTargetProfileQuery()` verifies; missing or invalid signature →
  typed error → HTTP 403.
- Additionally enforce I3 in `recordingTarget()`: recompute
  `TargetVariantHash(decodedTarget)`; if a client-supplied `variant` is
  present and differs → 400. The server-derived hash is authoritative in all
  cases.
- No expiry claim in v1. URLs are re-issued per playback-info call; TTL adds
  clock coupling for little gain while both ends are the same process. Note
  as a follow-up if edge delivery (Phase 3) materializes.

**Acceptance criteria:**

- Tamper test: flip one byte in payload or signature → 403, no job created.
- Missing signature → 403 (when strict flag on).
- Rotation test: URL signed with previous key still verifies; URL signed
  with unknown key fails.
- Canonicalization stability test: encode → decode → re-encode yields
  byte-identical payload and signature.
- Variant mismatch test: valid signature but forged `variant` param → 400.

---

## 6. Step 4 — `BuildIntent` with `SourceTruth` and the tolerance contract

**Goal:** The builder validates reality against the planner's assumption and
fails closed with a typed, machine-actionable error that triggers upstream
self-healing.

**New types** (location: `backend/internal/domain/playbackprofile/ports/types.go`,
aliased like `TargetPlaybackProfile`):

```go
// SourceTruth captures what the planner believed about the source media
// when it issued the intent. The builder validates probe reality against it.
type SourceTruth struct {
    Container  string  `json:"container,omitempty"`
    VideoCodec string  `json:"videoCodec,omitempty"`
    AudioCodec string  `json:"audioCodec,omitempty"`
    Width      int     `json:"width,omitempty"`
    Height     int     `json:"height,omitempty"`
    BitDepth   int     `json:"bitDepth,omitempty"`
    DurationS  float64 `json:"durationS,omitempty"`
}

// BuildIntent is the complete, planner-issued execution order for one build.
type BuildIntent struct {
    IntentHash  string                // == RecordingTargetVariantHash(Target)
    SourceTruth SourceTruth
    Target      TargetPlaybackProfile
}
```

The builder API from Step 1 changes to accept `BuildIntent` instead of a bare
`*TargetPlaybackProfile`. `SourceTruth` zero-value means "planner had no probe
truth" and skips validation (bootstrap case: first-ever access before any
probe ran).

**Tolerance contract** (implemented as a pure function with table tests,
e.g. `func ValidateSourceTruth(truth SourceTruth, probed StreamInfo) *TruthMismatch`):

| Dimension | Rule |
|-----------|------|
| Video codec | HARD — exact match required |
| Audio codec | HARD — exact match required |
| Container | HARD — exact match required |
| Bit depth | HARD — exact match required |
| Resolution | HARD on class boundary (SD/HD/FHD/UHD), SOFT within class |
| Duration | SOFT — tolerate ±2.0 s or ±0.5 %, whichever is larger |
| Bitrate, FPS | SOFT — never a mismatch |

HARD violation → abort. SOFT deviation → log at INFO, continue.

**Failure and self-heal path:**

1. On HARD mismatch, the builder fails the job with typed reason
   `TruthMismatch{Field, Expected, Actual}` (structured, not a formatted
   string) via `markFailedFromBuild`.
2. The recordings layer maps `TruthMismatch` to an invalidation of its truth /
   probe cache for that recording (the `truth.go` / `duration_truth.go`
   machinery in `backend/internal/control/recordings/`), and persists the
   corrected probe result.
3. No retry inside the builder. The client's existing polling loop re-enters
   playback-info, the planner re-plans against corrected truth, issues a new
   signed intent (new variant), and the next `ResolvePlaylist` builds it.

**Acceptance criteria:**

- Table tests covering every row of the tolerance contract.
- Integration test for the self-heal loop: seed stale truth (H.264) for an
  MPEG-2 source → first build fails with `TruthMismatch` → truth cache
  invalidated → simulated re-poll produces a new intent → second build
  succeeds. Assert exactly one failed and one successful job.
- `TruthMismatch` is exported, serializable, and carried in
  `vod.Metadata.FailureReason` (or equivalent) so the audit trail shows why
  a build aborted.
- Builder never falls back to its own decision on mismatch (assert no
  transcode job with parameters differing from the intent).

---

## 7. Explicit non-goals (Phase 1)

- No deletion of `internal/control/recordings/decision`, `pipeline/shadow`,
  or any shadow-divergence machinery. The legacy engine keeps running in
  shadow mode untouched.
- No JIT/on-the-fly packaging, no MoQ/WebTransport work.
- No asymmetric signatures / PKI. HMAC with a server-side secret is
  sufficient while issuer and verifier are the same process.
- No persistence of intents in a job table. The client polling loop is the
  queue; the signed URL is the durable carrier of the intent.

## 8. Phase 2 gate (for reference, not part of this implementation)

The legacy recordings decision engine is deleted only when ALL hold:

- Shadow divergence at exactly 0 % for ≥ 4 consecutive weeks, measured via
  the SQLite audit store.
- Coverage across the source-codec matrix (H.264 8-bit, H.264 10-bit, HEVC,
  MPEG-2) and the active client matrix (Safari/iOS, Chrome, Android TV).
- Sign-off by Manuel based on the audit data, not on absence of complaints.

## 9. Implementation order and PR slicing

One PR per step, in order 1 → 2 → 3 → 4. Each PR independently green on CI
and deployable. Step 2 ships with its flag off; the flag flip is a separate
one-line PR after Step 3 is deployed (signed URLs must exist before strict
enforcement makes sense).
