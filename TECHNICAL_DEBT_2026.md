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
- Introduced wrapper module at `webui/src/client-ts/wrapper.ts`.
- Product code now consumes generated client only via `webui/src/client-ts` exports or wrapper helpers.
- Added typed RFC7807/API error mapping tests at `webui/src/client-ts/wrapper.test.ts`.
- Added guard script `webui/scripts/verify-client-wrapper-boundary.sh` and wired it into `webui` lint workflow.

**Verification Note**
- Static scan in repo confirms no direct `client-ts/*.gen` imports outside wrapper/client-ts internals.

### P3.3 Go toolchain directive sauber
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- `go` directive: `go 1.xx`
- (optional) `toolchain go1.xx.y`
- Offline Gates bleiben gruen

**Result**
- `go.mod` normalized to `go 1.25` with `toolchain go1.25.7`.

---

## P4 — Test Coverage + CI Hardening

### P4.1 Wrapper boundary CI hardening
**Status:** Completed on 2026-02-19.

**Exit-Criteria**
- Wrapper-boundary check is centralized (single script) and reused in CI workflows.
- Diff-scoped PR gate and full CI gate both enforce no direct `client-ts/*.gen` imports outside wrapper.

**Result**
- Added/extended `webui/scripts/verify-client-wrapper-boundary.sh` with full-scan + diff-scoped modes.
- Wired guard into:
  - `webui` lint workflow (`npm run verify:client-wrapper`)
  - `.github/workflows/ci.yml`
  - `.github/workflows/pr-required-gates.yml`
  - `.github/workflows/phase4-guardrails.yml`
  - `.github/workflows/ci-deep-scheduled.yml`
