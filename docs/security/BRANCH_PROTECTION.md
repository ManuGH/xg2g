# Branch Protection Rules

This document describes the recommended branch protection rules for the xg2g repository to enforce security and quality standards.

## Overview

Branch protection rules help maintain code quality and security by:
- Requiring successful CI checks before merging
- Enforcing code review requirements
- Preventing accidental deletions or force pushes
- Ensuring all security checks pass

## Recommended Settings

### Required Status Checks

The following CI checks must pass before merging to `main`:

| Check Name | Purpose | Workflow File |
|------------|---------|---------------|
| **actionlint** | Validates GitHub Actions workflows | `.github/workflows/actionlint.yml` |
| **conftest** | Enforces Dockerfile and Kubernetes security policies | `.github/workflows/conftest.yml` |
| **CI** | Builds and tests all packages | `.github/workflows/ci.yml` |
| **Coverage** | Ensures code coverage standards | `.github/workflows/coverage.yml` |
| **Security Scan** | Runs gosec and govulncheck | `.github/workflows/gosec.yml`, `.github/workflows/govulncheck.yml` |
| **Container Security** | Scans container images with Trivy | `.github/workflows/container-security.yml` |
| **CodeQL** | Static analysis for security vulnerabilities | `.github/workflows/codeql.yml` |
| **SBOM** | Generates Software Bill of Materials | `.github/workflows/sbom.yml` |

### Pull Request Requirements

- **Require approvals**: At least 1 approving review
- **Dismiss stale reviews**: When new commits are pushed
- **Require review from code owners**: If CODEOWNERS file exists
- **Restrict who can dismiss reviews**: Repository maintainers only

### Additional Protections

- **Require linear history**: Prevent merge commits
- **Require signed commits**: All commits must be GPG signed
- **Include administrators**: Apply rules to administrators too
- **Allow force pushes**: Disabled
- **Allow deletions**: Disabled

## Implementation

### Using GitHub CLI

```bash
#!/bin/bash
# Configure branch protection for main branch

OWNER="ManuGH"
REPO="xg2g"
BRANCH="main"

# Note: This requires admin permissions on the repository
gh api -X PUT \
  -H "Accept: application/vnd.github+json" \
  "/repos/$OWNER/$REPO/branches/$BRANCH/protection" \
  --input - <<EOF
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "actionlint",
      "conftest",
      "CI",
      "Coverage",
      "Security Scan",
      "Container Security",
      "CodeQL",
      "SBOM"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismissal_restrictions": {},
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 1,
    "require_last_push_approval": false
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false,
  "required_conversation_resolution": true,
  "lock_branch": false,
  "allow_fork_syncing": true
}
EOF
```

### Using GitHub Web UI

1. Go to **Settings** → **Branches**
2. Click **Add rule** or edit existing `main` branch rule
3. Configure the following:
   - Branch name pattern: `main`
   - ✅ Require a pull request before merging
     - ✅ Require approvals (1)
     - ✅ Dismiss stale pull request approvals when new commits are pushed
   - ✅ Require status checks to pass before merging
     - ✅ Require branches to be up to date before merging
     - Select required checks (see table above)
   - ✅ Require conversation resolution before merging
   - ✅ Require signed commits
   - ✅ Require linear history
   - ✅ Include administrators
   - ❌ Allow force pushes
   - ❌ Allow deletions

## Bypass Permissions

Only the following roles can bypass branch protection:

- **Repository administrators**: For emergency fixes
- **GitHub Actions**: For automated releases and dependency updates

**Important**: All bypasses are logged and should be reviewed regularly.

## Emergency Procedures

In case of urgent security patches or critical bugs:

1. Create a hotfix branch from `main`
2. Make minimal changes required to fix the issue
3. Open a PR with the `urgent` label
4. Request expedited review from maintainers
5. Merge once required checks pass

**Never bypass branch protection without documented justification.**

## Monitoring

Review branch protection bypasses monthly:

```bash
# List recent pushes to main (including bypasses)
gh api repos/ManuGH/xg2g/events \
  --jq '.[] | select(.type=="PushEvent" and .payload.ref=="refs/heads/main") | {created_at, actor: .actor.login, commits: .payload.commits[].message}'
```

## Related Documentation

- [Security Policy](SECURITY.md)
- [Contributing Guidelines](../CONTRIBUTING.md)
- [CI/CD Documentation](../ci-cd.md)
- [Security Audit Checklist](../SECURITY_AUDIT_CHECKLIST.md)

## References

- [GitHub Branch Protection Rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
- [Required Status Checks](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches#require-status-checks-before-merging)
- [Signed Commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification)
