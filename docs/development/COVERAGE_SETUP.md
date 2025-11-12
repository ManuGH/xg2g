# Coverage & Test Reporting Setup

## Overview

This project uses automated coverage reporting and test result visualization integrated directly into GitHub.

## Features

### 📊 Coverage Reports
- **Automatic HTML reports** generated on every push
- **Coverage badges** in README (updated on main branch)
- **GitHub Pages deployment** with detailed coverage visualization
- **PR comments** with coverage diff
- **Codecov integration** (optional)

### 🧪 Test Reports
- **Detailed test summaries** in PR comments
- **Test annotations** on failures
- **Benchmark results** tracking
- **Slow test detection** (top 10 slowest tests)
- **Pass rate tracking** over time

### 🚦 Quality Gates
- **Minimum coverage threshold** (50%)
- **Fail PR** if coverage drops below threshold
- **Test failure annotations** in PR files view

## Setup Instructions

### 1. Enable GitHub Pages

1. Go to **Settings** → **Pages**
2. Set **Source** to "GitHub Actions"
3. Save

Your coverage reports will be available at:
```
https://YOUR_USERNAME.github.io/xg2g/
```

### 2. Create Coverage Badge (Optional)

#### Option A: Using Gist (Recommended)

1. Create a personal access token:
   - Go to **Settings** → **Developer settings** → **Personal access tokens**
   - Generate token with `gist` scope
   - Save as repository secret `GIST_SECRET`

2. Create a new Gist:
   - Go to https://gist.github.com/
   - Create file `xg2g-coverage.json` with content: `{}`
   - Note the Gist ID (last part of URL)
   - Save as repository secret `GIST_ID`

3. Add badge to README:
   ```markdown
   ![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/YOUR_USERNAME/GIST_ID/raw/xg2g-coverage.json)
   ```

#### Option B: Using shields.io (Simpler)

Add to README:
```markdown
![Coverage](https://img.shields.io/badge/coverage-56%25-yellow)
```

Update manually or with script after coverage runs.

### 3. Configure Codecov (Optional)

1. Sign up at https://codecov.io
2. Add your repository
3. Copy upload token
4. Add as repository secret `CODECOV_TOKEN`

Codecov provides:
- Historical coverage graphs
- Coverage diff in PRs
- Sunburst visualization
- Team dashboards

### 4. Test the Setup

Run locally to verify:

```bash
# Generate coverage
go test -covermode=atomic -coverprofile=coverage.out ./...

# View coverage percentage
go tool cover -func=coverage.out | grep total

# Generate HTML report
go tool cover -html=coverage.out -o coverage.html

# Open in browser
open coverage.html
```

## Workflows

### Coverage Workflow (`.github/workflows/coverage.yml`)

**Triggers:**
- Push to `main`
- Pull request to `main`
- Manual dispatch

**Jobs:**

1. **coverage** - Generate reports
   - Run tests with coverage
   - Calculate coverage percentage
   - Generate HTML report
   - Comment on PR (if applicable)
   - Update badge (main branch only)
   - Upload to Codecov (main branch only)
   - Archive artifacts

2. **deploy-pages** - Deploy to GitHub Pages
   - Upload HTML report
   - Make available at github.io URL

3. **coverage-guard** - Quality gate
   - Check coverage threshold (50%)
   - Fail PR if below threshold

### Test Report Workflow (`.github/workflows/test-report.yml`)

**Triggers:**
- Push to `main`
- Pull request to `main`
- Manual dispatch

**Jobs:**

1. **test-report** - Generate test summaries
   - Run tests with JSON output
   - Parse results (pass/fail/skip counts)
   - Calculate pass rate
   - Identify slowest tests
   - Create annotations for failures
   - Comment summary on PR

2. **benchmark-report** - Track performance
   - Run benchmarks on main branch
   - Archive results for comparison

## Coverage Metrics

### Current Status

| Metric | Target | Current |
|--------|--------|---------|
| Overall Coverage | ≥ 50% | 56% |
| API Package | ≥ 60% | 73% |
| Jobs Package | ≥ 70% | 78% |
| Proxy Package | ≥ 30% | 22% |

