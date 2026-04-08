# CI/CD Workflows & Quality Gates

This directory contains CI/CD workflows and checks to maintain repository quality.

## Current CI Roles

- `.github/workflows/ci.yml`
  Core PR/push gate that runs the broad make-based validation bundle.
- `.github/workflows/pr-required-gates.yml`
  Canonical PR-time WebUI integration gate: WebUI build plus browser-backed smoke.
- `.github/workflows/lint.yml`
  Scoped invariant backstop. Its scope resolution is intentionally fail-closed.
- `.github/workflows/ui-contract.yml`
  Historical compatibility workflow. PR enforcement is owned by `pr-required-gates.yml`;
  this workflow remains as a push/manual backstop until an explicit branch-protection cutover.
- `.github/workflows/phase4-guardrails.yml`
  Historical compatibility workflow with the same role split as `ui-contract.yml`.
- `.github/workflows/repo-health.yml`
  Repository hygiene and drift-prevention checks. This is the normative merge-policy
  authority for repo-health invariants; local Make targets only mirror it for convenience.

See [docs/ops/CI_POLICY.md](../docs/ops/CI_POLICY.md) for the canonical policy.

## Workflows

### `repo-health.yml`

Automated checks to prevent common repository hygiene issues:

**1. Large File Prevention**

- **Trigger:** Every PR and push to main
- **Check:** Detects files >5MB in commits
- **Purpose:** Prevent binary bloat, keep repo fast to clone
- **Solutions if failed:**
  - Move to `testdata/` (gitignored)
  - Use Git LFS for necessary large files
  - Host externally (CDN/S3)

**2. Test Assets Location**

- **Trigger:** Every PR and push to main
- **Check:** Ensures no test files (*.mp4, *.ts, test_*) in repo root
- **Purpose:** Maintain clean root directory structure
- **Solutions if failed:**
  - Move files to `testdata/` directory

## Local Git Hooks

### Pre-commit Hook

Install the pre-commit hook to catch issues locally before pushing:

```bash
# Link the hook
ln -s ../../.githooks/pre-commit .git/hooks/pre-commit

# Or copy it
cp .githooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

**What it checks:**

- Files >5MB in staged commits
- Provides helpful error messages and solutions

**Bypass (NOT recommended):**

```bash
git commit --no-verify
```

## Configuration

**Thresholds:**

- Max file size: 5MB (configurable in workflows/hooks)
- Test file patterns: `*.mp4`, `*.ts`, `*test*.mp4`, etc.

**Adjustment:**
Edit `XG2G_MAX_FILE_SIZE_MB` handling in:

- `.githooks/pre-commit`
- `backend/scripts/ci/check-large-files.sh`

## Why These Checks?

**Problem:** Test/media files in Git cause:

- Slow clone times (large .git folder)
- CI/CD bloat
- Developer frustration

**Solution:** Enforce testdata/ structure via automation

---

**Maintained by:** Engineering Team  
**Last Updated:** 2026-04-08
