# Coverage Operations Runbook

**Project:** xg2g
**Version:** 1.0
**Last Updated:** 2025-11-02

---

## Executive Summary

This runbook provides operational procedures for maintaining and monitoring code coverage in the xg2g project. All baseline configurations are complete and production-ready.

**Current Status:**
- ✅ Codecov integration active (commit 0e837c1)
- ✅ Coverage targets: Project 55%, Patch 90%
- ✅ 6 component-specific gates configured
- ✅ Atomic coverage mode in all workflows

---

## 1. GitHub Branch Protection Setup

### Required Status Checks

Navigate to: `Settings → Branches → Branch protection rules → main`

**Enable these Codecov checks:**

```yaml
Required status checks:
  - codecov/project          # Overall project coverage ≥55%
  - codecov/patch           # New code coverage ≥90%
  - codecov/component/daemon # Daemon module ≥60%
  - codecov/component/api    # API layer ≥70%
  - codecov/component/owi    # OWI client ≥65%
```

**Configuration:**
- ☑️ Require status checks to pass before merging
- ☑️ Require branches to be up to date before merging
- ☐ Do not require status checks for administrators (optional)

**Why this matters:**
- Prevents merging PRs that reduce coverage below thresholds
- Component-level checks ensure critical modules maintain high coverage
- Project-level check ensures overall code quality baseline

---

## 2. Coverage Monitoring KPIs

### Weekly Metrics

Check every Monday in Codecov Dashboard:

| Metric | Target | Alert Threshold | Action |
|--------|--------|-----------------|--------|
| Overall Project Coverage | ≥55% | <52% | Investigate coverage drops |
| Patch Coverage (rolling 5 PRs) | ≥90% | <85% | Review test requirements |
| Flag: unittests | ≥60% | <55% | Add unit tests |
| Flag: integration | ≥50% | <45% | Add integration tests |
| Flag: contract | ≥40% | <35% | Add contract tests |

### Monthly Review

First Monday of each month:

1. **Component Trends** (Codecov → Components tab)
   ```bash
   # Check each component vs target:
   - daemon: ≥60% (actual: check dashboard)
   - api: ≥70%
   - epg: ≥55%
   - playlist: ≥60%
   - proxy: ≥50%
   - owi: ≥65%
   ```

2. **Flag Distribution** (Codecov → Flags tab)
   ```bash
   # Ensure balanced test coverage:
   - unittests: Should be highest (60-70%)
   - integration: Medium (45-55%)
   - contract: Lowest (35-45%)
   ```

3. **Carryforward Status**
   - Verify at least one flag per component has fresh data
   - If >2 weeks stale: Run full test suite

### Quarterly Audit (Q1, Q2, Q3, Q4)

Export coverage metrics:

```bash
# Install Codecov CLI (if not already)
pip install codecov-cli

# Export metrics
codecov cli report --flags integration --format json > coverage_q1_2025.json

# Review trends
jq '.results[] | {file: .file_name, coverage: .coverage}' coverage_q1_2025.json | head -20
```

**Update CI_CD_AUDIT_REPORT.md:**
- Add quarterly coverage summary
- Document any threshold adjustments
- Note new components added

---

## 3. Operational Procedures

### After 5 PRs: Threshold Validation

**Goal:** Verify 90% patch target is achievable without blocking legitimate PRs.

**Procedure:**
1. Navigate to Codecov → Pull Requests
2. Review last 5 merged PRs
3. Check patch coverage for each:
   ```
   PR #123: 92% ✅
   PR #124: 88% ⚠️
   PR #125: 95% ✅
   PR #126: 87% ⚠️
   PR #127: 91% ✅
   ```

4. **Decision matrix:**
   - If ≥4/5 PRs meet 90%: Keep threshold
   - If 3/5 PRs meet 90%: Monitor for 5 more PRs
   - If ≤2/5 PRs meet 90%: Consider reducing to 85%

**Threshold adjustment:**
```yaml
# Edit codecov.yml
coverage:
  status:
    patch:
      default:
        target: 85%  # Reduced from 90%
        threshold: 5%
```

Commit with message:
```bash
git commit -m "chore(coverage): adjust patch target to 85% based on 5-PR review"
```

### Handling Coverage Drops

**Scenario:** PR blocked by codecov/project check (coverage dropped >0.5%)

**Troubleshooting:**

1. **Check if legitimate:**
   ```bash
   # View coverage diff in PR
   gh pr view <PR_NUMBER> --web
   # Navigate to Codecov comment
   ```

2. **Common causes:**
   - Refactoring removed dead code (OK)
   - Added new code without tests (NOT OK)
   - Removed tests during cleanup (NOT OK)
   - Changed test tags (check -tags=nogpu)

3. **Resolution paths:**

   **If drop is acceptable** (e.g., removed dead code):
   ```bash
   # Adjust threshold temporarily
   # Add comment to codecov.yml:
   # Threshold increase due to cleanup PR #XYZ
   coverage:
     status:
       project:
         default:
           threshold: 1.0%  # Temporarily increased
   ```

   **If tests are missing:**
   ```bash
   # Add tests to restore coverage
   # Ensure new code has test coverage
   go test -coverprofile=coverage.out ./...
   go tool cover -func=coverage.out | grep <your_file>
   ```

