# CI/CD Workflows & Quality Gates

This directory contains CI/CD workflows and checks to maintain repository quality.

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
Edit `MAX_SIZE_MB` in:

- `.githooks/pre-commit`
- `.github/workflows/repo-health.yml`

## Why These Checks?

**Problem:** Test/media files in Git cause:

- Slow clone times (large .git folder)
- CI/CD bloat
- Developer frustration

**Solution:** Enforce testdata/ structure via automation

---

**Maintained by:** Engineering Team  
**Last Updated:** 2026-01-07
