# Spec: xg2g Kernel Direction (K-Epics)

Status: **DRAFT — BLOCKED ON §1. NOT APPROVED, NOT STARTED.**
Owner: Manuel (architecture sign-off) — drafted by coding agent
Date: 2026-07-26
Related: `SPEC_MODERNIZATION_2026.md` (R-Epics, A-Epics) — this document does
not supersede it; see §6 for the exact relationship.

---

## 0. Naming (read first — avoids a real collision)

This document is **not** about an API version and **not** about a release
version. Three distinct `v` namespaces already exist in this repo:

| Namespace | Current value | Owner |
| :--- | :--- | :--- |
| Product release | `v3.8.1` (`backend/VERSION`) | GoReleaser / tags |
| API version | `/api/v3` active (334 route literals); `/api/v1` (6) and `/api/v2` (5) legacy in `internal/api` | `internal/control/http/v3` |
| Architecture generation | *this document* | — |

Calling a rebuild "v2" would name it after a **retired API version that still
registers 5 routes**. The epics here are therefore **K-Epics** ("kernel"), with
no version digit. Nothing in this document changes `backend/VERSION` or
introduces an `/api/v4`.

---

## 1. BLOCKING DECISION — What is xg2g? (Manuel only)

**No K-epic starts and nothing is deleted until this section is resolved.**
This is the only question in this document that cannot be answered from the
code, and it changes the target state of every epic below.

### The measured surface at stake

| Package | Non-test LOC | Class |
| :--- | ---: | :--- |
| `internal/entitlements` | 761 | commercial |
| `internal/receipts` | 884 | commercial |
| `internal/household` | 1,236 | multi-user |
| **Subtotal (in scope of this decision)** | **2,881** | |
| `internal/domain/deviceauth` | 2,494 | device identity — *not* in scope |
| `internal/control/http/v3/{pairing,deviceauth}` | 1,452 | device identity — *not* in scope |
| `internal/hdhr` | 665 | bridge core — *not* in scope |

Additionally 19 non-test files reference `monetization` (incl.
`XG2G_MONETIZATION_AMAZON_USE_SANDBOX`).

**Explicitly excluded from this decision:** `deviceauth` + `pairing` exist
because the Android app needs a non-browser identity. They stand or fall with
the Android client, not with commercialization. `hdhr` (HDHomeRun emulation)
is bridge functionality for Plex/Jellyfin consumers — core either way.

### Path A — Bridge

xg2g is a single-household Enigma2 → modern-client bridge.

- Delete `entitlements`, `receipts`, and the monetization references.
- Decide `household` separately: keep only if more than one human actually
  uses distinct profiles today.
- Consequence: ~1,600–2,900 LOC plus their tests, config keys, API endpoints
  (`/system/entitlements*`, `/household/*` = 6 of 62 endpoints) and CI surface
  disappear. Every K-epic below gets cheaper.
- Cost: publishing xg2g as a paid product later means rebuilding this layer.

### Path B — Product

xg2g is a product that happens to have one user today.

- The commercial layer stays and gets real backing instead of a half-built
  one: a defined entitlement source of truth, receipt verification that is
  actually verified against a store, and a documented threat model.
- Consequence: K1 (one state store) must model tenancy from the start;
  K4's config budget must carve out a per-tenant tier; K5's resource shapes
  need an owner dimension.
- Cost: strictly more work in every K-epic, for an option that is currently
  unexercised.

### Path C — Defer (this document's default while unresolved)

Both paths stay described here. **Nothing is deleted.** K-epics may only
start in a form that is identical under A and B — in practice K1 (storage
consolidation, tenancy-agnostic), K6 (ops truth) and the measurement slices
of K2/K5. K3 and K4 cannot start under C: their target states differ between
A and B.

**Acceptance for §1:** Manuel records A or B in this section with a date. The
first PR of any K-epic references that line.

---

## 2. Ground rules

G1–G5 from `SPEC_MODERNIZATION_2026.md` apply unchanged (hard gate per epic,
mini-spec before code, cutover invariants I1–I5, shadow-first migration, one
PR per step with green gates). The execution rules for the implementing agent
in that document's A-Epics section (`make pre-push`, no `git add .`, no
force-push on open PRs, move-only means move-only) apply here verbatim.

