# Repository-Wide Architecture Audit

**Date:** 2026-01-11
**Auditor:** Engineering Leadership
**Scope:** Complete repository structure (cmd/, docs/, scripts/, .github/, Dockerfiles, Makefile, OpenAPI)
**Status:** Active - Implementation Required

---

## Executive Summary

This audit expands beyond `internal/` to cover the **entire repository infrastructure**. The codebase shows strong engineering discipline (ADRs, security scanning, SBOM attestation) but suffers from **historical accumulation** in CI workflows, documentation, and build tooling.

**Critical Findings:**
- **P0 Contract Risk:** WebUI client and backend server may be generating from DIFFERENT OpenAPI specs
- **P0 CI Path Bug:** CI validates codegen for non-existent path (`internal/api/v3/` doesn't exist)
- **P0 Git Hygiene:** 33MB compiled binaries tracked in repo (daemon, xg2g, xg2g-smoke)

**Total Identified Issues:** 15 findings across 6 categories
**Estimated Cleanup Effort:** 12-16 hours over 10 PRs
**Risk Profile:** 3 critical, 4 high-priority, 5 medium, 3 low

---

## 1. CMD/ - BINARY PROLIFERATION AND CLI DESIGN

### FINDING 1.1: Four Separate Binaries Instead of Subcommands

**System Assumption:**
- `cmd/daemon/` (428 LOC) - Main service
- `cmd/validate/` (71 LOC) - Config validator
- `cmd/v3probe/` (454 LOC) - API health checker
- `cmd/gencert/` (34 LOC) - TLS cert generator

`daemon` already implements subcommands (`config`, `healthcheck`, `diagnostic`) but `validate/gencert/v3probe` are separate binaries.

**Breakpoint:**
Binary distribution complexity:
- GoReleaser (`.goreleaser.yml`) ONLY builds `daemon` (lines 8-33)
- `validate`, `gencert`, `v3probe` are NOT in releases
- Users must compile manually: `go run ./cmd/validate`
- PATH pollution: 4 binaries vs. unified CLI
- No shared flag parsing (each reimplements --help, --version, --config)

**Damage Pattern:**
1. CI uses `validate` (`.github/workflows/ci.yml` line 338-345) but it's not in released artifacts
2. Compiled `v3probe` binary exists in repo root (8.6MB, manually built, not in .gitignore)
3. `gencert` functionality duplicated in `daemon` (auto-generates certs, `cmd/daemon/main.go` line 234-248)

**Decision Demand:**

**Option A: Consolidate into Subcommands (Recommended)**
```
xg2g daemon [run]          # Current main mode
xg2g validate -f config.yaml
xg2g gencert --cert path --key path
xg2g probe --base-url http://localhost:8088
```

**Implementation:**
- Use stdlib `flag` or `cobra` for subcommand routing
- Extract shared logic (config loading, logging setup) to `internal/cli/`
- Single binary to distribute (smaller release footprint)
- Consistent UX across all commands

**Option B: Keep Separate + Fix GoReleaser**
Update `.goreleaser.yml` to build all 4 binaries:
```yaml
builds:
  - id: daemon
    main: ./cmd/daemon
  - id: validate
    main: ./cmd/validate
  - id: v3probe
    main: ./cmd/v3probe
  - id: gencert
    main: ./cmd/gencert
```

**Recommendation:** **Option A** - Consolidate to single `xg2g` binary with subcommands.
**Effort:** 3-4 hours (1 PR)
**Risk:** Low (behavioral compatibility maintained)

---

### FINDING 1.2: Compiled Binaries Tracked in Git

**System Assumption:**
Repository root contains compiled binaries:
```
-rwxr-xr-x  34006055 Jan 11 08:29 daemon
-rwxr-xr-x   8679609 Jan  8 20:46 v3probe
-rwxr-xr-x  33865859 Jan 10 11:55 xg2g
-rwxr-xr-x  33896516 Jan 10 17:07 xg2g-smoke
```

These are NOT in `.gitignore`.

**Breakpoint:**
Binary bloat in git history:
- 33MB per binary × 4 binaries = 132MB in working tree
- Every `make build` creates modified binaries → `git status` shows dirty tree
- Security: Binaries could be tampered, unclear provenance
- Accidental commits to main branch

**Damage Pattern:**
```bash
$ git status
M  daemon
M  xg2g
```
Developer builds locally → accidentally stages binaries → pollutes commit history.

**Decision Demand:**

**Option A: Add to .gitignore (Recommended)**
```gitignore
# Binaries (built by `make build`)
/daemon
/v3probe
/xg2g
/xg2g-smoke
/bin/
```

**Option B: Move to bin/ (Already Exists)**
Makefile already uses `bin/` (line 32, 173), just enforce it:
```make
build: ## Build the xg2g daemon
	@mkdir -p bin
	CGO_ENABLED=0 go build -o bin/xg2g ./cmd/daemon
```

**Recommendation:** **Option A + B** - Add to .gitignore AND enforce `bin/` consistently.
**Effort:** 5 minutes (Quick Win)
**Risk:** None

---

## 2. DOCS/ - DOCUMENTATION STRUCTURE

### FINDING 2.1: Duplicate Architecture Docs (Root vs. docs/arch/)

**System Assumption:**
`ARCHITECTURE.md` exists in TWO locations:
- `/root/xg2g/ARCHITECTURE.md` (21KB, 8 sections)
- `/root/xg2g/docs/arch/ARCHITECTURE.md` (32KB, 10 sections, richer)

**Breakpoint:**
Source of truth unclear:
- New contributors land on root README → find root `ARCHITECTURE.md` → might miss richer `docs/arch/` content
- Both files overlap but differ (docs/arch/ has PACKAGE_LAYOUT.md, 10/10 scorecard)
- No canonical link between them

**Damage Pattern:**
Developer updates root `ARCHITECTURE.md` → docs/arch/ diverges → confusion.

**Decision Demand:**

**Option A: Consolidate to docs/ (Recommended)**
1. Delete root `ARCHITECTURE.md`
2. Replace with stub:
   ```md
   # Architecture
   See [docs/arch/ARCHITECTURE.md](docs/arch/ARCHITECTURE.md) for complete system design.
   ```
3. Update root `README.md` to link:
   ```md
   ## Documentation
   - [Architecture & Design](docs/arch/)
   - [Developer Guide](docs/guides/DEVELOPMENT.md)
   ```

**Option B: Keep Both, Sync Weekly**
Risk: Manual sync burden, drift inevitable.

**Recommendation:** **Option A** - Enforce `docs/` as canonical source.
**Effort:** 15 minutes (Quick Win)
**Risk:** None (docs-only change)

---

### FINDING 2.2: ADRs Excellent But Hidden from Root README

**System Assumption:**
11 ADRs documented in `docs/adr/README.md` with clear index table.

**Breakpoint:**
Root `README.md` doesn't mention ADRs → contributors unaware they exist.

**Decision Demand:**

**Option A: Link ADRs from Root (Recommended)**
Add to root `README.md`:
```md
## Architecture Decisions
See [ADRs (Architecture Decision Records)](docs/adr/) for rationale behind key design choices.
```

**Recommendation:** **Option A**
**Effort:** 2 minutes (Quick Win)
**Risk:** None

---

### FINDING 2.3: OpenAPI Spec Location Confusion

**System Assumption:**
Two OpenAPI specs exist:
- `/root/xg2g/api/openapi.yaml` (66KB, v3.0.0 metadata)
- `/root/xg2g/internal/control/http/v3/openapi.yaml` (v3 current API)

**Breakpoint:**
WebUI generates TypeScript client from **DIFFERENT spec** than server:
```json
// webui/package.json line 9
"generate-client": "openapi-ts --input ../api/openapi.yaml --output ./src/client-ts"
```

But CI validates server codegen from:
```yaml
# .github/workflows/ci.yml line 113
oapi-codegen ... -o internal/control/http/v3/server_gen.go internal/control/http/v3/openapi.yaml
```

**Damage Pattern (CRITICAL):**
IF `api/openapi.yaml` and `internal/control/http/v3/openapi.yaml` diverge:
- WebUI client expects different response schema than server provides
- Runtime 404s, type errors, contract violations

**Decision Demand:**

**Option A: Single Source of Truth (Recommended)**
1. Move v3 spec to public location:
   ```bash
   mv internal/control/http/v3/openapi.yaml api/openapi-v3.yaml
   ```

2. Update ALL consumers:
   - `webui/package.json` → `../api/openapi-v3.yaml`
   - `Makefile` generate target
   - `.github/workflows/ci.yml` validation

3. Deprecate legacy:
   ```bash
   mv api/openapi.yaml api/openapi-v2-legacy.yaml
   ```

4. Add CI gate:
   ```yaml
   - name: Verify Single OpenAPI Source
     run: |
       if [ -f api/openapi.yaml ] && [ -f api/openapi-v3.yaml ]; then
         if ! diff -q api/openapi.yaml api/openapi-v3.yaml; then
           echo "❌ Multiple specs exist and differ"
           exit 1
         fi
       fi
   ```

**Option B: Namespace by Version**
- `api/v2/openapi.yaml` (legacy)
- `api/v3/openapi.yaml` (current)

Risk: Maintains dual structure.

**Recommendation:** **Option A** - Single source (`api/openapi-v3.yaml`).
**Effort:** 1 hour (Ownership Fix)
**Risk:** Medium (requires coordinated updates across 4 files)
**Priority:** **P0 CRITICAL**

---

## 3. SCRIPTS/ - BUILD AND VERIFICATION TOOLING

### FINDING 3.1: Scripts vs. Makefile Role Confusion

**System Assumption:**
13 scripts in `scripts/`:
- `check-go-toolchain.sh` called by CI (`.github/workflows/ci.yml` line 51)
- `verify_gate_a_control_store.sh` called by Makefile (line 714)
- `build-ffmpeg.sh` documented but not automated

**Breakpoint:**
Inconsistent execution paths:
- Developer runs `make quality-gates` → doesn't run `check-go-toolchain.sh` → CI fails
- Some scripts called by CI, some by Makefile, some by neither
- No `scripts/README.md` documenting purpose

**Damage Pattern:**
Developer workflow diverges from CI → late failures.

**Decision Demand:**

**Option A: Makefile Is Interface, Scripts Are Subroutines (Recommended)**
1. Add to Makefile:
   ```make
   check-toolchain: ## Verify Go toolchain policy
       @./scripts/check-go-toolchain.sh

   quality-gates: check-toolchain gate-a lint test-cover security-vulncheck
   ```

2. Create `scripts/README.md`:
   ```md
   # Scripts Directory
   Implementation details for Makefile targets. DO NOT call directly.
   Use `make <target>` instead.
   ```

3. Add script preamble:
   ```bash
   # INTERNAL: Called by Makefile. Do not run directly.
   ```

**Option B: Keep Dual Interface**
Accept scripts as "power user CLI."

Risk: Workflow drift.

**Recommendation:** **Option A** - Makefile is the contract.
**Effort:** 30 minutes (Quick Win)
**Risk:** Low

---

### FINDING 3.2: Pre-commit Hooks Defined But Not Enforced

**System Assumption:**
- `.pre-commit-config.yaml` exists (79 lines) with gitleaks, golangci-lint, yamllint
- Makefile has `hooks:` target (line 676-678)
- No CI validation that hooks are installed

**Breakpoint:**
Developers can bypass hooks by not running `make hooks`.

**Damage Pattern:**
Secrets could leak if gitleaks runs in pre-commit but hook not installed.

**Decision Demand:**

**Option A: Enforce in CI (Recommended)**
Add to `.github/workflows/ci.yml`:
```yaml
- name: Verify Pre-commit Hooks
  run: |
    pip install pre-commit
    pre-commit run --all-files
```

**Option B: Mandatory Hooks Check in Makefile**
```make
build: check-hooks ui-build
	...

check-hooks:
	@if [ ! -f .git/hooks/pre-commit ]; then \
		echo "❌ Pre-commit hooks not installed. Run: make hooks"; \
		exit 1; \
	fi
```

**Recommendation:** **Option A + B** - CI enforcement + local guardrail.
**Effort:** 20 minutes (CI/Gate)
**Risk:** None

---

## 4. .GITHUB/WORKFLOWS/ - CI/CD PIPELINE

### FINDING 4.1: Duplicate CI Workflows (ci.yml vs. ci-v2.yml)

**System Assumption:**
Two CI workflows:
- `ci.yml` (345 lines) - Triggers on push/PR
- `ci-v2.yml` (385 lines) - **ALSO** triggers on push/PR

**Breakpoint:**
Both workflows run tests, build WebUI, validate configs.

**Damage Pattern:**
- 2× CI time
- 2× GitHub Actions minutes consumed
- Developer confusion: "Which CI failure matters?"

**Decision Demand:**

**Option A: Merge into Single Workflow (Recommended)**
1. Identify unique jobs in `ci-v2.yml`
2. Merge into `ci.yml`
3. Rename `ci-v2.yml` → `ci-archived-2025.yml`
4. Update branch protection rules to require only `ci.yml`

**Option B: Keep Both, Document Differences**
Add comment explaining why two workflows exist.

Risk: Maintenance burden persists.

**Recommendation:** **Option A** - Consolidate to single `ci.yml`.
**Effort:** 2 hours (1 PR)
**Risk:** Medium (requires careful job merging)
**Priority:** **P1 HIGH**

---

### FINDING 4.2: CI Codegen Path Mismatch

**System Assumption:**
CI validates v3 API codegen (`.github/workflows/ci.yml` line 113):
```yaml
oapi-codegen -package v3 -generate types,chi-server,spec \
  -o internal/api/v3/server_gen.go \
  internal/api/v3/openapi.yaml
```

**Breakpoint:**
Path `internal/api/v3/` **does not exist**. Actual path: `internal/control/http/v3/`.

**Damage Pattern:**
CI job might be:
- Failing silently (error caught but not propagated)
- Passing false positives (validating wrong output)

**Decision Demand:**

**Option A: Fix Path (Immediate Hotfix)**
Update `.github/workflows/ci.yml` line 113:
```yaml
oapi-codegen -package v3 -generate types,chi-server,spec \
  -o internal/control/http/v3/server_gen.go \
  internal/control/http/v3/openapi.yaml
```

**Recommendation:** **Option A** - Immediate fix.
**Effort:** 2 minutes (Hotfix)
**Risk:** None
**Priority:** **P0 CRITICAL**

---

### FINDING 4.3: No Breaking Change Detection for OpenAPI

**System Assumption:**
CI validates generated code is up-to-date but NOT backward compatibility.

**Breakpoint:**
Developer changes `/api/v3/services` response schema → existing clients break.

**Decision Demand:**

**Option A: Add oasdiff (Recommended)**
Add to `.github/workflows/quality-gates.yml`:
```yaml
- name: Check OpenAPI Breaking Changes
  run: |
    npx @oasdiff/oasdiff breaking \
      <(git show origin/main:api/openapi-v3.yaml) \
      api/openapi-v3.yaml \
      --fail-on ERR
```

**Recommendation:** **Option A**
**Effort:** 15 minutes (CI/Gate)
**Risk:** None
**Priority:** **P1 HIGH**

---

### FINDING 4.4: Actions Pinned Inconsistently (SHA vs. Tag)

**System Assumption:**
127 action uses across workflows, mix of:
- `actions/checkout@8e8c483...` (SHA, immutable)
- `actions/setup-go@v6` (tag, mutable)

**Breakpoint:**
Inconsistent pinning strategy → supply chain risk.

**Damage Pattern:**
`@v6` tags can be force-pushed (attack vector).

**Decision Demand:**

**Option A: Pin ALL by SHA (Recommended)**
Policy: Never use `@vX`, always use `@sha256:...` with comment.

Add CI check:
```yaml
- name: Verify Actions Pinned
  run: |
    if grep -r 'uses:.*@v[0-9]' .github/workflows/; then
      echo "❌ Unpinned actions found"
      exit 1
    fi
```

**Recommendation:** **Option A**
**Effort:** 1 hour (scripted replacement)
**Risk:** Low
**Priority:** **P3 LOW**

---

### FINDING 4.5: No Post-Release Verification

**System Assumption:**
`release.yml` builds Docker images, signs them, generates SBOM (lines 272-339).

**Breakpoint:**
No step validates released image ACTUALLY WORKS.

**Damage Pattern:**
Image could be signed but crash on startup → users pull broken release.

**Decision Demand:**

**Option A: Add Smoke Test (Recommended)**
Add to `release.yml`:
```yaml
verify-release:
  runs-on: ubuntu-latest
  needs: [release]
  steps:
    - name: Pull Released Image
      run: docker pull ghcr.io/manugh/xg2g:${{ github.ref_name }}

    - name: Smoke Test
      run: |
        docker run --rm -d --name xg2g-verify \
          ghcr.io/manugh/xg2g:${{ github.ref_name }}
        sleep 10
        docker exec xg2g-verify xg2g healthcheck ready || exit 1
```

**Recommendation:** **Option A**
**Effort:** 30 minutes (CI/Gate)
**Risk:** None
**Priority:** **P1 HIGH**

---

## 5. BUILD SYSTEM - DOCKERFILES, MAKEFILE

### FINDING 5.1: Dockerfile.distroless Is Dead Code

**System Assumption:**
- `Dockerfile` (103 lines) - Multi-stage, Debian-based, includes FFmpeg
- `Dockerfile.distroless` (38 lines) - Minimal, gcr.io/distroless/static

**Breakpoint:**
`docker.yml` has comment (line 160-165):
```yaml
# Distroless build disabled: Not yet fully implemented
```

But `Dockerfile.distroless` EXISTS and looks complete.

**Damage Pattern:**
Dead code or missing implementation? Unclear.

**Decision Demand:**

**Option A: Implement Distroless in CI (If FFmpeg Optional)**
Build both variants:
```yaml
- name: Build Distroless
  uses: docker/build-push-action@...
  with:
    file: Dockerfile.distroless
    tags: ghcr.io/manugh/xg2g:${{ github.sha }}-distroless
```

**Option B: Delete Dockerfile.distroless**
If FFmpeg is mandatory, distroless cannot work.

**Recommendation:** **Option B** if FFmpeg mandatory, else **Option A**.
**Effort:** 30 minutes (implement) or 2 minutes (delete)
**Priority:** **P1 HIGH** (clarify intent)

---

### FINDING 5.2: Makefile Is 737 Lines (Approaching Complexity Limit)

**System Assumption:**
Makefile has 60+ targets, some are 20+ lines of bash.

**Breakpoint:**
Target overlap:
- `test-cover` (line 261-278)
- `coverage` (line 316-329)
- `coverage-check` (line 331-344)

All do similar things. Developers confused which to use.

**Decision Demand:**

**Option A: Extract Heavy Targets to Scripts (Recommended)**
Move 10+ line targets to `scripts/`:
```make
sbom: dev-tools
	@./scripts/generate-sbom.sh
```

Keep Makefile as thin interface (< 500 lines).

**Option B: Accept 737 Lines**
Many enterprise projects have 1000+ line Makefiles.

**Recommendation:** **Option A** - Extract to scripts.
**Effort:** 2-3 hours (refactor)
**Priority:** **P2 MEDIUM**

---

## 6. OPENAPI/CODEGEN - API CONTRACT GOVERNANCE

### FINDING 6.1: TypeScript Client Generation Not Validated

**System Assumption:**
WebUI generates client (`package.json` line 9):
```json
"generate-client": "openapi-ts --input ../api/openapi.yaml --output ./src/client-ts"
```

CI validates output is committed but NOT that it compiles.

**Breakpoint:**
If `openapi.yaml` has invalid schema → client generation succeeds but TypeScript build fails.

**Decision Demand:**

**Option A: Validate Client Compiles (Recommended)**
Add to CI after client generation:
```yaml
- name: Validate Client Compiles
  run: |
    cd webui
    npm run type-check  # tsc --noEmit
```

**Recommendation:** **Option A**
**Effort:** 10 minutes (CI/Gate)
**Risk:** None
**Priority:** **P2 MEDIUM**

---

## SUMMARY: PRIORITY MATRIX

| Finding | Priority | Effort | Risk | PR Type |
|---------|----------|--------|------|---------|
| OpenAPI Spec Divergence (2.3) | **P0** | 1h | Medium | Ownership Fix |
| CI Codegen Path Bug (4.2) | **P0** | 2min | None | Hotfix |
| Binaries in Git (1.2) | **P0** | 5min | None | Quick Win |
| Duplicate CI Workflows (4.1) | P1 | 2h | Medium | Refactor |
| Dockerfile.distroless Dead Code (5.1) | P1 | 30min | Low | Decision |
| Breaking Change Detection (4.3) | P1 | 15min | None | CI/Gate |
| Post-Release Smoke Test (4.5) | P1 | 30min | None | CI/Gate |
| Binary Consolidation (1.1) | P2 | 3-4h | Low | Refactor |
| Docs Duplication (2.1) | P2 | 15min | None | Quick Win |
| Scripts vs. Makefile (3.1) | P2 | 30min | Low | Quick Win |
| Pre-commit Enforcement (3.2) | P2 | 20min | None | CI/Gate |
| Makefile Complexity (5.2) | P2 | 2-3h | Low | Refactor |
| Client Validation (6.1) | P2 | 10min | None | CI/Gate |
| Action Pinning (4.4) | P3 | 1h | Low | Security |
| ADR Visibility (2.2) | P3 | 2min | None | Quick Win |

---

## THREE CONCRETE PR PLANS

### PR Plan 1: Quick Win Bundle (P0 + P3 Low-Hanging Fruit)

**Title:** `chore: repository hygiene - binaries, docs, ADRs`

**Changes:**
1. Add binaries to `.gitignore`:
   ```gitignore
   /daemon
   /v3probe
   /xg2g
   /xg2g-smoke
   ```

2. Delete root `ARCHITECTURE.md`, replace with stub linking to `docs/arch/`

3. Add ADR link to root `README.md`

4. Update `scripts/README.md` documenting "DO NOT call directly"

**Files Changed:** 4
**Effort:** 20 minutes
**Risk:** None (docs + gitignore only)
**Validation:**
```bash
git status  # Should not show binaries
grep -q "docs/arch/ARCHITECTURE.md" README.md || exit 1
```

---

### PR Plan 2: CI/Gate Hardening (P0 + P1 + P2 CI Fixes)

**Title:** `ci: enforce OpenAPI contract, pre-commit hooks, post-release verification`

**Changes:**
1. **Fix CI codegen path** (`.github/workflows/ci.yml` line 113):
   ```yaml
   -o internal/control/http/v3/server_gen.go \
   internal/control/http/v3/openapi.yaml
   ```

2. **Add breaking change detection** (`.github/workflows/quality-gates.yml`):
   ```yaml
   - name: Check OpenAPI Breaking Changes
     run: |
       npx @oasdiff/oasdiff breaking \
         <(git show origin/main:api/openapi-v3.yaml) \
         api/openapi-v3.yaml \
         --fail-on ERR
   ```

3. **Enforce pre-commit hooks** (`.github/workflows/ci.yml`):
   ```yaml
   - name: Verify Pre-commit Hooks
     run: |
       pip install pre-commit
       pre-commit run --all-files
   ```

4. **Add post-release smoke test** (`.github/workflows/release.yml`):
   ```yaml
   verify-release:
     needs: [release]
     steps:
       - run: docker pull ghcr.io/manugh/xg2g:${{ github.ref_name }}
       - run: |
           docker run -d --name test ghcr.io/manugh/xg2g:${{ github.ref_name }}
           sleep 10
           docker exec test xg2g healthcheck ready
   ```

5. **Validate TypeScript client compiles**:
   ```yaml
   - name: Validate Generated Client
     run: |
       cd webui
       npm run type-check
   ```

**Files Changed:** 3 (ci.yml, quality-gates.yml, release.yml)
**Effort:** 1 hour
**Risk:** Low (additive gates, no behavior changes)
**Validation:**
```bash
# After PR merge, trigger workflow
gh workflow run ci.yml
gh workflow run quality-gates.yml
```

---

### PR Plan 3: Ownership Fix - OpenAPI Single Source of Truth (P0 CRITICAL)

**Title:** `fix: consolidate OpenAPI spec to single source of truth`

**Problem:** WebUI and backend server generate from DIFFERENT specs → contract divergence risk.

**Changes:**

1. **Move v3 spec to public location:**
   ```bash
   mkdir -p api
   mv internal/control/http/v3/openapi.yaml api/openapi-v3.yaml
   ```

2. **Deprecate v2 spec:**
   ```bash
   mv api/openapi.yaml api/openapi-v2-legacy.yaml
   ```

3. **Update WebUI client generation** (`webui/package.json`):
   ```json
   "generate-client": "openapi-ts --input ../api/openapi-v3.yaml --output ./src/client-ts"
   ```

4. **Update server codegen** (`Makefile` + `.github/workflows/ci.yml`):
   ```yaml
   oapi-codegen ... -o internal/control/http/v3/server_gen.go api/openapi-v3.yaml
   ```

5. **Update OpenAPI validation** (`.github/workflows/ci.yml`):
   ```yaml
   - name: Validate OpenAPI v3
     run: redocly lint api/openapi-v3.yaml
   ```

6. **Add CI gate to prevent dual specs**:
   ```yaml
   - name: Verify Single OpenAPI Source
     run: |
       if [ -f api/openapi.yaml ] && [ -f api/openapi-v3.yaml ]; then
         echo "❌ Multiple OpenAPI specs exist"
         exit 1
       fi
   ```

7. **Update all internal imports:**
   - `internal/control/http/v3/v3.go` (embed spec)
   - Documentation references

**Files Changed:** 8
**Effort:** 1-1.5 hours
**Risk:** Medium (coordinated updates required)
**Validation:**
```bash
# Verify single source
ls api/openapi*.yaml | wc -l  # Should be 2 (v3 + v2-legacy)

# Verify codegen works
make generate
git diff --exit-code internal/control/http/v3/server_gen.go webui/src/client-ts/

# Verify WebUI builds
cd webui && npm run build
```

**Breaking Change:** None (paths change but content identical)
**Rollback Plan:** Revert commit, regenerate from old paths

---

## NEXT ACTIONS (Recommended Order)

1. **Immediate (Today):**
   - Execute PR Plan 1 (Quick Win) - 20 minutes
   - Execute hotfix for CI codegen path (Finding 4.2) - 2 minutes

2. **This Week:**
   - Execute PR Plan 3 (OpenAPI Ownership Fix) - 1.5 hours
   - Execute PR Plan 2 (CI/Gate Hardening) - 1 hour

3. **Next Sprint:**
   - Merge ci.yml + ci-v2.yml (Finding 4.1) - 2 hours
   - Decide on Dockerfile.distroless (Finding 5.1) - 30 minutes
   - Consolidate binaries to subcommands (Finding 1.1) - 3-4 hours

**Total Cleanup Effort (P0-P1):** ~8-10 hours over 5 PRs
**ROI:** Eliminates critical contract risks, hardens CI, improves developer UX

---

## APPENDIX: EXEMPLARY PRACTICES (DO NOT CHANGE)

The following are **excellent** and should be preserved:
- ✅ ADR discipline (11 active ADRs)
- ✅ Go module hygiene (no `replace` directives)
- ✅ Docker digest pinning (`debian@sha256:...`)
- ✅ Security scanning (Trivy, govulncheck, gosec, CodeQL)
- ✅ SBOM generation + Cosign signing
- ✅ Go toolchain policy enforcement
- ✅ Comprehensive Makefile with quality gates
- ✅ CI validates OpenAPI codegen sync

---

**END OF AUDIT**
**Document Version:** 1.0
**Last Updated:** 2026-01-11
