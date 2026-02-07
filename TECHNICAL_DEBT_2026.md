# TECHNICAL_DEBT_2026

## Invarianten (non-negotiable)
- Backend ist einzige Quelle der Wahrheit; WebUI ist strikt API-Client #1.
- Security ist fail-closed (Scopes, Tokens, SSRF etc.).
- Config ist Produktoberflaeche (Registry + Defaults + Validation).
- Observability & Tests sind Pflichtbestandteile (Request-ID end-to-end).

---

## P0 — Repo SSoT & Drift-Eliminierung

### P0.1 Repo-Hygiene: genau 1 Git-Wurzel
**Target:** Repository enthaelt exakt eine `.git`-Directory (nur im Repo-Root).

**Exit-Criteria**
- `./hack/verify-repo-hygiene.sh` ✅
- `find . -name .git -type d` -> genau 1 Treffer
- Keine Code-Kopie (`xg2g-main-21`, `*-copy`, `*_backup`) im Repo

**Gates**
- CI-Job: `./hack/verify-repo-hygiene.sh`

---

## P1 — Deprecated removal & Flake-Killer

### P1.1 internal/core final entfernen
**Exit-Criteria**
- `rg "internal/core" -S .` -> 0 Treffer
- `go test ./...` ✅

### P1.2 Recording-Cache atomic publish (keine retry-basierten Races)
**Exit-Criteria**
- `go test ./... -count=100` ✅
- (optional) `go test ./... -race` ✅
- Tests beweisen: Evictor fasst niemals in-progress Eintraege an

---

## P2 — Runtime Contract + Docs Governance

### P2.1 FFmpeg Pfad SSoT `/opt/ffmpeg`
**Exit-Criteria**
- Default ist eindeutig (Config + Doku + Container konsistent)
- `xg2g doctor` / Smoke-Test bestaetigt ffmpeg erreichbar

### P2.2 Docs Renderer + Verifier Templates vollstaendig
**Exit-Criteria**
- `./hack/verify-docs-drift.sh` ✅
- `./scripts/render-docs.sh` erzeugt keinen diff (`git diff --exit-code`)

---

## P3 — Produktkern saeubern + WebUI Client Hygiene

### P3.1 Remux Logik: loeschen oder VOD-integrieren (kein Stub)
**Exit-Criteria**
- Kein `DEPRECATED/REMOVED` Stub im Codepfad
- Entweder 0 Treffer oder vollstaendig integrierte Tests

### P3.2 WebUI OpenAPI Client nur via Wrapper
**Exit-Criteria**
- Keine direkten Imports aus generated client ausserhalb Wrapper
- Typed error mapping (RFC7807) getestet

### P3.3 Go toolchain directive sauber
**Exit-Criteria**
- `go` directive: `go 1.xx`
- (optional) `toolchain go1.xx.y`
- Offline Gates bleiben gruen
