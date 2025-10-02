# Maintenance Procedures

This document outlines recurring maintenance tasks to keep the repository healthy and secure.

## Monthly Checks

### 1. Branch Protection Configuration Audit

**Frequency**: Monthly (1st of month)
**Owner**: Repository maintainer
**Duration**: ~5 minutes

**Procedure**:

```bash
# Fetch current branch protection config
gh api repos/ManuGH/xg2g/branches/main/protection | jq '.' > /tmp/branch_protection_$(date +%Y%m).json

# Verify required settings
gh api repos/ManuGH/xg2g/branches/main/protection | jq '{
  admins_enforced: .enforce_admins.enabled,
  required_reviews: .required_pull_request_reviews.required_approving_review_count,
  codeowners_required: .required_pull_request_reviews.require_code_owner_reviews,
  required_checks: .required_status_checks.contexts,
  linear_history: .required_linear_history.enabled,
  conversation_resolution: .required_conversation_resolution.enabled
}'
```

**Expected Output**:

```json
{
  "admins_enforced": true,
  "required_reviews": 1,
  "codeowners_required": true,
  "required_checks": [
    "Static Analysis & Security",
    "Test with Race Detection",
    "Coverage Analysis",
    "Dependency Security Check",
    "Generate SBOM"
  ],
  "linear_history": true,
  "conversation_resolution": true
}
```

**Action Items**:
- If any value is `false` or missing, re-apply protection rules from `.github/branch-protection.json` (if maintained)
- If required_checks count ≠ 5, investigate which check was added/removed
- Store monthly snapshot for audit trail

---

### 2. Dependabot PR Review

**Frequency**: Weekly
**Owner**: Repository maintainer
**Duration**: ~10 minutes

**Procedure**:

```bash
# List open Dependabot PRs
gh pr list --author app/dependabot --state open --json number,title,url,statusCheckRollup

# For each PR, check CI status
gh pr checks <PR-number>
```

**Merge Criteria**:

✅ **Safe to Merge**:
- All required CI checks pass (green)
- No breaking changes in dependency changelog
- Patch or minor version bump (e.g., 1.2.3 → 1.2.4 or 1.2.0 → 1.3.0)

⚠️ **Review Required**:
- Major version bump (e.g., 1.x.x → 2.0.0)
- Security-labeled PRs (review CVE details)
- Failed tests (investigate before merging)

❌ **Do Not Merge**:
- CI checks failing
- Known regressions in dependency issue tracker
- Breaking changes without code updates

**Commands**:

```bash
# Merge PR if all checks pass
gh pr merge <PR-number> --squash --auto

# Close PR if incompatible
gh pr close <PR-number> -c "Incompatible with current codebase, tracking in issue #XXX"
```

---

### 3. SBOM Verification

**Frequency**: Monthly (after releases)
**Owner**: Repository maintainer
**Duration**: ~5 minutes

**Procedure**:

```bash
# List recent releases
gh release list --limit 5

# For latest release, verify all assets present
gh release view <tag> --json assets --jq '.assets[].name'
```

**Expected Assets** (per release):

- ✅ `xg2g-linux-amd64.zip`
- ✅ `xg2g-linux-arm64.zip`
- ✅ `xg2g-darwin-amd64.zip`
- ✅ `xg2g-darwin-arm64.zip`
- ✅ `xg2g-windows-amd64.zip`
- ✅ `xg2g-windows-arm64.zip`
- ✅ `checksums.txt`
- ✅ `sbom.spdx.json`
- ✅ `sbom.cyclonedx.json`
- ✅ `sbom.txt`

**Validation**:

```bash
# Download and verify SBOM content
gh release download <tag> -p 'sbom.*'

# Check SPDX SBOM has required fields
cat sbom.spdx.json | jq '{
  spdxVersion: .spdxVersion,
  name: .name,
  packages: .packages | length,
  creationInfo: .creationInfo.created
}'

# Check CycloneDX SBOM
cat sbom.cyclonedx.json | jq '{
  bomFormat: .bomFormat,
  specVersion: .specVersion,
  components: .components | length
}'
```

**Action Items**:
- If assets missing, investigate release workflow failure
- If SBOM empty or malformed, re-run `make sbom` locally and investigate

---

### 4. Security Scan Summary

**Frequency**: Weekly
**Owner**: Repository maintainer
**Duration**: ~5 minutes

**Procedure**:

```bash
# Run local security checks
make security

# Check GitHub Security tab
gh api repos/ManuGH/xg2g/vulnerability-alerts | jq '.[] | {
  package: .security_vulnerability.package.name,
  severity: .security_vulnerability.severity,
  patched: .security_vulnerability.first_patched_version.identifier
}'

# Review CodeQL alerts
gh api repos/ManuGH/xg2g/code-scanning/alerts --paginate | jq '.[] | select(.state=="open") | {
  rule: .rule.id,
  severity: .rule.security_severity_level,
  location: .most_recent_instance.location.path
}'
```

**Action Items**:
- **High/Critical vulnerabilities**: Create hotfix PR immediately (see [docs/incident.md](incident.md))
- **Medium vulnerabilities**: Schedule fix in next sprint
- **Low vulnerabilities**: Track in backlog

---

### 5. CI Workflow Health Check

**Frequency**: Monthly
**Owner**: Repository maintainer
**Duration**: ~10 minutes