One rule is added, and it is the reason this document exists as something
other than a longer R-Epic list:

### G6 — Deletion budget (binding)

**Every K-epic must end net-negative in non-test Go LOC.** Each epic's final
PR description records:

```
LOC before (non-test, backend, excl. vendor):  <n>
LOC after:                                     <m>
Delta:                                         <m-n>   # MUST be < 0
```

Measured with the same command every time:

```bash
find backend -name '*.go' ! -name '*_test.go' ! -path '*/vendor/*' | xargs wc -l | tail -1
```

**Rationale (evidence, §3):** the codebase currently carries two complete
generated API layers (6,540 + 3,290 lines) and 545 non-test `legacy`
references. Every one of those arrived through a change that added the new
path and deferred deleting the old one. An epic that only adds is how this
state was reached; G6 makes that outcome fail the epic rather than ship it.

An epic that legitimately cannot end negative (rare — K6 is the plausible
case) states so in its mini-spec **before** implementation starts and gets an
explicit exemption from Manuel. Exemption after the fact is not available.

---

## 3. Findings (measured 2026-07-26, `main` + `refactor/r5-1-guardrail-linter`)

All numbers below are reproducible with the command given. They are the
evidence base for K1–K6; where an earlier informal estimate was wrong, the
corrected figure is marked.

| Finding | Value | Command |
| :--- | ---: | :--- |
| Backend non-test Go LOC (excl. vendor) | 140,302 | `find backend -name '*.go' ! -name '*_test.go' ! -path '*/vendor/*' \| xargs wc -l \| tail -1` |
| Backend test LOC | 120,066 | same with `-name '*_test.go'` |
| `internal/control` | 57,226 LOC / 304 files | `find backend/internal/control -name '*.go' ! -name '*_test.go' \| xargs wc -l \| tail -1` |
| `control/http/v3` flat non-test files | 92 | `ls backend/internal/control/http/v3/*.go \| grep -v _test \| wc -l` |
| Generated server layers, both live | 6,540 (v3) + 3,290 (legacy `internal/api`) | `wc -l` on both `server_gen.go` |
| API v3 endpoints | 62 | `grep -cE '^\s{2}/' openapi/v3.normative.snapshot.yaml` |
| Packages owning `CREATE TABLE` | **10** | `grep -rl --include='*.go' 'CREATE TABLE' backend/internal \| grep -v _test \| sed 's\|/[^/]*$\|\|' \| sort -u` |
| `XG2G_*` env keys | ~293 (config pkg) / 305 (repo) | `grep -rho 'XG2G_[A-Z0-9_]*' backend/internal/config \| sort -u \| wc -l` |
| Frontend `src` LOC | 66,633 | `find frontend/webui/src -type f \| xargs wc -l \| tail -1` |
| Frontend boundary gate scripts | 8 | `frontend/webui/package.json` → `gate:*` + `design:check` |
| CI workflows / backend gate scripts | 22 / 110 | `ls .github/workflows \| wc -l`; `ls backend/scripts \| wc -l` |

### Debt marker distribution (corrected)

An earlier estimate of "561 debt markers" conflated marker types. The actual
non-test distribution is materially different and more informative:

| Marker | Count |
| :--- | ---: |
| `TODO` | 7 |
| `FIXME` / `HACK` / `XXX` | 0 |
| `deprecated` (case-insensitive) | 77 |
| `legacy` (case-insensitive) | 545 |

**Reading:** debt is not *marked* (7 TODOs — the A5 finding still holds) but it
is *pervasive as parallel old paths* (545 `legacy` references). This is the
single strongest argument for G6: the codebase does not have a TODO problem,
it has an undeleted-predecessor problem.

Similarly corrected: `os.WriteFile` appears **27** times in non-test code, not
219 (the higher figure included tests and a second pattern).

### The ten schema owners (K1's target)

