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
  green, review threads fixed-or-answered.** Per `AGENTS.md` merge policy.

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
`os.Stat`/file existence; `PromoteFailedToReadyIfPlaylist` is deleted; `kill -9`
during a build leaves the system able to resume from the store alone
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

Mostly done during the cutover (pre-push hook, `AGENTS.md` merge policy,
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

## A-Epics — Application-wide refactor findings (audit 2026-07-20)

Findings from the whole-app refactor pass. Unlike the R-epics these are
mostly mechanical and low-risk, so the bar to start is lower: **any A-epic
may start once cutover Phase 4 (#687) is merged** — no purge required.
Ground rules G1–G5 apply. Every A-epic is sliced so that each PR is small,
reviewable, and independently green.

### Execution rules for the implementing agent (binding, read first)

These exist because each rule was violated at least once during the
cutover and cost a CI round-trip or a review escalation:

1. Before EVERY push: run `make pre-push`. Before claiming "tests green":
   run the exact CI target (`make ci-pr`) and paste its final lines into
   the handoff. "It passed locally" without naming the command is not a
   claim.
2. NEVER `git add .` / `git add -A`. Stage files by explicit path. Before
   every commit: `git status --short` and confirm every listed file is
   intentional.
3. Never amend or force-push a branch that has an open PR.
4. Review threads: fix or answer in the thread FIRST, resolve second
   (`AGENTS.md` lifecycle). Never resolve silently.
5. Move-only PRs must not contain behavior changes. If you spot a bug
   while moving code, note it in the PR description and (if independently
   valuable) flag it for a separate task — do not fix it in the move PR.
6. When a step says "measure first", the measurement result goes to Manuel
   for a go/no-go BEFORE the removal step starts.

### A1 — Retire the legacy API surface (`internal/api`)

**Finding:** Two complete generated API layers exist:
`internal/api/server_gen.go` (3,290 lines, plus ~20 handler/wiring files,
wired in `internal/app/bootstrap` and `internal/daemon`) and the current
`internal/control/http/v3/server_gen.go` (6,540 lines). The legacy layer
is the single largest deletion candidate in the repo.

**Steps (one PR each):**

1. **A1.1 Measure.** Add a counting middleware to the legacy router only:
   metric `xg2g_legacy_api_requests_total{path,client}` + WARN log. No
   behavior change. Deploy staging + production. Observation window:
   ≥ 7 days. Also verify statically which spec the WebUI client is
   generated from: `grep -rn "servers\|basePath\|/api/v" frontend/webui/src/client-ts/ | head`
   and check the Android app's base paths the same way.
2. **A1.2 Gate.** Config flag `api.legacy_enabled` (default `true`),
   registered per the current config process. When `false`, legacy routes
   return 410 Gone with a JSON body pointing to the v3 equivalent.
   (Same pattern as `recordings.strict_target_required`.)
3. **A1.3 Flip.** After the window shows zero legitimate traffic and
   Manuel gives go: default `false`, one-line PR.
4. **A1.4 Delete.** Remove `internal/api` entirely plus its wiring in
   `bootstrap`/`daemon`/`cmd`. Acceptance:
   `grep -rn "internal/api" backend --include="*.go" | grep -v _test` → 0
   matches; full suite green; binary size recorded before/after.

**Do NOT** start A1.4 before A1.3 has been deployed for one cycle.

### A2 — Split the `internal/control/http/v3` god-package

**Finding:** 103 non-test files flat in one package (plus the largest
hand-written files in the repo: `playback_info_response_mapping.go` 794
lines, `handlers_sessions.go` 785). Everything can reach everything;
every feature change churns the same package.

**Method (strictly mechanical, one subpackage per PR):**

1. Record the route table before starting (the generated `server_gen.go`
   route registrations) — it must be byte-identical after every PR.
2. Extract in this order (dependency-light first): `tokens` →
   `sessions` → `playbackinfo` → `hls`. The existing `recordings/` and
   `artifacts/` subpackages are the template.
3. Per PR: move files, fix imports, export only what cross-package
   callers need (prefer keeping helpers unexported and moving them with
   their only caller). `go build ./...`, full test suite, `make pre-push`.
4. No renames, no signature changes, no "cleanup while here" (rule 5).

**Acceptance:** package `v3` root retains only server glue (generated
server, router wiring, shared middleware); no file > 500 lines moves
without being split at a natural seam noted in the PR description.

### A3 — Decompose the frontend playback orchestrator

**Finding:** `frontend/webui/src/features/player/usePlaybackOrchestrator.ts`
is 2,689 lines — a single React hook holding the entire player logic.
This is the frontend twin of the deleted `DecideProfile`: client-side
playback decisions the planner already makes authoritatively.

**Steps:**

1. **A3.1 Inventory (no code change).** Produce a table of every decision
   the hook makes (codec/container checks, retry policy, fallback
   selection, …) with a column "duplicated from `/playback-info`? y/n".
   Deliverable goes to Manuel — deletions are decided there, not
   unilaterally (some client fallbacks may be deliberate resilience).
2. **A3.2 Extract the pure state machine** into its own module with unit
   tests (existing player tests keep passing untouched).
3. **A3.3 Extract transport adapters** (hls.js glue, native video, error
   mapping).
4. **A3.4 Delete duplicated decisions** approved in A3.1; the hook shrinks
   to orchestration glue.

**Per PR:** `cd frontend/webui && npm run lint && npm run test` — paste
final lines in the handoff.

### A4 — Mechanical file splits (low priority)

`internal/pipeline/scan/manager.go` (1,376 lines) and
`internal/infra/media/ffmpeg/detector.go` (1,089 lines). Split along the
natural seams (scan: discovery vs. reconcile vs. persistence; detector:
probe execution vs. capability mapping). Move-only rules apply. Do these
last or as filler between review waits.

### A5 — Make transitional debt visible in code (do this FIRST — small)

**Finding:** The backend contains exactly 2 TODO markers while the specs
document at least four live transitional mechanisms. Debt invisible at
the code site will be missed by every future agent session.

**Step (single small PR):** Add `// TODO(SPEC_VOD_PLANNER_CUTOVER §…)` /
`// TODO(SPEC_MODERNIZATION_2026 §…)` comments at exactly these sites:

1. Legacy bare-target payload acceptance in
   `internal/control/http/v3/recordings/cache.go` (dual-format decode —
   removal is a cutover cleanup step).
2. The copy-default fallback in `recordingTarget()`
   (`artifacts/resolver.go`) — dies with the strict-flag flip.
3. The empty-variant default in `ResolvePlaylistState`
   (`artifacts/resolver.go`) — legacy semantics, revisit in R2.
4. The TS packaging path (superseded by R3).

Plus one `lint-invariants` addition: a naked `TODO`/`FIXME` without a
`(...)` reference fails the gate — keeps future debt findable.

## Sequencing summary

```
R0 (cutover done) ──▶ R1 (config codegen)      [independent, may start after #687]
        │
        ├──▶ R2 (artifact FSM) ──▶ R3 (CMAF + JIT, absorbs M1/M2)
        │
        └──▶ R4 (one intent envelope)          [after purge]
R5 anytime after #687.

A-epics (need only #687, not the purge):
A5 (debt markers, do first) ──▶ A1.1 (measure legacy API) ──▶ A1.2–A1.4
A2 (v3 split) and A3 (frontend orchestrator) in parallel lanes.
A4 as filler. Recommended overall entry order: A5 → A1.1 → A2 → A3.
```

M3 (ABR ladder) remains REJECTED per cutover spec Section 10.
