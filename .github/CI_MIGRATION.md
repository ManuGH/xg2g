# CI Migration Plan: Legacy → v2

**Status:** In Progress
**Timeline:** 1-2 weeks (3-5 PRs)
**Strategy:** Shadow running → Switchover → Cleanup

---

## Overview

Migrating from legacy `ci.yml` (nogpu tags) to `ci-v2.yml` (transcoder tags + Make targets).

### Key Changes
- **Build tags:** `nogpu` → default stub / `-tags=transcoder`
- **Job structure:** 1 monolithic → 5 specialized jobs
- **Test targets:** Direct `go test` → `make test`, `make test-schema`, `make test-transcoder`
- **New dependencies:** `check-jsonschema`, Rust toolchain (optional), FFmpeg headers (optional)

---

## Phase 1: Shadow Running (Week 1)

### 1.1 Enable Parallel CI

**Status:** ✅ Complete
**Files:**
- `ci.yml` (legacy, still running)
- `ci-v2.yml` (new, shadow mode)

**Actions:**
```bash
git add .github/workflows/ci-v2.yml
git commit -m "ci: add v2 workflow (shadow mode)"
git push origin <branch>
```

**Verification:**
- [ ] Both workflows trigger on the same PR
- [ ] Legacy checks still show as "required"
- [ ] New checks show as "optional" (informational)

### 1.2 Branch Protection: Keep Legacy Required

**GitHub Settings → Branches → main → Edit protection rule**

**Required status checks (keep as-is):**
- `build-test-integration` (from ci.yml)
- `validate-config` (from ci.yml)

**New checks (not required yet):**
- `unit-tests` (ci-v2.yml)
- `schema-validation` (ci-v2.yml)
- `lint` (ci-v2.yml)
- `integration-tests` (ci-v2.yml)
- `transcoder-tests` (ci-v2.yml, optional)
- `ci-summary` (ci-v2.yml)

### 1.3 Monitor 3-5 PRs

**Checklist per PR:**
- [ ] All new jobs complete successfully
- [ ] No unexpected failures in new jobs
- [ ] Runtime acceptable:
  - `unit-tests`: < 10 min ✅
  - `schema-validation`: < 5 min ✅
  - `lint`: < 10 min ✅
  - `integration-tests`: < 15 min ✅
  - `transcoder-tests`: < 15 min (only on schedule/main) ✅
- [ ] Artifacts uploaded correctly
- [ ] Coverage reports generated
- [ ] No flaky tests

**Track issues:**
```markdown
| PR | unit-tests | schema | lint | integration | Notes |
|----|------------|--------|------|-------------|-------|
| #X | ✅ 5m      | ✅ 3m  | ✅ 7m | ✅ 12m      | All green |
| #Y | ✅ 6m      | ⚠️ 4m  | ✅ 8m | ✅ 11m      | schema: check-jsonschema timeout |
| #Z | ✅ 5m      | ✅ 3m  | ✅ 7m | ✅ 13m      | All green |
```

---

## Phase 2: Switchover (Week 2)

### 2.1 Update Branch Protection Rules

**Trigger:** After 3-5 successful PRs in shadow mode

**GitHub Settings → Branches → main → Edit protection rule**

**Remove from required checks:**
- ❌ `build-test-integration` (legacy)
- ❌ `validate-config` (legacy, now part of schema-validation)

**Add to required checks:**
- ✅ `unit-tests` (ci-v2.yml)
- ✅ `schema-validation` (ci-v2.yml)
- ✅ `lint` (ci-v2.yml)
- ✅ `integration-tests` (ci-v2.yml)
- ⚠️ `transcoder-tests` (optional, only runs on schedule/main)
- ⚠️ `ci-summary` (optional, only for reporting)

**⚠️ Important:**
- Apply these changes **before** removing `ci.yml`
- Otherwise PRs will be blocked waiting for legacy checks that no longer run

### 2.2 Verify New Required Checks

**Test on a dummy PR:**
- [ ] PR cannot be merged without `unit-tests` passing
- [ ] PR cannot be merged without `schema-validation` passing
- [ ] PR cannot be merged without `lint` passing
- [ ] PR cannot be merged without `integration-tests` passing
- [ ] `transcoder-tests` does not block PR (only runs on schedule/main)

