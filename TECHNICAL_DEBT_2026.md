# TECHNICAL_DEBT_2026

## Invarianten (non-negotiable)
- Backend ist einzige Quelle der Wahrheit; WebUI ist strikt API-Client #1.
- Security ist fail-closed (Scopes, Tokens, SSRF etc.).
- Config ist Produktoberflaeche (Registry + Defaults + Validation).
- Observability & Tests sind Pflichtbestandteile (Request-ID end-to-end).

---

## P0 — Repo SSoT & Drift-Eliminierung

### P0.1 Repo-Hygiene: genau 1 Git-Wurzel
**Status:** Completed on 2026-02-19.

**Target:** Repository enthaelt exakt eine `.git`-Directory (nur im Repo-Root).

**Exit-Criteria**
- `./hack/verify-repo-hygiene.sh` ✅
- `find . -name .git -type d` -> genau 1 Treffer
- Keine Code-Kopie (`xg2g-main-21`, `*-copy`, `*_backup`) im Repo

**Gates**
- CI-Job: `./hack/verify-repo-hygiene.sh`

**Result**
- Repo-Hygiene Gate hardened and passing (`./hack/verify-repo-hygiene.sh`).
- Nested `.git` detection remains enforced while pruning heavyweight local-only trees for gate performance.
- Sensitive artifact marker scan is now centralized via `git grep` and de-duplicated by file.

---

## P1 — Deprecated removal & Flake-Killer

### P1.1 internal/core final entfernen
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- `rg "internal/core" -S .` -> 0 Treffer
- `go test ./...` ✅

**Result**
- `internal/core` package remains removed and is protected against reintroduction by validation tests.
- Full backend test suite (`go test ./...`) is green.

**Verification Note**
- Remaining `internal/core` matches are intentional documentation and enforcement-test references, not active package imports.

### P1.2 Recording-Cache atomic publish (keine retry-basierten Races)
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- `go test ./... -count=100` ✅
- (optional) `go test ./... -race` ✅
- Tests beweisen: Evictor fasst niemals in-progress Eintraege an

**Result**
- VOD eviction path and tests were stabilized to avoid touching in-progress recording entries.
- Stress checks passed for affected area:
  - `go test -race ./internal/control/vod -count=1`
  - `go test ./internal/control/vod -run TestEvictRecordingCache_Concurrent -count=50`

---

## P2 — Runtime Contract + Docs Governance

### P2.1 FFmpeg Pfad SSoT `/opt/ffmpeg`
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- Default ist eindeutig (Config + Doku + Container konsistent)
- `xg2g doctor` / Smoke-Test bestaetigt ffmpeg erreichbar

**Result**
- FFmpeg path defaults/docs/container contract are consistently aligned to `/opt/ffmpeg`.
- Runtime/operator docs and setup scripts now expose the same canonical path.

### P2.2 Docs Renderer + Verifier Templates vollstaendig
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- `./hack/verify-docs-drift.sh` ✅
- `./scripts/render-docs.sh` erzeugt keinen diff (`git diff --exit-code`)

**Result**
- Missing verifier templates restored and renderer emits expected verifier units.
- Docs drift gate passes both in clean mode and local WIP mode (`--allow-dirty`).

---

## P3 — Produktkern saeubern + WebUI Client Hygiene

### P3.1 Remux Logik: loeschen oder VOD-integrieren (kein Stub)
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- Kein `DEPRECATED/REMOVED` Stub im Codepfad
- Entweder 0 Treffer oder vollstaendig integrierte Tests

**Result**
- Deprecated remux stub removed (`internal/control/http/v3/recordings_remux.go` deleted).
- HTTP v3 package compile gate remained green after removal.

### P3.2 WebUI OpenAPI Client nur via Wrapper
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- Keine direkten Imports aus generated client ausserhalb Wrapper
- Typed error mapping (RFC7807) getestet

**Result**
- Introduced wrapper module at `frontend/webui/src/client-ts/wrapper.ts`.
- Product code now consumes generated client only via `frontend/webui/src/client-ts` exports or wrapper helpers.
- Added typed RFC7807/API error mapping tests at `frontend/webui/src/client-ts/wrapper.test.ts`.
- Added guard script `frontend/webui/scripts/verify-client-wrapper-boundary.sh` and wired it into the WebUI lint workflow.

**Verification Note**
- Static scan in repo confirms no direct `client-ts/*.gen` imports outside wrapper/client-ts internals.

### P3.3 Go toolchain directive sauber
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- `go` directive: `go 1.xx`
- (optional) `toolchain go1.xx.y`
- Offline Gates bleiben gruen

**Result**
- `go.mod`, `go.work`, and default build pins aligned on Go `1.25.9`.

---

## P4 — Test Coverage + CI Hardening

### P4.1 Wrapper boundary CI hardening
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- Wrapper-boundary check is centralized (single script) and reused in CI workflows.
- Diff-scoped PR gate and full CI gate both enforce no direct `client-ts/*.gen` imports outside wrapper.

**Result**
- Added/extended `frontend/webui/scripts/verify-client-wrapper-boundary.sh` with full-scan + diff-scoped modes.
- Wired guard into:
  - `webui` lint workflow (`npm run verify:client-wrapper`)
  - `.github/workflows/ci.yml`
  - `.github/workflows/pr-required-gates.yml`
  - `.github/workflows/phase4-guardrails.yml`
  - `.github/workflows/ci-deep-scheduled.yml`

---

## P5 — Release/Docs Governance Drift (found 2026-07-07)

### P5.1 RELEASE_MANIFEST.json / DIGESTS.lock silently stale for 3 releases
**Status:** Completed on 2026-07-07.