### Slack Alerting (Optional)

**Activation:**

1. Create Slack webhook:
   ```
   https://api.slack.com/messaging/webhooks
   ```

2. Add to GitHub Secrets:
   ```bash
   gh secret set SLACK_WEBHOOK --body "https://hooks.slack.com/services/..."
   ```

3. Uncomment in codecov.yml:
   ```yaml
   slack:
     url: "secret:SLACK_WEBHOOK"
     threshold: 1%
     only_pulls: false
     message: "Coverage changed for {{owner}}/{{repo}}"
     branches:
       - main
   ```

**Alert format:**
```
📊 Coverage Alert: xg2g
Branch: main
Coverage: 56.2% → 54.8% (-1.4%)
View: https://app.codecov.io/gh/ManuGH/xg2g
```

---

## 4. Advanced: Rust Transcoder Coverage

**Status:** Not implemented (transcoder excluded in codecov.yml:195)

**Future implementation:**

```bash
# Install llvm-cov
rustup component add llvm-tools-preview
cargo install cargo-llvm-cov

# Generate Rust coverage
cd transcoder
cargo llvm-cov --lcov --output-path lcov.info

# Upload with separate flag
cd ..
codecov upload-file --file transcoder/lcov.info --flags rust
```

**Configuration:**
```yaml
# Add to codecov.yml
flags:
  rust:
    paths:
      - "transcoder/"
    carryforward: true
    carryforward_mode: labels

component_management:
  individual_components:
    - component_id: rust-transcoder
      name: "Rust Transcoder"
      paths:
        - "transcoder/**"
      statuses:
        - type: project
          target: 70%
        - type: patch
          target: 85%
```

**Why separate:**
- Go and Rust have different testing patterns
- FFmpeg FFI code is harder to test (50-70% realistic)
- Avoids skewing overall Go coverage metrics

---

## 5. Troubleshooting

### Issue: Codecov upload fails

**Symptoms:**
```
Error: Could not upload coverage report
```

**Solutions:**

1. Check CODECOV_TOKEN secret:
   ```bash
   gh secret list | grep CODECOV_TOKEN
   ```

2. Verify coverage.out exists:
   ```bash
   # In workflow, add debug step:
   - name: Debug coverage file
     run: |
       ls -lh coverage.out
       head -5 coverage.out
   ```

3. Check Codecov API status:
   ```bash
   curl -I https://codecov.io/api
   # Should return 200 OK
   ```

4. Validate codecov.yml:
   ```bash
   curl -X POST --data-binary @codecov.yml https://codecov.io/validate
   ```

### Issue: Component coverage not appearing

**Symptoms:**
- Component checks missing in PR
- Codecov dashboard shows no components

**Solutions:**

1. Verify paths match actual code structure:
   ```bash
   # Check if paths exist
   ls -d internal/daemon internal/api internal/epg
   ```

2. Ensure coverage.out includes component paths:
   ```bash
   grep "internal/daemon" coverage.out
   ```

3. Check component_management in codecov.yml:
   ```yaml
   component_management:
     individual_components:
       - component_id: daemon
         paths:
           - "cmd/daemon/**"      # Must match repo structure
           - "internal/daemon/**"
   ```

### Issue: Carryforward not working

**Symptoms:**
- Coverage drops when only running subset of tests
- Flag shows as stale

**Solutions:**

1. Verify flag is set during upload:
   ```yaml
   # In .github/workflows/coverage.yml
   - uses: codecov/codecov-action@v5
     with:
       flags: unittests  # Must match codecov.yml
   ```

2. Check carryforward_mode:
   ```yaml
   flags:
     unittests:
       carryforward: true
       carryforward_mode: labels  # Not "all"
   ```

3. Ensure labels match:
   ```bash
   # Check label on uploaded coverage
   # In Codecov dashboard: Uploads → View labels
   ```

---

## 6. CI/CD Integration Checklist

**Pre-deployment:**
- [x] `-covermode=atomic` in all test workflows
- [x] Single coverage.out per job (no merge needed)
- [x] codecov.yml validated via API
- [x] GitHub branch protection rules documented
- [x] Slack webhook documented (optional)
- [x] Quarterly audit schedule defined

**Post-deployment (after 5 PRs):**
- [ ] Validate 90% patch target achievable
- [ ] Review component coverage trends
- [ ] Confirm GitHub status checks blocking merges
- [ ] Export first quarterly metrics (Q1 2025)

---

## 7. Contact and Escalation

**Coverage Issues:**
- Check: https://docs.codecov.com/docs/
- GitHub: https://github.com/codecov/feedback/discussions

**CI/CD Issues:**
- Check: docs/CI_CD_AUDIT_REPORT.md
- Review: .github/workflows/coverage.yml

**Emergency Coverage Bypass:**
```bash
# ONLY use in exceptional circumstances
# Requires admin override of branch protection
gh pr merge <PR_NUMBER> --admin --squash
```

**Document all bypasses in:**
```
docs/CI_CD_AUDIT_REPORT.md → Appendix A → Known Issues
```

---

## 8. Changelog

| Date | Version | Changes |
|------|---------|---------|
| 2025-11-02 | 1.0 | Initial runbook (commit 0e837c1) |

---

**Next Review:** After 5 PRs (estimated 2-4 weeks)