### 2.3 Announce Migration

**In team channel / PR:**
```markdown
🚀 CI Migration Complete - New Workflow Active

The CI pipeline has been upgraded:
- ✅ Faster parallel jobs
- ✅ Stub transcoder by default (no Rust dependency)
- ✅ Optional Rust/CGO tests (nightly)
- ✅ Explicit schema validation

**For Contributors:**
- Local testing: `make test` (fast, no Rust)
- Transcoder tests: `make test-transcoder` (requires Rust)
- Schema tests: `make test-schema` (requires check-jsonschema)

See: CONTRIBUTING.md for updated instructions
```

---

## Phase 3: Cleanup (End of Week 2)

### 3.1 Remove Legacy CI

**Trigger:** After 5+ PRs successfully using new branch protection

**Option A: Archive (safer)**
```bash
git mv .github/workflows/ci.yml .github/workflows/ci-legacy.yml.archived
git commit -m "ci: archive legacy workflow"
```

**Option B: Delete (cleaner)**
```bash
git rm .github/workflows/ci.yml
git commit -m "ci: remove legacy workflow"
```

**Verification:**
- [ ] No "required check did not run" errors on new PRs
- [ ] All PRs merge smoothly
- [ ] No complaints from contributors

### 3.2 Rename ci-v2.yml → ci.yml (Optional)

**Make the new CI canonical:**
```bash
git mv .github/workflows/ci-v2.yml .github/workflows/ci.yml
git commit -m "ci: promote v2 to canonical ci.yml"
```

**Then update branch protection again:**
- Job names stay the same (`unit-tests`, `lint`, etc.)
- Workflow name changes from `CI v2` to `CI`

### 3.3 Update Documentation

**Files to update:**
- [ ] `CONTRIBUTING.md` - Add test commands
- [ ] `README.md` - Update build instructions
- [ ] `.github/CI_MIGRATION.md` - Mark as complete

---

## Rollback Plan

**If new CI fails catastrophically:**

### Quick Rollback (< 5 min)
```bash
# 1. Revert branch protection to legacy checks
# 2. Disable ci-v2.yml trigger
git mv .github/workflows/ci-v2.yml .github/workflows/ci-v2.yml.disabled
git commit -m "ci: emergency disable v2 workflow"
git push
```

### Investigation Checklist
- [ ] Check GitHub Actions logs for exact error
- [ ] Verify all dependencies available (Go, Python, Node, Rust)
- [ ] Test locally: `make test`, `make test-schema`
- [ ] Check for OS-specific issues (Linux vs macOS)
- [ ] Review recent changes to Makefile or build tags

### Recovery
- Fix issues in `ci-v2.yml`
- Re-enable: `git mv ci-v2.yml.disabled ci-v2.yml`
- Restart shadow phase

---

## Success Criteria

**Phase 1 Complete:**
- ✅ 3-5 PRs run both CIs successfully
- ✅ New jobs stable, no flakiness
- ✅ Runtime within budget

**Phase 2 Complete:**
- ✅ Branch protection switched to new jobs
- ✅ 5+ PRs merged with new CI only
- ✅ No merge blockers

**Phase 3 Complete:**
- ✅ Legacy CI removed/archived
- ✅ Documentation updated
- ✅ Contributors onboarded

---

## Timeline Checkpoints

| Date | Milestone | Status |
|------|-----------|--------|
| 2025-11-27 | ci-v2.yml created | ✅ |
| TBD | First PR with shadow CI | ⏳ |
| TBD | 3rd PR verified | ⏳ |
| TBD | Branch protection switched | ⏳ |
| TBD | Legacy CI removed | ⏳ |
| TBD | Documentation updated | ⏳ |

---

## Notes

### Why Shadow Running?
- New dependencies (check-jsonschema, Rust)
- New build tags (transcoder vs nogpu)
- New Make targets vs direct go commands
- New job structure (5 jobs vs 1 monolith)

Too many moving parts for direct cutover without validation.

### Why Not Keep Both Forever?
- Maintenance burden (2x CI updates)
- Confusion for contributors
- Branch protection complexity
- Duplicate compute costs

Clear sunset keeps things clean.