### Quality Gates

**PR Blocking:**
- ❌ Overall coverage < 50%
- ❌ Any test failures
- ✅ All smoke tests pass

**Non-blocking (warnings):**
- ⚠️ Coverage decrease > 5%
- ⚠️ Slow tests > 10s
- ⚠️ Flaky tests detected

## Viewing Reports

### In Pull Requests

Every PR automatically gets:

1. **Coverage comment** (updated on each push)
   ```
   📊 Code Coverage Report
   Overall Coverage: 56.2%

   Coverage by Package:
   - internal/api: 73.4%
   - internal/jobs: 78.1%
   - internal/proxy: 21.8%
   ```

2. **Test results comment**
   ```
   🧪 Test Results Summary
   Total Tests: 127
   ✅ Passed: 127
   ❌ Failed: 0
   Pass Rate: 100%
   ```

3. **Annotations** on failed tests
   - Show exact line where test failed
   - Include failure message
   - Link to full test output

### On GitHub Pages

Visit your coverage report:
```
https://YOUR_USERNAME.github.io/xg2g/
```

Features:
- **Interactive HTML report** - click packages/files to drill down
- **Line-by-line coverage** - see which lines are covered
- **Color coding** - green (covered), red (not covered), grey (not executable)

### In GitHub Actions

1. Go to **Actions** tab
2. Click on any workflow run
3. View **Summary** for:
   - Coverage percentage
   - Top packages by coverage
   - Test pass rate
   - Slowest tests

## Troubleshooting

### Coverage badge not updating

1. Check `GIST_SECRET` is set correctly
2. Verify Gist ID matches
3. Check workflow logs for badge update step
4. Ensure token has `gist` scope

### GitHub Pages not deploying

1. Check Pages is enabled in settings
2. Verify source is "GitHub Actions"
3. Check workflow permissions include `pages: write`
4. Look for deployment in Actions tab

### PR comments not appearing

1. Check workflow has `pull-requests: write` permission
2. Verify GitHub token is valid
3. Check if bot comment was deleted (creates new one)

### Coverage appears incorrect

1. Run `go test -covermode=atomic` (not count)
2. Use `-coverpkg=./...` to include all packages
3. Check test files are not counted (use `-coverpkg`)
4. Verify no test caching (add `-count=1`)

## Best Practices

### DO ✅

- **Review coverage PRs** - understand what code is untested
- **Focus on critical paths** - 100% coverage not always needed
- **Test behavior, not lines** - coverage is a metric, not a goal
- **Keep threshold realistic** - 50-70% is often sufficient
- **Use coverage to find gaps** - not as absolute quality measure

### DON'T ❌

- Don't chase 100% coverage blindly
- Don't write tests just for coverage
- Don't test getters/setters obsessively
- Don't ignore low coverage in critical code
- Don't let coverage become a vanity metric

## Local Development

### Generate coverage locally

```bash
# Quick coverage check
make coverage

# Or manually:
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Check coverage threshold

```bash
# Check if meets 50% threshold
COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
if (( $(echo "$COVERAGE < 50" | bc -l) )); then
  echo "Coverage below threshold: $COVERAGE%"
  exit 1
fi
```

### Exclude from coverage

Add to test files:
```go
//go:build integration
// +build integration
```

Or use `-coverpkg` flag:
```bash
go test -coverpkg=./internal/... ./...
```

## Maintenance

### Update coverage threshold

Edit `.github/workflows/coverage.yml`:
```yaml
THRESHOLD=50  # Change to desired percentage
```

### Change badge colors

Edit coverage workflow step:
```yaml
# Green: >= 70%
# Yellow: >= 50%
# Red: < 50%
```

### Disable Codecov

Remove or comment out the Codecov step in coverage workflow.

## References

- [Go Coverage Tool](https://go.dev/blog/cover)
- [GitHub Actions for Go](https://docs.github.com/en/actions/automating-builds-and-tests/building-and-testing-go)
- [Codecov Documentation](https://docs.codecov.com/docs)
- [Coverage Best Practices](https://martinfowler.com/bliki/TestCoverage.html)
