# Branch Protection Checklist

Quick reference for updating GitHub branch protection rules during CI migration.

---

## Current State (Legacy CI)

**Workflow:** `.github/workflows/ci.yml`

**Required checks:**
```
build-test-integration
validate-config
```

---

## Target State (New CI)

**Workflow:** `.github/workflows/ci-v2.yml` → `.github/workflows/ci.yml`

**Required checks:**
```
unit-tests
schema-validation
lint
integration-tests
```

**Optional checks (informational only):**
```
transcoder-tests    # Only runs on: schedule, workflow_dispatch, main branch
ci-summary          # Reporting only, always runs
```

---

## Migration Steps

### Step 1: Shadow Phase (Keep Both)
- ✅ Keep legacy checks required
- ✅ New checks run but not required
- ⏱️ Monitor 3-5 PRs

### Step 2: Switchover
1. **Remove** from required:
   - `build-test-integration`
   - `validate-config`

2. **Add** to required:
   - `unit-tests`
   - `schema-validation`
   - `lint`
   - `integration-tests`

3. **⚠️ Do NOT require:**
   - `transcoder-tests` (only runs on schedule/main)
   - `ci-summary` (reporting only)

### Step 3: Cleanup
- 🗑️ Remove/archive `ci.yml`
- 🔄 Rename `ci-v2.yml` → `ci.yml`
- 📝 Update docs

---

## GitHub Settings Path

```
Repository → Settings → Branches → main → Edit (protection rule)
→ Require status checks to pass before merging
→ Status checks that are required
```

---

## Validation Commands

**Test locally before enabling as required:**

```bash
# Unit tests (fast, always should pass)
make test

# Schema tests (requires check-jsonschema)
pip install check-jsonschema
make test-schema

# Lint (requires golangci-lint)
make lint

# Integration tests (requires building binaries)
make test  # or run full integration suite
```

---

## Job Runtime Expectations

| Job | Expected Time | Max Time |
|-----|---------------|----------|
| `unit-tests` | 3-5 min | 10 min |
| `schema-validation` | 2-3 min | 5 min |
| `lint` | 3-7 min | 10 min |
| `integration-tests` | 5-12 min | 15 min |
| `transcoder-tests` | 10-15 min | 20 min |

**⚠️ If jobs exceed max time consistently:**
- Check for flaky tests
- Review test setup/teardown
- Consider caching improvements

---

## Troubleshooting

### "Required check did not run"
**Cause:** Branch protection references job that doesn't exist in workflow

**Fix:**
1. Check exact job name in `ci-v2.yml`: `jobs: → <job-name>:`
2. Verify name matches in branch protection (case-sensitive!)
3. Common mistake: `unit-tests` vs `unit_tests` vs `Unit Tests`

### "Status check is pending"
**Cause:** Job still running or stuck

**Fix:**
1. Check Actions tab for workflow run
2. Cancel and re-run if stuck
3. Check for resource constraints (runner out of disk space, etc.)

### "Check failed but PR was mergeable"
**Cause:** Check not marked as required

**Fix:**
1. Add to required checks list in branch protection
2. Force-push to trigger new check run
3. Verify PR now blocked until passing

---

## Reference: Job Naming

**ci.yml (legacy):**
```yaml
jobs:
  build-test-integration:  # ← old name
  validate-config:         # ← old name
```

**ci-v2.yml (new):**
```yaml
jobs:
  unit-tests:              # ← new name (required)
  schema-validation:       # ← new name (required)
  transcoder-tests:        # ← new name (optional)
  lint:                    # ← new name (required)
  integration-tests:       # ← new name (required)
  ci-summary:              # ← new name (optional)
```

**⚠️ Exact names matter!** Copy from workflow file, not this doc.
