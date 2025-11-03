# Coverage Operations Runbook

**Project:** xg2g
**Version:** 1.4
**Last Updated:** 2025-11-03

---

## Executive Summary

This runbook provides operational procedures for maintaining and monitoring code coverage in the xg2g project. All baseline configurations are complete and production-ready.

**Current Status:**
- ✅ Codecov integration active (commit 0e837c1)
- ✅ CODECOV_TOKEN configured (2025-11-03)
- ✅ Coverage targets: Project 55%, Patch 90%
- ✅ 6 component-specific gates configured
- ✅ Atomic coverage mode in all workflows

---

## 0. Prerequisites and Setup

### Codecov Token Configuration

**Status:** ✅ **Configured** (2025-11-03)

The repository upload token is configured in GitHub Secrets for enhanced security and reliability.

**Token Purpose:**
- Authenticates coverage uploads to Codecov
- Required for private repositories
- Provides better control over fork PRs
- Avoids rate limits on public repos

**Current Configuration:**
```bash
# Token is stored in GitHub Secrets
gh secret list | grep CODECOV_TOKEN
# Output: CODECOV_TOKEN	2025-11-03T03:06:31Z
```

**Token Management:**

1. **View Token** (Codecov Dashboard):
   ```
   https://app.codecov.io/github/manugh/xg2g
   → Settings → General → Repository Upload Token
   ```

2. **Regenerate Token** (if compromised):
   ```bash
   # 1. Regenerate in Codecov Dashboard (click "Regenerate")
   # 2. Update GitHub Secret:
   gh secret set CODECOV_TOKEN --body "NEW_TOKEN_HERE"
   ```

3. **Secret Rotation Schedule** (Security Best Practice):
   ```bash
   # Biannual token rotation (every 6 months)
   # Schedule: January 1st and July 1st

   # Step 1: Regenerate in Codecov Dashboard
   # https://app.codecov.io/github/manugh/xg2g → Settings → General → Regenerate

   # Step 2: Update GitHub Secret
   gh secret set CODECOV_TOKEN --body "NEW_TOKEN_FROM_DASHBOARD"

   # Step 3: Verify next workflow run
   gh run list --workflow=coverage.yml --limit 1
   ```

   **Rotation History:**
   - 2025-11-03: Initial configuration
   - Next rotation: 2026-01-01 (Q1 2026)

4. **Verify Token Usage**:
   ```bash
   # Check workflow logs for successful uploads
   gh run list --workflow=coverage.yml --limit 1
   gh run view <RUN_ID> --log | grep "Process Upload complete"
   ```

**Static Analysis Token (BETA):**
- **Status:** Not configured (not needed for coverage-only setup)
- **Use case:** Required only for Codecov Static Analysis features
- **Token:** Available in Codecov Dashboard → Settings → Static Analysis

**Tokenless Uploads:**

For public repositories, Codecov supports tokenless uploads via GitHub App. However, using CODECOV_TOKEN provides:
- More reliable uploads
- Better security audit trails
- Support for private repos (if visibility changes)
- Reduced dependency on GitHub App permissions

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

## 8. Test Analytics (Advanced)

**Status:** ✅ Active (test-results-action@v1 in test-report.yml)

### Overview

Codecov Test Analytics provides additional insights beyond code coverage:

| Feature | Description | Benefit |
|---------|-------------|---------|
| **Failed Test Reporting** | Lists failed tests in PR comments with stack traces | Faster debugging |
| **Flaky Test Detection** | Identifies tests that fail intermittently | Improve test reliability |
| **Test Performance Tracking** | Monitors test run times over time | Identify slow tests |
| **Test History** | View test results across commits | Trend analysis |

### Current Integration

**test-report.yml** already uploads JUnit XML to Codecov:

```yaml
- name: Upload test results to Codecov
  if: always()
  uses: codecov/test-results-action@v1
  with:
    token: ${{ secrets.CODECOV_TOKEN }}
    files: test-results.xml
    flags: unittests
    fail_ci_if_error: false
    verbose: true
```

**Data flow:**
1. `gotestsum` generates `test-results.xml` (JUnit format)
2. `codecov/test-results-action@v1` uploads to Codecov
3. Codecov processes test history and failures
4. Results appear in:
   - Codecov Dashboard → Tests tab
   - PR comments (if tests fail)
   - Flaky test reports

### Monitoring Test Analytics

**Weekly Review (Codecov Dashboard → Tests):**

1. **Failed Test Rate**
   - Target: <5% failure rate
   - Alert: >10% failure rate sustained over 3 days

2. **Flaky Test Detection**
   - Check "Flaky Tests" tab
   - Investigate tests with >20% flakiness
   - Add to flaky test suppression list or fix

3. **Slow Test Performance**
   - Review "Test Duration" metrics
   - Target: P95 test time <5s per test
   - Optimize or parallelize tests >10s

**Monthly Flaky Test Audit:**

