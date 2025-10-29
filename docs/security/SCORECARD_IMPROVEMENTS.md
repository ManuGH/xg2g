# OpenSSF Scorecard Improvements

Current Score: **4.5/10**
Target Score: **8.5/10**

## Immediate Wins (High Impact, Low Effort)

### 1. Token-Permissions (Current: 0/10 → Target: 10/10)

**Problem:** Workflows have excessive or missing permissions declarations.

**Solution:** Add explicit minimal permissions to all workflows.

**Template for workflows:**
```yaml
# At workflow level - minimal default
permissions:
  contents: read

jobs:
  job-name:
    # Override per-job if needed
    permissions:
      contents: read
      pull-requests: write  # Only if creating/updating PRs
      security-events: write  # Only for SARIF uploads
```

**Files to update:**
- All workflows in `.github/workflows/`
- Ensure each job has minimal required permissions

---

### 2. Dangerous-Workflow (Current: 0/10 → Target: 10/10)

**Problem:** Script injection risks from untrusted inputs.

**Dangerous patterns:**
```yaml
# ❌ UNSAFE - Direct interpolation
run: echo "Title: ${{ github.event.pull_request.title }}"
run: npm install ${{ github.event.inputs.package }}
```

**Safe patterns:**
```yaml
# ✅ SAFE - Use environment variables
env:
  PR_TITLE: ${{ github.event.pull_request.title }}
run: echo "Title: $PR_TITLE"

# ✅ SAFE - Use intermediate variables
- name: Set package name
  id: pkg
  run: echo "name=${{ github.event.inputs.package }}" >> "$GITHUB_OUTPUT"
- name: Install
  run: npm install "${{ steps.pkg.outputs.name }}"
```

**Action items:**
1. Audit all `run:` steps for `${{}}` interpolation
2. Move untrusted inputs to `env:` blocks
3. Use `"$VAR"` quoting in shell scripts
4. Never use `eval` or similar with user input

---

### 3. Pinned-Dependencies (Current: 0/10 → Target: 8/10)

**Problem:** GitHub Actions use tags (@v4) instead of SHA hashes.

**Solution:** Use Dependabot to automatically pin and update actions.

**Current state:**
- ✅ Dependabot configured in `.github/dependabot.yml`
- ✅ Weekly updates scheduled for Monday 04:00
- ⏳ Waiting for Dependabot to create PRs with pinned SHAs

**Manual pinning format:**
```yaml
# ❌ Tag-based (mutable)
uses: actions/checkout@v4

# ✅ SHA-based (immutable) with comment
uses: actions/checkout@8efa7b2f...  # v4.3.0
```

**Why not manual?**
- 100+ action references across 19 workflows
- Dependabot will automatically update SHAs when new versions release
- Reduces maintenance burden

---

### 4. Branch-Protection (Current: 1/10 → Target: 9/10)

**Problem:** Branch protection rules not maximally configured.

**Solution:** Apply comprehensive branch protection rules.

**Script available:** `docs/security/BRANCH_PROTECTION.md`

**Required settings:**
```bash
gh api repos/ManuGH/xg2g/branches/main/protection -X PUT -f "$(cat <<'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["build-test-integration", "Scorecard analysis"]
  },
  "enforce_admins": false,
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
  "required_conversation_resolution": true
}
EOF
)"
```

**Run command:**
```bash
./docs/security/apply-branch-protection.sh
```

---

### 5. Code-Review (Current: 0/10 → Target: 10/10)

**Problem:** No required approvals for PRs.

**Solution:** Enable branch protection with required reviews (handled by #4 above).

**Impact:** Automatically improved when branch protection is applied.

---

### 6. Signed-Releases (Current: 0/10 → Target: 10/10)

**Problem:** Release artifacts are not cryptographically signed.

**Solution:** Enable Cosign signing in release workflow.

**Current state:**
- ✅ Cosign already configured in `.github/workflows/release.yml`
- ✅ SBOM generation active
- ⚠️ Signing commented out (needs keyless signing setup)

**Required changes to `release.yml`:**
```yaml
- name: Install Cosign
  uses: sigstore/cosign-installer@v3

- name: Sign binaries with Cosign (keyless)
  env:
    COSIGN_EXPERIMENTAL: "1"
  run: |
    cd dist
    for file in xg2g-*; do
      cosign sign-blob --yes "$file" --output-signature "${file}.sig" --output-certificate "${file}.pem"
    done

- name: Verify signatures
  env:
    COSIGN_EXPERIMENTAL: "1"
  run: |
    cd dist
    for file in xg2g-linux-amd64 xg2g-linux-arm64 xg2g-darwin-amd64 xg2g-windows-amd64.exe; do
      cosign verify-blob --signature "${file}.sig" --certificate "${file}.pem" "$file"
    done
```

---

## Implementation Priority

### Phase 1: Quick Wins (30 minutes)
1. ✅ Add workflow-level `permissions: contents: read` to all workflows
2. ✅ Audit and fix dangerous workflow patterns
3. ✅ Apply branch protection rules

**Expected score increase: 4.5 → 7.0**

### Phase 2: Automation (Dependabot-driven)
1. ⏳ Wait for Dependabot PRs to pin actions to SHA
2. ⏳ Review and merge Dependabot PRs

**Expected score increase: 7.0 → 8.0**

### Phase 3: Release Hardening (Next release)
1. Enable Cosign signing in release workflow
2. Document signature verification in README

**Expected score increase: 8.0 → 8.5+**

---

## Current Excellent Scores (Keep These)

✅ **CI-Tests: 10/10** - All PRs require tests
✅ **Dependency-Update-Tool: 10/10** - Renovate + Dependabot
✅ **Fuzzing: 10/10** - Go native fuzzing integrated
✅ **License: 10/10** - MIT License
✅ **Packaging: 10/10** - GitHub Actions releases
✅ **SAST: 10/10** - CodeQL on all commits
✅ **Security-Policy: 10/10** - SECURITY.md present
✅ **Vulnerabilities: 10/10** - No known CVEs

---

## Monitoring

Check score updates at:
- https://securityscorecards.dev/viewer/?uri=github.com/ManuGH/xg2g
- https://api.securityscorecards.dev/projects/github.com/ManuGH/xg2g

Scorecard runs:
- Weekly on Monday 03:00 UTC
- On every push to main
- Manual trigger via workflow_dispatch
