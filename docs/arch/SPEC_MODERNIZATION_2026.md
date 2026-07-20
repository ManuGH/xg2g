# Spec: xg2g Modernization Program 2026 (R-Epics)

Status: APPROVED DIRECTION — NOT STARTED
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-20
Related: `SPEC_VOD_PLANNER_CUTOVER.md` (must be COMPLETE first, see R0)

This document turns the post-cutover retrospective into a bounded rebuild
program. Each epic exists because a concrete incident during the VOD cutover
(2026-07-19/20) proved the current design costs real time or correctness.
Evidence lines cite those incidents so the motivation stays checkable.

## Ground rules (binding, same spirit as the cutover spec)

- **G1 — Hard gate per epic.** No epic starts before its listed
  preconditions hold AND Manuel has explicitly kicked it off. Nothing here
  is picked up opportunistically while working on something else.
- **G2 — Mini-spec before code.** Each epic gets its own spec document
  (invariants, acceptance criteria, PR slicing) in `docs/arch/` before the
  first implementation PR. This document defines scope, not implementation.
- **G3 — Cutover invariants I1–I5 apply unchanged** (planner decides,
  fail-closed + self-heal, content-addressed intent, paths are not intent,
  no behavior change without a test).
- **G4 — Migration = shadow first.** Any epic that replaces a live
  mechanism runs old and new in parallel with a divergence metric before
  the old path is deleted (the pattern that made the planner cutover safe).
- **G5 — One PR per step, auto-merge (`gh pr merge --auto`), all gates
  green, review threads fixed-or-answered.** Per AGENTS.md merge policy.

## R0 — Prerequisite: finish the cutover (not part of this program)

The program is gated on the cutover being fully done:

