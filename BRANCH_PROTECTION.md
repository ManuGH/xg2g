# Branch Protection Rules

This document describes the branch protection rules configured for the `main` branch.

## Current Configuration

The `main` branch has the following protection rules enabled:

### ✅ Require Pull Request Before Merging
- Changes to `main` must be made through pull requests
- Direct pushes are blocked (except for administrators)
- Helps maintain code quality and review process

### ✅ Require Status Checks
- **2 required status checks** must pass before merging:
  1. CI workflow (tests, linting)
  2. Security scans (CodeQL, gosec, etc.)

### ⚠️ Administrator Bypass
- Repository administrators can bypass these rules
- Use carefully and only when necessary

## Recommended Configuration

To further improve security and code quality, consider enabling:

### 1. Require Approving Reviews
```
Settings → Branches → Branch protection rules → main
- ✅ Require a pull request before merging
- ✅ Require approvals: 1
- ✅ Dismiss stale pull request approvals when new commits are pushed
```

**Benefits:**
- Ensures code is reviewed by another developer
- Catches bugs and security issues early
- Knowledge sharing among team members

### 2. Require Linear History
```
Settings → Branches → Branch protection rules → main
- ✅ Require linear history
```

**Benefits:**
- Enforces rebasing instead of merge commits
- Cleaner git history
- Easier to track changes

### 3. Include Administrators
```
Settings → Branches → Branch protection rules → main
- ✅ Include administrators
```

**Benefits:**
- Enforces rules for everyone (including repo owner)
- Prevents accidental direct pushes
- Ensures consistent workflow

### 4. Require Signed Commits
```
Settings → Branches → Branch protection rules → main
- ✅ Require signed commits
```

**Benefits:**
- Verifies commit authenticity
- Prevents commit spoofing
- Enhanced security

## How to Configure

### Via GitHub Web UI

1. Go to: **Settings → Branches → Add rule**

2. **Branch name pattern:** `main`

3. **Protect matching branches:**
   - ✅ Require a pull request before merging
     - Required approvals: **1**
     - Dismiss stale reviews: **Yes**
   - ✅ Require status checks to pass before merging
     - ✅ Require branches to be up to date before merging
     - Status checks:
       - `CI / test`
       - `CodeQL`
       - `gosec`
   - ✅ Require linear history
   - ✅ Require signed commits (optional)
   - ✅ Include administrators (recommended)
   - ✅ Restrict who can push to matching branches
     - Only allow: Maintainers

4. Click **Create** or **Save changes**

### Via GitHub CLI

```bash
# Install GitHub CLI
brew install gh  # macOS
# or: sudo apt install gh  # Linux

# List current rules
gh api repos/ManuGH/xg2g/branches/main/protection

# Update branch protection (example)
gh api --method PUT \
  repos/ManuGH/xg2g/branches/main/protection \
  --input - <<EOF
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["CI / test", "CodeQL", "gosec"]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismissal_restrictions": {},
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 1
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
EOF
```

## Status Checks to Require

These are the key workflows that should pass before merging:

### Critical (Must Pass)
- **CI** - Main test suite
- **CodeQL** - Security analysis
- **gosec** - Go security scanner

### Recommended
- **Docker Integration Tests** - Container functionality
- **govulncheck** - Vulnerability scanning
- **golangci-lint** - Code quality

### Optional
- **Benchmark** - Performance tests (informational)
- **Fuzz** - Fuzzing tests (can be long-running)

## Bypassing Protection (Emergency)

If you absolutely need to bypass protection (e.g., critical hotfix):

### Temporary Disable
1. Go to: **Settings → Branches → main → Edit**
2. Uncheck **Include administrators**
3. Push your changes
4. **Immediately re-enable** the rule

### Better Alternative: Hotfix Workflow
```bash
# Create hotfix tag directly
git tag v1.5.1-hotfix
git push origin v1.5.1-hotfix

# Then create PR to fix in main
git checkout -b hotfix/critical-issue
# ... make changes ...
git push origin hotfix/critical-issue
gh pr create
```

## Troubleshooting

### "Required status checks are expected"
- Some CI jobs haven't completed yet
- Wait for all checks to finish
- Or re-run failed checks

### "Changes must be made through a pull request"
- You tried to push directly to `main`
- Create a feature branch instead:
  ```bash
  git checkout -b feature/my-change
  git push origin feature/my-change
  gh pr create
  ```

### "1 approving review is required"
- Ask a team member to review your PR
- Address review comments
- Re-request review after changes

## Monitoring

Check branch protection compliance:

```bash
# View protection status
gh api repos/ManuGH/xg2g/branches/main/protection | jq

# View recent pushes to main
gh api repos/ManuGH/xg2g/commits?sha=main | jq '.[].commit.message'

# View open PRs
gh pr list --base main
```

## References

- [GitHub Docs: Branch Protection Rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)
- [GitHub Security Best Practices](https://docs.github.com/en/code-security/getting-started/securing-your-repository)

---

**Questions?** Open a [Discussion](https://github.com/ManuGH/xg2g/discussions).