```
internal/control/recordings/capreg      internal/library
internal/control/recordings/decision    internal/persistence/sqlite
internal/domain/deviceauth/store        internal/pipeline/resume
internal/domain/session/store           internal/pipeline/scan
internal/entitlements                   internal/household
```

Ten packages independently open a database, define a schema, and own their own
migration story. `internal/persistence/sqlite` exists and is the obvious host,
but is currently one of the ten rather than the one.

---

## 4. K-Epics

### K1 — One state store

**Goal:** One SQLite database, one migration ledger, one open/pragma/backup
policy. Filesystem is byte storage and is never read to infer state.

**Evidence:** the ten schema owners above; the `PromoteFailedToReadyIfPlaylist`
heuristic, the empty-variant `/stream-info` regression, and the lease-expiry
zap incident all originated in state living in more than one place.

**Relationship to R2:** R2 does this for the artifact FSM only. K1 is R2's
mechanism applied to the remaining nine schema owners. **If R2 has shipped,
K1 starts from its store and its FSM pattern — it does not redo R2.**

**Direction:**
- `internal/persistence/sqlite` becomes the only package that opens a database
  and the only owner of migrations; the other nine expose repositories over it.
- One versioned migration ledger; `schema_version` is asserted at boot and the
  daemon fails closed on an unknown version.
- Per G4: dual-write with a divergence metric per store, then cut reads over,
  then delete the old store — one store per PR, never two at once.

**Acceptance:** `grep -rl 'CREATE TABLE' backend/internal | grep -v _test`
returns exactly one package. `kill -9` mid-build leaves the system resumable
from the store alone (test exists). G6 delta recorded.

**Under Path B:** every table carries an owner/tenant column from the first
migration. Under Path A it does not. This is why K1 is the one storage epic
that must still read §1 before its final schema is frozen.

---

### K2 — Kernel/adapter split, and guardrails as types

**Goal:** A pure decision kernel (planner, profiles, capability resolution,
artifact FSM) with no I/O, no HTTP, no config structs — everything else is an
adapter around it.

**Evidence:** R5 builds a **grep linter** forbidding `"hevc"`, `"h264"`,
`1920`, `1080` outside the profile packages. The frontend ships **8** boundary
gate scripts (`no-client-decision-engine`, `no-ua-sniffing`,
`no-duration-guessing`, `no-raw-json-fetch`, …). Each of those gates exists
because the boundary is crossable and was crossed. A grep gate is a
compensating control for a type system that permits the mistake.

**Direction:**
- Codec, container, and resolution stop being strings/ints with public
  constructors. They become types whose only constructors live inside the
  profile domain (unexported fields + a validating factory, or generated
  enums). Adapters can carry them, not synthesize them.
- The kernel gets golden and property tests as its primary suite
  (`test/invariants/decision_goldens_test.go` is the existing seed); adapters
  get thin contract tests instead of duplicated unit tests.
- **A guardrail linter is deleted only when the type it compensates for
  exists.** R5's linter stays until then — this epic is what earns its removal.

**Acceptance:** the decision kernel compiles with no import of `net/http`,
`os`, or `internal/config`. At least one R5 grep gate is deleted in the same
PR that makes its rule unrepresentable, with the type change shown.

**Slicing:** (1) identify kernel boundary, no code change, deliverable is a
package list + import violations; (2) introduce the profile value types behind
the existing API; (3) cut adapters over; (4) delete the compensating gates.

---

### K3 — One playback path (live is VOD with a moving window)

**Goal:** One packager, one intent envelope, one delivery path. Live becomes
the sliding-window case of the same machinery, not a parallel universe.

**Evidence:** two intent envelopes exist (`PlannerReceipt` vs `BuildIntent`) —
R4 merges them after the fact. Two delivery formats exist (TS and fMP4) —
R3 merges them after the fact. Both were built separately because live and VOD
were treated as separate domains from the start; `internal/control` is 57,226
LOC largely as a consequence.

**Relationship to R3/R4:** K3 **is** R3+R4 with one addition — after both land,
the remaining live-specific delivery code is deleted rather than kept beside
the unified path. **If R3 and R4 have shipped, K3 is only that deletion step**
and should be sized accordingly (it is then the cheapest epic here, not the
most expensive).