**Procedure**:

```bash
# Check recent workflow runs
gh run list --limit 20 --json conclusion,name,createdAt,workflowName

# Identify failures
gh run list --status failure --limit 10

# Review specific failed run
gh run view <run-id> --log-failed
```

**Metrics to Track**:

| Metric | Target | Action if Exceeded |
|--------|--------|-------------------|
| Failure rate | < 5% | Investigate flaky tests |
| Average duration | < 10 min | Optimize CI (caching, parallelization) |
| Workflow updates | Quarterly | Review for deprecated actions |

**Common Issues**:
- Flaky tests: Add retries or fix race conditions
- Slow builds: Review cache hit rate, consider matrix optimization
- Deprecated actions: Update to latest versions (use Dependabot)

---

## Quarterly Tasks

### 1. Dependency Audit

**Frequency**: Quarterly (Jan, Apr, Jul, Oct)
**Duration**: ~30 minutes

```bash
# Generate full dependency tree
go mod graph > /tmp/deps_$(date +%Y%m).txt

# Check for outdated dependencies
go list -u -m all

# Check for unused dependencies
go mod tidy

# Review replace directives
grep ^replace go.mod
```

**Action Items**:
- Remove unused dependencies
- Upgrade outdated non-breaking dependencies
- Document rationale for pinned versions
- Remove obsolete replace directives

---

### 2. Documentation Review

**Frequency**: Quarterly
**Duration**: ~1 hour

**Checklist**:

- [ ] [README.md](../README.md) - Verify all examples work, ENV vars current
- [ ] [SECURITY.md](../SECURITY.md) - Update supported versions, response timelines
- [ ] [SUPPORT.md](../SUPPORT.md) - Update response times based on actual metrics
- [ ] [CONTRIBUTING.md](../CONTRIBUTING.md) - Verify development setup instructions
- [ ] [docs/incident.md](incident.md) - Update based on real incidents
- [ ] [CHANGELOG.md](../CHANGELOG.md) - Ensure all releases documented

---

### 3. Performance Baseline

**Frequency**: Quarterly
**Duration**: ~1 hour

```bash
# Run benchmarks
go test -bench=. -benchmem ./... > /tmp/bench_$(date +%Y%m).txt

# Compare to previous quarter
# diff /tmp/bench_202401.txt /tmp/bench_202404.txt

# Run load test (if available)
make load-test

# Check memory usage in production
kubectl top pod -l app=xg2g
```

**Action Items**:
- If performance degraded > 20%, investigate recent changes
- Update performance targets in documentation
- Consider optimization for hotspots

---

## Annual Tasks

### 1. Go Version Upgrade

**Frequency**: Annually (when new Go stable released)
**Duration**: ~2 hours

```bash
# Update go.mod
go mod edit -go=1.25

# Update CI workflows
# Edit .github/workflows/*.yml - change GO_VERSION

# Test locally
make test
make lint
make build

# Create PR with full test suite
```

---

### 2. Security Policy Review

**Frequency**: Annually
**Duration**: ~30 minutes

- Review [SECURITY.md](../SECURITY.md) response timelines
- Update severity classification if needed
- Review incident response procedures
- Update contact information

---

### 3. License Compliance

**Frequency**: Annually
**Duration**: ~1 hour

```bash
# Generate license report
go-licenses report ./... > /tmp/licenses.txt

# Check for incompatible licenses (GPL, AGPL)
cat /tmp/licenses.txt | grep -i 'GPL'

# Update SBOM with license info
make sbom
```

---

## Adding New CI Jobs to Required Checks

When a new CI job is added and should be required for PR merges:

```bash
# Get current required checks
gh api repos/ManuGH/xg2g/branches/main/protection | jq '.required_status_checks.contexts'

# Add new check (replace entire array)
gh api -X PATCH repos/ManuGH/xg2g/branches/main/protection/required_status_checks \
  -f strict=true \
  -f contexts[]='Static Analysis & Security' \
  -f contexts[]='Test with Race Detection' \
  -f contexts[]='Coverage Analysis' \
  -f contexts[]='Dependency Security Check' \
  -f contexts[]='Generate SBOM' \
  -f contexts[]='New Job Name Here'

# Verify
gh api repos/ManuGH/xg2g/branches/main/protection | jq '.required_status_checks.contexts'
```

**Important**: All existing checks must be included in the `contexts[]` array when adding a new one.

---

## Automation Opportunities

Consider automating these checks with:

1. **GitHub Actions scheduled workflows** for monthly audits
2. **Slack/email notifications** for failed Dependabot PRs
3. **Prometheus alerts** for production metrics
4. **Automated PR creation** for quarterly dependency updates

---

## Maintenance Log

Keep a log of maintenance activities:

```bash
# Create maintenance log entry
echo "$(date -Iseconds) - Monthly branch protection audit - Status: OK" >> docs/maintenance.log
```

Example log:
```text
2024-01-01T10:00:00+00:00 - Monthly branch protection audit - Status: OK
2024-01-08T14:30:00+00:00 - Dependabot PR review - 3 PRs merged, 1 closed
2024-01-15T09:00:00+00:00 - Security scan - No high/critical issues
2024-02-01T10:00:00+00:00 - Monthly branch protection audit - Status: OK
```