1. Phase 4 (#687) merged.
2. `recordings.strict_target_required` flipped to `true` and stable for one
   deploy cycle (fallback metric at zero for legitimate clients).
3. Legacy bare-target payload acceptance removed (cutover spec, Step 3/4
   cleanup).
4. The Great Purge executed per cutover spec Section 8 (≥ 4 weeks of 0 %
   shadow divergence, codec + client matrix covered, Manuel sign-off).

R1 may exceptionally start after item 1 alone — it does not touch the media
path. R2–R4 require all four.

---

## R1 — Config: generate everything from the registry

**Goal:** Adding a config key means adding ONE registry entry. Structs, env
mapping, YAML merge, validation wiring, example files, and docs are
generated from it.

**Evidence:** `target_signing_key` (Phase 3) required hand-edits in
`types.go`, `registry.go`, `merge_env.go`, `merge_file.go`,
`validation.go`, two example YAMLs — and the `merge_file.go` wiring was
forgotten, so the key parsed strictly from YAML and still arrived empty at
runtime. Only the registry-truth audit test caught it. Additionally, 100+
duplicated test env literals had to be mass-edited (now centralized in
`setRequiredTestSecrets`, PR #688).

**Direction:**

- Extend the existing registry (`backend/internal/config/registry.go`) to
  be the single machine-readable source: key, type, env var, YAML path,
  default, required/secret flags, validation rule, description.
- A `go:generate` step emits: env merge code, file merge code, validation
  registration, `config.example.yaml`, and the ops env-var reference table
  in docs. Generated files are committed and drift-checked in CI (the
  `verify-generated-artifacts` gate already exists — extend it).
- The existing registry-truth audit tests become generator round-trip
  tests.

**Acceptance:** A new required secret key is introduced in a follow-up PR by
touching exactly one registry entry + `setRequiredTestSecrets`; everything
else is regenerated. Handwritten `merge_env.go`/`merge_file.go` mapping
bodies are deleted.

**Slicing:** (1) generator + characterization diff against current
handwritten mappers, zero behavior change; (2) cut merge/validation over to
generated code; (3) generate examples + docs; (4) delete handwritten paths.

---

## R2 — One artifact state machine, persisted

**Goal:** Artifact lifecycle state (PREPARING/READY/FAILED, failure reason,
variant, paths, truth fingerprint) lives in ONE persistent store (SQLite —
already vendored and used by the decision audit store), not in three
places.

**Evidence:** State currently lives in `vod.Metadata`, in-memory
`JobStatus`, and implicitly in the filesystem. This triple bookkeeping
produced: `PromoteFailedToReadyIfPlaylist` (a heuristic promoting FAILED
artifacts because a playlist file happens to exist), the empty-variant
`/stream-info` regression (filesystem-as-truth pointed at a directory that
never exists), and multiple reconcile special cases in the resolver.

**Direction:**

- Define the artifact FSM explicitly (states, events, allowed transitions)
  as a pure module with property tests.
- Persist it in SQLite keyed by (recording ref, variant hash). Filesystem
  becomes byte storage only — never read to infer state.
- Crash recovery = FSM replay + orphan sweep, replacing today's
  promote/reconcile heuristics.
- Migration per G4: dual-write with divergence metric, then cut reads over,
  then delete `vod.Metadata` file handling and all Promote*/reconcile
  heuristics.

**Acceptance:** `grep` finds no code path that derives artifact state from
`os.Stat`/file existence; `PromoteFailedToReadyIfPlaylist` is deleted; kill
-9 during a build leaves the system able to resume from the store alone
(test exists).

---

## R3 — Single delivery format (CMAF/fMP4) + JIT copy path

**Goal:** One segmenter, one segment format (CMAF/fMP4), and zero disk
materialization for copy-mode playback. Absorbs and supersedes backlog
items M1 (JIT remux) and M2 (seek-ahead) from the cutover spec Section 10.

**Evidence:** TS and fMP4 packaging paths exist in parallel today; the most
common case (client natively decodes the source codec — the planner now
knows this reliably) still builds a full HLS artifact on disk and makes the
client poll PREPARING for something that needs no transcoding.

**Direction:**

- Put delivery behind one interface consuming a signed plan; implement a
  CMAF segmenter as the only packager.
- Copy-mode: repackage the source TS into fMP4 fragments on the fly at
  request time, including still-growing recordings (timeshift). No
  artifact, no PREPARING loop for this class.
- Transcode-mode: keep artifact builds (via R2's FSM) and add seek-ahead
  (second segment-aligned ffmpeg run at the requested position) — the M2
  design.
- Retire the TS segment path once the client matrix shows no consumer
  needs it (data-driven, per G4).

**Acceptance:** A native-capable client starts playback of any finished or
in-progress recording without a single FFmpeg transcode process and without
disk writes beyond logs/metrics; seeking beyond the built frontier of a
transcode build starts serving within one segment duration.

---

## R4 — One intent envelope for live and VOD

**Goal:** After the purge, live and VOD use the same signed plan envelope,
the same issuance path, and the same decision audit trail — one primitive
instead of two similar ones (live receipts from #679/#666, VOD signed
targets from the cutover).

**Evidence:** The cutover deliberately mirrored the live path's concepts
(verified claims, signed intent, fail-closed) but implemented them
separately. Two envelopes = two serializations, two key-rotation stories,
two places for the next #679-class bug.

**Direction:** Unify types (`BuildIntent` / planner receipt) into one
signed envelope with a `mode` discriminator; one signing/rotation config;
one audit sink. Pure consolidation — no behavior change, characterization
tests before and after.

**Acceptance:** One package owns issue/verify for all playback intents;
`XG2G_DECISION_SECRET` and `recordings.target_signing_key` collapse into
one documented key pair (with migration window via the existing
previous-key mechanism).

---

## R5 — Guardrail completions (small, may run anytime after R0.1)

Mostly done during the cutover (pre-push hook, AGENTS.md merge policy,
enforce_admins, test-secrets helper). Remaining:

- **Decision-literal linter:** extend `lint-invariants` with a grep gate
  forbidding codec/container/resolution literals (`"hevc"`, `"mpeg2video"`,
  `1920`, …) outside `internal/domain/playbackprofile` and the planner
  packages — the technical enforcement of I1. Evidence: the old
  `DecideProfile` lived unnoticed in the builder for years; a linter makes
  that class of leak impossible to reintroduce silently.
- **CI/local parity:** `make pre-push` is the cheap mirror; document that
  `make ci-pr` is the authoritative local bundle and keep both in sync when
  the PR gate changes (add a drift check that the gate workflow calls the
  same targets).

---

## Sequencing summary

```
R0 (cutover done) ──▶ R1 (config codegen)      [independent, may start after #687]
        │
        ├──▶ R2 (artifact FSM) ──▶ R3 (CMAF + JIT, absorbs M1/M2)
        │
        └──▶ R4 (one intent envelope)          [after purge]
R5 anytime after #687.
```

M3 (ABR ladder) remains REJECTED per cutover spec Section 10.