**Acceptance:** one packager implementation; one signed envelope type; the live
path contains no segment-writing code of its own. G6 delta must be strongly
negative — this epic is the primary source of the deletion budget for the rest.

**Precondition:** K2 slice 2 (profile value types), otherwise the unified path
re-introduces the literals R5 is currently grepping for.

---

### K4 — Config budget, not just config codegen

**Goal:** A hard ceiling on operator-visible configuration, with everything
else derived or auto-detected.

**Evidence:** ~293 `XG2G_*` keys for a single-household bridge. The registry
already classifies by `Profile` (Simple/Advanced/Integrator/Internal) and
`Status` (Active/Deprecated/Candidate/Internal) — the taxonomy exists, the
budget does not.

**Relationship to R1:** R1 generates structs/merge/validation/docs from the
registry, which makes 293 keys *cheaper to maintain*. K4 reduces the number.
R1 is a precondition, not a substitute — do not start K4 before R1 lands, or
the classification work is done twice.

**Direction:**
- Classify all ~293 keys into: **keep as Simple** (target ≤ 15), keep as
  Advanced/Integrator, **derive** (computable from other config or the
  environment), **auto-detect** (the vendor-detection work in
  `e47f78d7` is the proof case — hardware capability is detected, not
  configured), **delete** (Candidate/zombie keys with no reader).
- Deletion of a key follows G4: mark Deprecated with a WARN on use, one deploy
  cycle, then remove.
- The registry gains a test that fails when the Simple tier exceeds its budget.

**Acceptance:** Simple tier ≤ 15 keys, enforced by a test. Every remaining
Advanced/Integrator key has a description that names a scenario in which an
operator would change it — keys that cannot get one are Internal or deleted.

**Blocked under Path C:** the target count differs between A and B (Path B
needs a per-tenant tier). Do not start this epic before §1 is resolved.

---

### K5 — API resource shapes

**Goal:** Fewer, orthogonal resources with representations, instead of verbs
in paths.

**Evidence:** 62 endpoints. One recording is reachable through nine of them:

```
/recordings/{id}          /recordings/{id}/delete      /recordings/{id}/rename
/recordings/{id}/status   /recordings/{id}/stream-info /recordings/{id}/scrub.jpg
/recordings/{id}/thumbnail.jpg  /recordings/{id}/stream.mp4
/recordings/{id}/playlist.m3u8  /recordings/{id}/timeshift.m3u8
```

That is one resource, two mutations, one status projection, two image
representations, and three delivery representations — expanded into ten routes
that each carry their own handler, test, and client wrapper.

