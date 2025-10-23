# CI Load Testing Integration Guide

This guide explains how to integrate the load testing framework into CI/CD pipelines for continuous performance monitoring and regression detection.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [GitHub Actions Integration](#github-actions-integration)
- [Test Configuration](#test-configuration)
- [Performance Baselines](#performance-baselines)
- [Regression Detection](#regression-detection)
- [Reporting](#reporting)
- [Best Practices](#best-practices)

---

## Overview

The load testing framework (`test/load/`) provides:
- ✅ Realistic OpenWebIF mocks
- ✅ Configurable load scenarios
- ✅ Built-in metrics (latency, throughput, errors)
- ✅ Performance regression detection
- ✅ Automated reporting

**Goals:**
1. Detect performance regressions before merge
2. Track performance trends over time
3. Validate scalability improvements
4. Ensure SLA compliance

---

## Quick Start

### Local Testing

Run load tests locally:

```bash
# Run all load tests
go test -tags=integration_slow -v ./test/load/...

# Run specific test
go test -tags=integration_slow -v ./test/load/... -run=TestLoadBaseline

# Run with custom timeout
go test -tags=integration_slow -v -timeout=10m ./test/load/...

# Generate detailed output
go test -tags=integration_slow -v -json ./test/load/... | tee load-results.json
```

### Benchmarks

Run performance benchmarks:

```bash
# Run all benchmarks
go test -bench=. -benchmem -benchtime=5s ./test/load/...

# Save results for comparison
go test -bench=. -benchmem -benchtime=5s ./test/load/... > baseline.txt

# Compare with baseline
go test -bench=. -benchmem -benchtime=5s ./test/load/... > current.txt
benchstat baseline.txt current.txt
```

---

## GitHub Actions Integration

### Option 1: Nightly Load Tests (Recommended)

Create `.github/workflows/load-tests.yml`:

```yaml
name: Load Tests

on:
  # Run nightly
  schedule:
    - cron: '0 2 * * *'  # 2 AM UTC daily

  # Allow manual trigger
  workflow_dispatch:
    inputs:
      scenario:
        description: 'Load scenario to run'
        required: false
        default: 'all'
        type: choice
        options:
          - all
          - baseline
          - concurrent
          - high-load
          - unstable

jobs:
  load-tests:
    name: Run Load Tests
    runs-on: ubuntu-latest
    timeout-minutes: 30

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - name: Run load tests
        id: load_tests
        run: |
          # Run tests with JSON output
          go test -tags=integration_slow -v -json -timeout=20m ./test/load/... \
            | tee load-results.json

          # Extract metrics
          go run ./scripts/parse-load-results.go load-results.json > metrics.json

      - name: Upload results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: load-test-results-${{ github.run_number }}
          path: |
            load-results.json
            metrics.json
          retention-days: 30

      - name: Check for regressions
        run: |
          go run ./scripts/check-regression.go metrics.json baseline.json

      - name: Comment on commit (if regression)
        if: failure()
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const metrics = JSON.parse(fs.readFileSync('metrics.json'));

            github.rest.repos.createCommitComment({
              owner: context.repo.owner,
              repo: context.repo.repo,
              commit_sha: context.sha,
              body: `⚠️ **Performance Regression Detected**\n\n${metrics.summary}`
            });
```

### Option 2: PR-Based Load Tests (Optional)

For critical performance changes, run on PRs with label:

```yaml
name: PR Load Tests

on:
  pull_request:
    types: [labeled, synchronize]

jobs:
  load-tests:
    name: Load Tests
    if: contains(github.event.pull_request.labels.*.name, 'performance')
    runs-on: ubuntu-latest

    steps:
      - name: Checkout PR
        uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Run baseline tests
        run: |
          go test -tags=integration_slow -v ./test/load/... \
            -run=TestLoadBaseline > pr-baseline.txt

      - name: Checkout main
        uses: actions/checkout@v4
        with:
          ref: main

      - name: Run main baseline
        run: |
          go test -tags=integration_slow -v ./test/load/... \
            -run=TestLoadBaseline > main-baseline.txt

      - name: Compare results
        run: |
          echo "## Load Test Comparison" >> $GITHUB_STEP_SUMMARY
          diff main-baseline.txt pr-baseline.txt || true

      - name: Comment on PR
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const comparison = fs.readFileSync('comparison.txt', 'utf8');

            github.rest.issues.createComment({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: context.issue.number,
              body: `## 📊 Load Test Results\n\n${comparison}`
            });
```

### Option 3: Continuous Benchmarking

Track performance over time:

```yaml
name: Continuous Benchmarks

on:
  push:
    branches: [main]

jobs:
  benchmark:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Run benchmarks
        run: |
          go test -bench=. -benchmem -benchtime=5s ./test/load/... \
            | tee benchmark.txt

      - name: Store benchmark result
        uses: benchmark-action/github-action-benchmark@v1
        with:
          tool: 'go'
          output-file-path: benchmark.txt
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-push: true
```

---

## Test Configuration

### Environment Variables

Configure load tests via environment:

```bash
# Mock server config
export LOAD_TEST_MOCK_LATENCY_MIN=10ms
export LOAD_TEST_MOCK_LATENCY_MAX=50ms
export LOAD_TEST_MOCK_ERROR_RATE=0.05
export LOAD_TEST_MOCK_TIMEOUT_RATE=0.01

# Test parameters
export LOAD_TEST_ITERATIONS=20
export LOAD_TEST_CONCURRENCY=10
export LOAD_TEST_TIMEOUT=5m

# Baseline thresholds
export LOAD_TEST_MAX_DURATION=5s
export LOAD_TEST_MIN_SUCCESS_RATE=0.95
export LOAD_TEST_MAX_LATENCY=100ms
```

### CI-Specific Configuration

For CI environments, use relaxed thresholds:

```yaml
env:
  # CI runners may be slower
  LOAD_TEST_MAX_DURATION: 10s
  LOAD_TEST_TIMEOUT: 10m

  # More tolerant of variance
  LOAD_TEST_SUCCESS_RATE_THRESHOLD: 0.90
```

---

## Performance Baselines

### Establishing Baselines

1. **Run baseline tests** on clean main branch:
```bash
git checkout main
go test -tags=integration_slow -v ./test/load/... -run=TestLoadBaseline \
  | tee baseline-$(git rev-parse --short HEAD).txt
```

2. **Store baseline** in repository or artifact storage:
```bash
# Option 1: Commit to repo
git add baselines/baseline-$(date +%Y%m%d).txt
git commit -m "chore: Add performance baseline"

# Option 2: Upload to GitHub Releases
gh release create baseline-v1.0.0 baseline.txt

# Option 3: Store in CI cache
# (shown in workflow examples above)
```

3. **Document baseline conditions**:
```yaml
# baselines/metadata.yaml
baseline_v1:
  date: 2025-01-15
  commit: abc123
  go_version: 1.23
  os: ubuntu-22.04
  hardware: github-actions-standard
  metrics:
    avg_refresh_duration: 75.19ms
    success_rate: 100%
    avg_latency: 33.93µs
```

### Baseline Variants

Maintain baselines for different scenarios:

```
baselines/
├── baseline-small.txt      # 1 bouquet × 10 channels
├── baseline-medium.txt     # 3 bouquets × 50 channels
├── baseline-large.txt      # 5 bouquets × 200 channels
├── baseline-concurrent.txt # 10 concurrent clients
└── baseline-unstable.txt   # 20% error rate
```

---

## Regression Detection

### Automated Regression Check

Create `scripts/check-regression.go`:

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
)

type Metrics struct {
    AvgDuration   float64 `json:"avg_duration_ms"`
    SuccessRate   float64 `json:"success_rate"`
    AvgLatency    float64 `json:"avg_latency_us"`
}

func main() {
    current := loadMetrics(os.Args[1])
    baseline := loadMetrics(os.Args[2])

    // Duration regression: > 20% slower
    durationChange := (current.AvgDuration - baseline.AvgDuration) / baseline.AvgDuration
    if durationChange > 0.20 {
        fmt.Printf("⚠️ Duration regression: %.1f%% slower\n", durationChange*100)
        os.Exit(1)
    }

    // Success rate regression: < 5% drop
    if current.SuccessRate < baseline.SuccessRate - 0.05 {
        fmt.Printf("⚠️ Success rate regression: %.1f%% drop\n",
            (baseline.SuccessRate - current.SuccessRate)*100)
        os.Exit(1)
    }

    // Latency regression: > 50% increase
    latencyChange := (current.AvgLatency - baseline.AvgLatency) / baseline.AvgLatency
    if latencyChange > 0.50 {
        fmt.Printf("⚠️ Latency regression: %.1f%% increase\n", latencyChange*100)
        os.Exit(1)
    }

    fmt.Println("✅ No performance regression detected")
}
```

### Regression Thresholds

Recommended thresholds:

| Metric | Threshold | Severity |
|--------|-----------|----------|
| Duration | +20% | ⚠️ Warning |
| Duration | +50% | 🚨 Critical |
| Success Rate | -5% | ⚠️ Warning |
| Success Rate | -10% | 🚨 Critical |
| Latency | +50% | ⚠️ Warning |
| Latency | +100% | 🚨 Critical |

---

## Reporting

### Test Summary Report

Generate markdown summary:

```bash
# Parse test results
go run scripts/parse-load-results.go load-results.json > summary.md

# Append to GitHub Step Summary
cat summary.md >> $GITHUB_STEP_SUMMARY
```

**Example summary:**

```markdown
## 📊 Load Test Results

### TestLoadBaseline
- **Duration:** 75.19ms (baseline: 75.00ms) ✅
- **Success Rate:** 100% (baseline: 100%) ✅
- **Total Requests:** 22
- **Avg Latency:** 33.93µs ✅

### TestLoadConcurrentRefreshes
- **Clients:** 5
- **Duration:** 12.5s ✅
- **Throughput:** 1.2 ops/sec
- **Success:** 15/15 ✅

### Performance Comparison
| Test | Current | Baseline | Change |
|------|---------|----------|--------|
| Baseline | 75.19ms | 75.00ms | +0.25% ✅ |
| Concurrent | 12.5s | 12.0s | +4.17% ✅ |
| HighLoad | 45.2s | 44.0s | +2.73% ✅ |
```

### Grafana Dashboard

For long-term tracking, send metrics to Prometheus/Grafana:

```yaml
- name: Export metrics to Prometheus
  run: |
    cat << EOF > metrics.prom
    xg2g_load_test_duration_ms{test="baseline"} $(jq -r '.baseline.duration' metrics.json)
    xg2g_load_test_success_rate{test="baseline"} $(jq -r '.baseline.success_rate' metrics.json)
    xg2g_load_test_latency_us{test="baseline"} $(jq -r '.baseline.latency' metrics.json)
    EOF

    curl -X POST https://pushgateway.example.com/metrics/job/load_tests \
      --data-binary @metrics.prom
```

---

## Best Practices

### Test Selection

**Nightly:**
- ✅ Run full load test suite
- ✅ Include stress tests
- ✅ Test all scenarios

**PR (with label):**
- ✅ Run baseline tests only
- ✅ Quick smoke tests
- ✅ Critical path validation

**Main Push:**
- ✅ Run fast load tests
- ✅ Regression check against baseline

### Resource Management

**CI Runner Sizing:**
```yaml
runs-on: ubuntu-latest  # Standard (2 CPU, 7GB RAM)
# OR
runs-on: ubuntu-latest-8-cores  # Larger (8 CPU, 32GB RAM)
```

**Timeout Configuration:**
```yaml
timeout-minutes: 30  # Prevent hung tests
```

**Artifact Retention:**
```yaml
retention-days: 30  # Keep 1 month of history
```

### Flake Prevention

**Retry flaky tests:**
```yaml
- name: Run load tests with retry
  uses: nick-fields/retry@v2
  with:
    timeout_minutes: 20
    max_attempts: 3
    command: go test -tags=integration_slow -v ./test/load/...
```

**Use stable baselines:**
- Median of 5 runs instead of single run
- Exclude outliers (top/bottom 10%)
- Weekly baseline updates instead of daily

### Performance Optimization

**Parallel test execution:**
```bash
# Run tests in parallel
go test -tags=integration_slow -v -parallel=4 ./test/load/...
```

**Test result caching:**
```yaml
- uses: actions/cache@v4
  with:
    path: ~/.cache/go-test
    key: load-tests-${{ runner.os }}-${{ hashFiles('test/load/**') }}
```

---

## Troubleshooting

### Tests Timeout in CI

**Problem:** Tests timeout after 20 minutes

**Solutions:**
```yaml
# Increase timeout
timeout-minutes: 45

# Reduce test scope
go test -tags=integration_slow -short ./test/load/...

# Skip slow tests in CI
go test -tags=integration_slow -run='^TestLoad(Baseline|Concurrent)$' ./test/load/...
```

### High Variance in Results

**Problem:** Results vary significantly between runs

**Solutions:**
- Use larger sample sizes (more iterations)
- Run on dedicated runners (not shared)
- Disable CPU frequency scaling
- Run at off-peak times

### False Positive Regressions

**Problem:** Regression alerts on valid changes

**Solutions:**
- Adjust thresholds (20% → 30%)
- Use statistical significance tests
- Require multiple consecutive failures
- Manual approval for expected changes

---

## Example: Complete CI Setup

Minimal setup for continuous load testing:

```yaml
# .github/workflows/load-tests.yml
name: Load Tests

on:
  schedule:
    - cron: '0 2 * * *'
  workflow_dispatch:

jobs:
  load:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'

      - name: Run tests
        run: go test -tags=integration_slow -v -json ./test/load/... | tee results.json

      - name: Check regression
        run: |
          go run scripts/check-regression.go results.json baseline.json || \
            echo "::warning::Performance regression detected"

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: load-results
          path: results.json
```

---

## References

- Load test framework: `test/load/`
- Baseline storage: `baselines/`
- Regression scripts: `scripts/`
- Existing benchmarks: `.github/workflows/benchmark.yml`

---

**Last Updated:** 2025-10-23
**Maintainer:** DevOps Team
**Related:** [TESTING_STRATEGY.md](./TESTING_STRATEGY.md), [COVERAGE_SETUP.md](./COVERAGE_SETUP.md)
