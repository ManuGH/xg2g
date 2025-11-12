# GitHub Actions Security Best Practices

## Overview

This document describes security best practices for GitHub Actions workflows in the xg2g repository, focusing on action pinning to commit SHAs.

## Why Pin Actions to Commit SHAs?

### Security Benefits

1. **Prevents Tag Hijacking**: Tags like `@v5` can be moved to point to malicious code
2. **Supply Chain Attack Prevention**: Protects against compromised action repositories
3. **Immutable References**: Commit SHAs cannot be changed after creation
4. **Audit Trail**: Clear record of exactly which code version is running

### Industry Standards

- **OpenSSF Scorecard**: Requires pinning to commit SHAs for high security score
- **SLSA Framework**: Level 2+ requires hermetic builds with pinned dependencies
- **CIS Benchmarks**: Recommends pinning all third-party actions

## Current State (Before Pinning)

```yaml
# ❌ NOT SECURE: Tag can be moved
- uses: actions/checkout@v5

# ❌ NOT SECURE: Branch can be updated
- uses: actions/setup-go@main
```

## Recommended Practice (After Pinning)

```yaml
# ✅ SECURE: Pinned to immutable commit SHA
- uses: actions/checkout@a81bbbf8298c0fa03ea29cdc473d45769f953675  # v5.0.0

# ✅ SECURE: With explanatory comment
- uses: actions/setup-go@41d4c13e14c99f68d48e41a9e4bd0e5c0d29b1c2  # v6.0.0
```

## Automated Management with Dependabot

The xg2g repository already has Dependabot configured for GitHub Actions in `.github/dependabot.yml`:

```yaml
- package-ecosystem: "github-actions"
  directory: "/"
  schedule:
    interval: "weekly"
```

### How Dependabot Helps

1. **Automatic Updates**: Dependabot will create PRs to update action SHAs
2. **Version Comments**: Maintains human-readable version comments
3. **Weekly Checks**: Scans for new action versions every Monday
4. **Security Alerts**: Notifies about vulnerable actions

### Dependabot Workflow

```
1. Dependabot detects new action version
2. Creates PR with updated SHA
3. Includes changelog and release notes
4. CI runs on the PR
5. Manual review and merge
```

## Migration Plan

### Phase 1: Enable SHA Pinning

Add the following configuration to `.github/dependabot.yml`:

```yaml
- package-ecosystem: "github-actions"
  directory: "/"
  schedule:
    interval: "weekly"
  # Request Dependabot to use commit SHAs
  # This is automatic - no additional config needed!
  # Dependabot will maintain both SHA and version comment
```

**Note**: Dependabot automatically pins to commit SHAs when updating actions. No additional configuration required!

### Phase 2: Bulk Migration (Optional)

For immediate migration of all workflows, use this command:

```bash
# Install pin-github-action tool
go install github.com/mheap/pin-github-action@latest

# Pin all actions in workflow files
find .github/workflows -name '*.yml' -exec pin-github-action {} \;
```

### Phase 3: Verification

After migration, verify all actions are pinned:

```bash
# Check for unpinned actions (should return nothing)
grep -r "uses:.*@v[0-9]" .github/workflows/

# Verify SHA format
grep -r "uses:.*@[a-f0-9]\{40\}" .github/workflows/
```

## Workflow Examples

### Before (Tag-Based)

```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go test ./...
```

### After (SHA-Pinned)

```yaml
name: CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@a81bbbf8298c0fa03ea29cdc473d45769f953675  # v5.0.0
      - uses: actions/setup-go@41d4c13e14c99f68d48e41a9e4bd0e5c0d29b1c2  # v6.0.0
        with:
          go-version-file: go.mod
      - run: go test ./...
```

## Finding Commit SHAs

### Method 1: GitHub Releases Page

1. Go to action repository (e.g., `actions/checkout`)
2. Navigate to "Releases"
3. Find the desired version (e.g., `v5.0.0`)
4. Click on the commit SHA
5. Copy the full 40-character SHA

### Method 2: Git Command

```bash
# Clone the action repository
git clone https://github.com/actions/checkout

# Find SHA for a specific tag
git rev-parse v5.0.0
# Output: a81bbbf8298c0fa03ea29cdc473d45769f953675
```

### Method 3: GitHub API

```bash
# Get commit SHA for a tag
curl -s https://api.github.com/repos/actions/checkout/git/refs/tags/v5.0.0 \
  | jq -r '.object.sha'
```

## Maintenance Strategy

### Regular Updates

1. **Weekly**: Dependabot automatically checks for updates
2. **Review PRs**: Manually review and approve Dependabot PRs
3. **Test Changes**: CI runs automatically on all PRs
4. **Merge**: Merge after successful tests and review

### Security Alerts

GitHub will alert about:
- Vulnerable action versions
- Deprecated actions
- Actions with known security issues

### Rollback Procedure

If an action update causes issues:

```bash
# Revert to previous commit
git revert <commit-sha>

# Or cherry-pick specific changes
git cherry-pick <working-commit-sha>
```

## Exceptions

### When to Use Tags

Some actions may legitimately use tags:

```yaml
# Self-hosted actions in same repository
- uses: ./.github/actions/custom-action

# Local composite actions
- uses: ./build/actions/deploy
```

### Trusted First-Party Actions

While still recommended to pin, these are generally safe:
- `actions/*` (GitHub first-party)
- `github/*` (GitHub first-party)

However, for **maximum security**, pin even these to SHAs.

## Security Scanning

### Action Linting

Use `actionlint` to detect security issues:

```bash
# Install actionlint
go install github.com/rhysd/actionlint/cmd/actionlint@latest

# Scan all workflows
actionlint .github/workflows/*.yml
```

### Scorecard

Use OpenSSF Scorecard to audit security:

```bash
# Run scorecard
docker run -e GITHUB_TOKEN=$GITHUB_TOKEN \
  gcr.io/openssf/scorecard:latest \
  --repo=github.com/ManuGH/xg2g \
  --show-details
```

## References

- [GitHub Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [OpenSSF Scorecard](https://github.com/ossf/scorecard)
- [SLSA Framework](https://slsa.dev/)
- [Dependabot Configuration](https://docs.github.com/en/code-security/dependabot/dependabot-version-updates/configuration-options-for-the-dependabot.yml-file)

## Current Status

- ✅ Dependabot configured for GitHub Actions
- ✅ Weekly automated checks enabled
- ⏳ Migration to SHA pinning (automated via Dependabot PRs)
- ✅ Security monitoring active

## Next Steps

1. **Let Dependabot Work**: Allow Dependabot to gradually migrate actions to SHAs
2. **Review PRs**: Approve Dependabot PRs as they arrive
3. **Optional**: Use `pin-github-action` tool for immediate bulk migration
4. **Monitor**: Watch for security alerts and address promptly

---

**Last Updated**: 2025-11-01
**Owner**: @ManuGH
**Status**: ⏳ In Progress (Automated via Dependabot)