**Direction:**
- Measurement slice first (no code change): a table of all 62 endpoints with
  columns *resource*, *is-verb*, *representation-of*, *client callers*
  (WebUI + Android, from `client-ts` and the app's base paths). Deliverable to
  Manuel; the reshape set is decided there, not unilaterally.
- Reshape happens **only** behind the existing `/api/v3` prefix as additive
  routes plus deprecation of the old ones — no `/api/v4`. A new API version
  would create exactly the second-complete-layer problem this document exists
  to end (see `internal/api`: 3,290 lines still live).
- Retirement of a replaced route follows the A1 pattern that already exists:
  count it, gate it behind a flag, flip, delete.

**Acceptance:** endpoint count reduced with zero client breakage, verified by
the counting middleware showing zero traffic on retired routes for ≥ 7 days
before deletion.

**Sequencing note:** K5 must run **after** A1 (legacy `internal/api` deleted)
and A2 (v3 package split). Reshaping routes while two generated server layers
and a 92-file flat package exist multiplies the work.

---

### K6 — Ops truth

**Goal:** One deploy path that is the real one, and a runtime that can prove
what it is running.

**Evidence (P5.1/P5.2, already recorded in `TECHNICAL_DEBT_2026.md`):**
`DEPLOYMENT.md` described a process nobody follows — production ran a
host-built binary pushed via `pct push`, at audit time 6 commits ahead of its
branch, i.e. unpushed code in production. `RELEASE_MANIFEST.json` and
`DIGESTS.lock` were stale for three releases. The registry `digest` field has
read `"pending"` for every release back to `3.1.7` — the contract was never
once fulfilled.

**Direction:**
- `/system/info` reports the **build SHA of the running binary**, injected at
  link time, and the daemon logs it at boot. A deployed binary that cannot
  name its own commit is the actual root cause of the P5.2 finding.
- The fast-iteration `pct push` path stays sanctioned (it is what is used) and
  gains the one mechanical check that matters: refuse to deploy a binary whose
  SHA is not reachable on `origin`.
- Retire, do not backfill, the parts of the release contract that have never
  been fulfilled — a `digest` field that has said `"pending"` since 3.1.7 is
  removed or populated, not carried.

**G6 exemption:** likely the one epic that ends LOC-neutral or slightly
positive. Requested here in advance per G6.

---

## 5. Explicitly out of scope — do not rebuild

The value in xg2g is not in the code; it is in empirical knowledge that cost
months of debugging against real hardware. These are **inputs** to every epic
above and are not to be re-derived, re-litigated, or "cleaned up while here":

- `docs/arch/CODEC_MATRIX.md` — codec/container truth
- `docs/arch/ENIGMA2_STREAMING_TOPOLOGY.md` — receiver behavior
- The Vu+ Uno 4K DVB assertion crash finding (OpenATV 7.6.0, not xg2g-caused)
- The AV1/Safari and hardware-vendor findings (`e47f78d7`, `82191735`)
- The zap/lease state machine and its expiry semantics
- `ADR_P7` / `ADR_P8` / `ADR_PLAYBACK_DECISION*` — decision-engine semantics

A rewrite that discards these buys clean code for a repeated year.

---

## 6. Relationship to `SPEC_MODERNIZATION_2026.md`

This document does **not** replace the R/A-Epics and does not authorize
skipping them. Overlap is explicit:

| K-Epic | R/A relationship |
| :--- | :--- |
| K1 | Extends R2 from artifacts to all 10 schema owners. R2 first. |
| K2 | Earns the deletion of R5's grep linter. R5 stays until then. |
| K3 | Is R3+R4 plus the deletion step. If R3/R4 shipped, K3 is only the deletion. |
| K4 | Requires R1 (registry codegen) as a precondition. Adds the budget R1 lacks. |
| K5 | Requires A1 (legacy API deleted) and A2 (v3 split) first. |
| K6 | Independent. Closes P5.1/P5.2 properly rather than by backfill. |

**The one thing here that is genuinely new is G6.** Everything else is the
R/A program with an explicit end state. Without a deletion budget, the R/A
epics produce the unified path *and* keep the old one — which is the mechanism
that produced 545 `legacy` references and two live generated server layers.

---

## 7. Sequencing (proposed — not authorized)

```
§1 DECISION ─────────────────────────────────────────────┐
                                                         │
Path C (unresolved) may run only:                        │
  K6 (ops truth)        ── independent, start anytime    │
  K2.1 (kernel boundary inventory, no code change)       │
  K5.1 (endpoint inventory, no code change)              │
                                                         │
After §1 resolved:                                       ▼
  R2 ──▶ K1 (one state store)
  R5 ──▶ K2.2–K2.4 (types, cutover, delete gates)
  R3+R4 ──▶ K3 (delete the parallel live path)
  R1 ──▶ K4 (config budget)
  A1+A2 ──▶ K5.2+ (resource reshape, additive under /api/v3)
```

Recommended entry order once §1 is answered: **K6 → K2.1 → K1 → K2.2–K2.4 →
K3 → K4 → K5.**

---

## 8. Open items requiring Manuel

1. **§1** — Path A or B, recorded with a date. Blocks K3, K4, and K1's final
   schema.
2. **G6 exemption for K6** — approve or refuse.
3. **`household`** — under Path A, does more than one human use distinct
   profiles today? Determines whether 1,236 LOC are in or out.
4. **Android client** — K5's endpoint inventory needs the app's actual base
   paths; confirm whether the Android app is a maintained client or dormant.
   The answer changes whether `deviceauth`+`pairing` (3,946 LOC) is live
   surface or dead weight, independently of §1.