```bash
# Export flaky tests from Codecov
# (Via UI: Tests → Flaky Tests → Export CSV)

# Review flaky test list
# For each flaky test:
# 1. Check flakiness percentage (>20% = investigate)
# 2. Review test implementation
# 3. Common causes:
#    - Race conditions (use -race flag)
#    - External dependencies (mock or skip in unit tests)
#    - Timing-sensitive assertions (use Eventually/Consistently)
#    - Resource leaks (check goroutine/file handle cleanup)
```

### Test Analytics Best Practices

**1. JUnit XML Quality:**

```yaml
# Ensure test names are descriptive
func TestAPIEndpoint_WithValidToken_ReturnsSuccess(t *testing.T) {
  // Test name appears in Codecov UI
}

# Not recommended:
func TestAPI1(t *testing.T) {
  // Unclear in analytics
}
```

**2. Test Isolation:**

```go
// Good: Each test is independent
func TestCache_Get(t *testing.T) {
  cache := NewCache()
  defer cache.Close()
  // Test logic
}

// Bad: Shared state causes flakiness
var globalCache = NewCache()
func TestCache_Get(t *testing.T) {
  // Flaky due to shared state
}
```

**3. Timeout Configuration:**

```go
// Use context with timeout for network tests
func TestHTTPClient_Timeout(t *testing.T) {
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()
  // Test with ctx
}
```

### Troubleshooting

**Issue: Tests not appearing in Codecov**

**Solutions:**

1. Verify JUnit XML is valid:
   ```bash
   # Check XML structure
   head -20 test-results.xml

   # Validate XML syntax
   xmllint --noout test-results.xml
   ```

2. Check upload logs:
   ```bash
   # In GitHub Actions log, look for:
   # "Uploading test results to Codecov"
   # "Successfully uploaded test results"
   ```

3. Verify token is set:
   ```bash
   gh secret list | grep CODECOV_TOKEN
   ```

**Issue: Flaky test detection not working**

**Requirements:**
- Test must run multiple times (at least 3 commits)
- Test must fail at least once
- Test name must be consistent across runs

**If flaky test not detected:**
1. Check test name consistency
2. Ensure test runs on main branch (not just PRs)
3. Wait for 5+ commits with test execution

### Integration with Coverage

Test Analytics uses the same `flags` as coverage:

```yaml
# coverage.yml
flags:
  unittests:
    paths: ["internal/"]
    carryforward: true

# test-report.yml
- uses: codecov/test-results-action@v1
  with:
    flags: unittests  # Same flag name
```

**Benefits:**
- Unified view of coverage + test health per flag
- Carryforward applies to both coverage and test results
- Component-level insights include test failures

### Future Enhancements

**1. Integration Tests:**

```yaml
# Add to integration-tests.yml (when created)
- uses: codecov/test-results-action@v1
  with:
    flags: integration
    files: integration-results.xml
```

**2. Contract Tests:**

```yaml
# Add to contract-tests.yml (when created)
- uses: codecov/test-results-action@v1
  with:
    flags: contract
    files: contract-results.xml
```

**3. Performance Benchmarks:**

```yaml
# Convert benchmark output to JUnit XML
# (Requires custom tooling or gotestsum enhancement)
```

---

## 9. Coverage Improvement Strategy (API & Proxy)

**Goal:** API ≥70% (short-term), Proxy 30-40% (mid-term), Overall 60-62% (5-8 PRs)

**Current Baseline (2025-11-03, updated post-PR-1.5):**
- Overall: 60.2% (target: 55% ✅, was 57.6%)
- API: 72.1% (target: 70% ✅, was 64.4%) **TARGET REACHED**
- Proxy: 43.0% (target: 50%, gap: -7%, was incorrectly listed as 16.0%)
- Config: 64.5% (reload.go at 0% drags down average)
- Jobs: 40.6% (fetch.go at 0% drags down average)
- Daemon: 60.7% (target: 60% ✅)
- EPG: 93.7% (target: 55% ✅)
- Playlist: 93.8% (target: 60% ✅)

**Philosophy:** No policy changes - improve coverage through targeted testing, not by lowering standards.

### 9.1 Quick Wins (1-2 PRs)

**API Component (+5-8% expected):**

1. **Circuit Breaker State Machine Tests**
   - Test state transitions: Closed → Open → Half-Open → Closed
   - Timer/Clock injection for deterministic tests
   - Test cases: threshold exceeded, timeout recovery, success resets
   - Implementation: Extract `type Clock interface { Now() time.Time }` for injection

2. **HTTP Handler Table-Driven Tests**
   - Success cases: 2xx responses with valid inputs
   - Error cases: 4xx (invalid params, malformed JSON, missing auth)
   - Edge cases: empty bodies, nil values, oversized payloads
   - Use `httptest.NewRecorder()` and `httptest.NewRequest()`

   ```go
   func TestHandler_Cases(t *testing.T) {
       tests := []struct {
           name       string
           method     string
           path       string
           body       string
           wantStatus int
       }{
           {"valid_request", "GET", "/api/v1/items?limit=10", "", 200},
           {"invalid_limit", "GET", "/api/v1/items?limit=0", "", 400},
           // 10-15 cases
       }
   }
   ```