**Target:** `RELEASE_MANIFEST.json` and `DIGESTS.lock` reflect the actual
latest tag, and cannot silently drift again.

**Root Cause**
- `backend/scripts/release-prepare.sh` is the only thing that updates these
  files, and it is a manual, opt-in script. Nothing in CI enforces it.
- Tag commits for v3.7.1 (#584), v3.7.2 (#588), and v3.8.0 (#600) each only
  bumped `backend/VERSION` and re-rendered docs; none touched
  `RELEASE_MANIFEST.json` or `DIGESTS.lock`. The files were frozen at v3.7.0
  (2026-06-08) while three more tags shipped.

**Exit-Criteria**
- `RELEASE_MANIFEST.json.version` and a `DIGESTS.lock` entry match the
  latest tag.
- `.github/workflows/release.yml` fails the build on a tag push if they
  don't match.

**Result**
- Backfilled both files to `v3.8.0` (correct `git_sha` from the tag).
- Added a gate step to `release.yml`, before GoReleaser runs, that checks
  `backend/VERSION`, `RELEASE_MANIFEST.json`, and `DIGESTS.lock` all agree
  with `GITHUB_REF_NAME` and fails loudly (pointing at
  `release-prepare.sh`) if not.
- `backend/scripts/verify-digest-lock.sh` and
  `backend/scripts/verify-release-output-contract.sh` both pass against the
  backfilled files.

**Verification Note**
- The registry `digest` field itself has never been populated with a real
  value for any release in this project's history (every entry back to
  `3.1.7` reads `"pending"`) — that part of the contract was aspirational
  from the start and is unresolved by this fix. Left as-is rather than
  fabricating digests.

### P5.2 `docs/ops/DEPLOYMENT.md` described a process nobody follows
**Status:** Completed on 2026-07-07.

**Target:** The documented deployment path matches what actually runs in
production.

**Root Cause**
- `DEPLOYMENT.md` stated the only supported path is `deploy/sync.sh` and
  that manual `/srv/xg2g` drift is unsupported. Live forensics on LXC 110
  (`docker inspect`, `pct exec`, `/root/xg2g` git log) showed both running
  instances actually execute a binary built ad-hoc on the host and pushed
  via `pct push` — the exact workflow the doc called unsupported — and at
  the time of the audit that binary was 6 commits ahead of
  `origin/fix/lease-consumption-renewal`, i.e. running unpushed code.

**Exit-Criteria**
- The doc names both real paths (tagged-release/OCI for anyone outside the
  maintainer's own host, fast-iteration `pct push` for the maintainer's own
  host) instead of pretending only one exists.

**Result**
- Added a "Fast Iteration Path (maintainer's own host only)" section to
  `DEPLOYMENT.md` documenting the `pct push` workflow as sanctioned, with
  the one rule that actually matters for it: nothing gets deployed that
  isn't already pushed to `origin` (a clean `git status` on the build host
  is not sufficient evidence of that).

**Verification Note**
- Per `AGENTS.md`'s own rule ("update `RUNBOOK_SYSTEMD_COMPOSE.md` with the
  exact observed delta"), the specific observed facts (image label vs.
  runtime binary mismatch, unpushed commit running in prod) were also added
  there as an "Observed live-host delta on July 7, 2026" entry, matching the
  existing March/April 2026 entries' format. `DEPLOYMENT.md` carries the
  policy change (the path is sanctioned); the runbook carries the forensic
  record of what was actually found.

### P5.3 CHANGELOG.md missing six tagged releases
**Status:** Completed on 2026-07-07.

**Target:** Every tagged release from `v3.5.0` through `v3.8.0` has a
`CHANGELOG.md` entry, and `Unreleased` reflects work merged since.

**Root Cause**
- `CHANGELOG.md` jumped directly from `[v3.8.0]` to `[v3.4.9]` — a version
  that was itself never tagged or released (the `v3.4.9` prep commits rolled
  forward into `v3.5.0` instead). `v3.5.0`, `v3.5.1`, `v3.6.0`, `v3.7.0`,
  `v3.7.1`, and `v3.7.2` — six real releases per `gh release list` — had no
  entry at all, and `Unreleased` was empty despite 46 merged commits since
  `v3.8.0`.

**Exit-Criteria**
- All six missing releases and `Unreleased` have entries.

**Result**
- Added all six entries plus `Unreleased`, each reconstructed from
  `git log <prev-tag>..<tag>` and marked as mechanically reconstructed
  rather than hand-authored impact prose (see each entry's provenance
  note).

**Verification Note**
- These entries are commit-subject groupings, not independently verified
  user-impact claims the way `v3.8.0`'s hand-written "Behavioral Changes"
  section is. Treat them as a factual index into the PR history, not as a
  substitute for reading the linked PRs if precision matters.

### P5.4 Minor cleanup found during the same pass
**Status:** Completed on 2026-07-07 (P5.4a); tracked, not actioned (P5.4b).

- **P5.4a:** `backend/scripts/verify-digest-lock.sh` printed `vv3.8.0`
  (double `v`) in its log line, because `backend/VERSION` already includes
  the `v` prefix and the script prepended another. Fixed.
- **P5.4b:** `/srv/xg2g` on LXC 110 has five files (`IDENTITY.md`,
  `SOUL.md`, `USER.md`, `HEARTBEAT.md`, `TOOLS.md`) with zero history
  anywhere in this repo's git — orphaned artifacts on that host's stale
  checkout, not a repo issue. Needs a decision on the live host, not a
  commit; not actioned here.