**Proxy Component (+10-15% expected):**

1. **Dependency Inversion**
   - Extract interfaces: `Transcoder`, `Prober`
   - Production: FFmpeg implementation
   - Test: Fake implementations (no real FFmpeg binary)

2. **Fake FFmpeg for Tests**
   - Script-based stub: `test/fixtures/fake-ffmpeg.sh`
   - Environment variables control behavior (`FAKE_FFPEG_EXIT`)
   - Simulates success, failure, version check

### 9.2 Mid-Term Improvements (3-6 PRs)

**Proxy Deep Testing:**

1. **I/O Abstraction**
   - Inject `io.Reader`/`io.Writer` instead of direct file/network access
   - Tests use `bytes.Buffer`/`io.NopCloser`
   - Error paths: empty streams, interrupted reads, write failures

2. **FFmpeg Error Simulation**
   - Exit code ≠ 0
   - SIGPIPE handling
   - Incomplete header parsing
   - Stream format mismatches

3. **Contract Tests (Flag=contract)**
   - Mock OpenWebIF service (Docker container in CI)
   - Validate API contract stability
   - Only runs in CI, not locally blocking

**Chaos-Adjacent Unit Tests:**

- Inject latency via custom `http.RoundTripper`
- Timeout simulation with `context.WithTimeout`
- Upstream failures with fake servers returning 5xx

### 9.3 Implementation Scaffolding

**Note:** Interface extraction deferred to avoid naming conflicts with existing `Transcoder` struct. Tests can use fakes and dependency injection directly.

**1. Test Fakes** (`internal/proxy/fake/transcoder_fake_test.go`):
```go
//go:build test

package fake

type FakeTranscoder struct { Err error }
func (f *FakeTranscoder) Start(ctx, in, out) error { ... }
```

**3. Fake FFmpeg** (`test/fixtures/fake-ffmpeg.sh`):
```bash
#!/bin/sh
[ "$1" = "-version" ] && echo "ffmpeg version 6.0-test" >&2
exit "${FAKE_FFPEG_EXIT:-0}"
```

**4. Circuit Breaker Test Template** (`internal/api/circuit_breaker_test.go`):
```go
func TestCircuitBreaker_StateMachine(t *testing.T) {
    cases := []struct{
        name        string
        failures    int
        expectState string
    }{
        {"closed_to_open_on_threshold", 5, "open"},
        {"half_open_resets_on_success", 0, "closed"},
    }
    // Inject test clock for deterministic timing
}
```

### 9.4 KPI & Governance

**No Policy Changes:**
- Patch coverage gate remains **90%** (enforces quality on new code)
- Project minimum remains **55%** (defensive baseline)
- Component targets remain **informative only** (not blocking)

**Review Trigger (After 5 PRs):**
- If API component stable at <68%, consider lowering target 70% → 65%
- If Proxy reaches 40%, celebrate and document best practices
- If overall hits 62%, update baseline in this document

**Measurement:**
```bash
# Before starting work
go tool cover -func=coverage.out | grep "total:"
# After each PR
go tool cover -func=coverage.out | grep -E "internal/(api|proxy)"
```

**Success Criteria:**
- ✅ API: 70%+ (Quick Wins implemented)
- ✅ Proxy: 30-40% (Interfaces + Fakes deployed)
- ✅ Overall: 60-62% (5-8 PRs completed)
- ✅ Patch coverage: Maintained at 90%+
- ✅ Flaky rate: <5% (Test Analytics monitoring)

**Tracking:**
- Use GitHub Issues with label `coverage-improvement`
- Template: `.github/ISSUE_TEMPLATE/coverage_improvement.md`
- Link PRs to issues for audit trail

**Downstream Benefits:**
- Better testability (interfaces enable mocking)
- Faster CI (no real FFmpeg in unit tests)
- Reduced flakiness (deterministic clocks/timers)
- Easier onboarding (clear test patterns)

---

## 10. Changelog

| Date | Version | Changes |
|------|---------|---------|
| 2025-11-02 | 1.0 | Initial runbook (commit 0e837c1) |
| 2025-11-03 | 1.1 | Added Test Analytics section, test-results-action integration |
| 2025-11-03 | 1.2 | Added CODECOV_TOKEN configuration, prerequisites section |
| 2025-11-03 | 1.3 | Added biannual secret rotation schedule, security best practices |
| 2025-11-03 | 1.4 | Added Coverage Improvement Strategy (API & Proxy), baseline metrics, scaffolding guide |
| 2025-11-03 | 1.5 | **PR-1.5 completed**: API 58.8% → 72.1% ✅ (commits bfef578, d9bd284). Updated baselines from Codecov API: Overall 60.2%, Proxy corrected to 43.0% |

---

**Next Review:** After 5 PRs (estimated 2-4 weeks)
