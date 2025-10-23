# Testing Strategy

## Overview

This project uses a **risk-based, multi-tier testing strategy** that balances fast feedback loops with comprehensive validation.

## Test Tiers

### 🚀 Tier 1: Smoke Tests (Critical Path)
**Execution**: Every PR, < 2 minutes
**Tag**: `integration_fast`
**Risk Level**: HIGH

Fast smoke tests covering critical production paths that MUST work:
- Health/readiness endpoints (< 100ms)
- API authentication (< 50ms)
- Basic refresh flow (< 500ms)
- Concurrent request safety (< 1s)

**Why**: Catch critical regressions immediately. If these fail, system is broken.

```bash
# Run locally
go test -tags=integration_fast -timeout=3m ./test/integration/... -run="^TestSmoke"
```

### 📦 Tier 2: Full Integration Tests
**Execution**: Push to main, nightly, manual
**Tag**: `integration`
**Risk Level**: MEDIUM-HIGH

Complete end-to-end integration tests:
- Full refresh flows with M3U/XMLTV generation
- Error handling (500, 503, timeouts)
- Circuit breaker activation
- Retry logic with backoff
- Graceful degradation
- File serving
- Concurrent load (5-20 requests)

**Why**: Verify complete system behavior under various scenarios.

```bash
# Run locally
go test -tags=integration -timeout=5m ./test/integration/...
```

### 🐌 Tier 3: Slow Tests (Deep Validation)
**Execution**: Nightly only, 5-10 minutes
**Tag**: `integration_slow`
**Risk Level**: LOW-MEDIUM

Deep validation with long-running scenarios:
- Timeout handling (5s+ delays)
- Context cancellation (10s)
- Recovery after prolonged failures
- Extended retry sequences

**Why**: Catch edge cases and timing-dependent bugs that only appear under stress.

```bash
# Run locally (skip with -short flag)
go test -tags=integration_slow -timeout=10m ./test/integration/... -run="^TestSlow"

# Skip slow tests
go test -short -tags=integration ./test/integration/...
```

## CI/CD Integration

### Pull Request Flow
```
PR Created
  ↓
Tier 1: Smoke Tests (< 2min)
  ├─ PASS → Approve PR ✅
  └─ FAIL → Block PR ❌
```

### Main Branch Flow
```
Merge to Main
  ↓
Tier 1: Smoke Tests (< 2min)
  ↓
Tier 2: Full Integration (< 5min)
  ├─ PASS → Deploy to staging ✅
  └─ FAIL → Rollback & Alert 🚨
```

### Nightly Flow
```
02:00 UTC Daily
  ↓
Tier 1: Smoke Tests
  ↓
Tier 2: Full Integration
  ↓
Tier 3: Slow Tests
  ↓
Compatibility Matrix (Go 1.23, 1.24, 1.25)
  ├─ PASS → Email summary ✅
  └─ FAIL → Create GitHub Issue 🐛
```

## Risk-Based Test Classification

### HIGH Risk (Tier 1 - Always Run)
- Authentication bypass
- Health check failures
- Data corruption
- Concurrent access violations
- Core business logic (refresh, file generation)

### MEDIUM Risk (Tier 2 - Main + Nightly)
- Error handling edge cases
- Circuit breaker behavior
- Retry logic
- File serving
- Graceful degradation

### LOW Risk (Tier 3 - Nightly Only)
- Timeout handling
- Long-running cancellation
- Recovery timing
- Extended retry sequences

## Test Tags Reference

| Tag | Description | When to Run | Typical Duration |
|-----|-------------|-------------|------------------|
| `integration_fast` | Critical smoke tests | Every PR | < 2 min |
| `integration` | Full integration suite | Main push, nightly | < 5 min |
| `integration_slow` | Deep validation | Nightly only | 5-10 min |

## Local Development

### Quick Feedback Loop (Recommended)
```bash
# Run only smoke tests - fastest feedback
go test -tags=integration_fast -v ./test/integration/...

# Watch mode (requires entr)
ls test/integration/*.go | entr -c go test -tags=integration_fast ./test/integration/...
```

### Pre-Commit Validation
```bash
# Run full suite before committing
go test -tags=integration -timeout=5m ./test/integration/...
```

### Complete Validation (Before PR)
```bash
# Run everything including slow tests
go test -tags=integration -timeout=10m ./test/integration/...
```

## Adding New Tests

### Decision Tree

```
New test to add?
  │
  ├─ Is it critical path? (auth, health, core business logic)
  │  └─ YES → Add to smoke_test.go with tag integration_fast
  │
  ├─ Does it take > 5 seconds?
  │  └─ YES → Add to slow_test.go with tag integration_slow
  │
  └─ Otherwise → Add to full_flow_test.go or resilience_test.go
                 with tag integration
```

### Test Template

```go
// SPDX-License-Identifier: MIT

//go:build integration_fast
// +build integration_fast

package test

// TestSmoke_YourFeature tests critical path (< 100ms)
// Tag: critical, fast
// Risk Level: HIGH - explain why this is critical
func TestSmoke_YourFeature(t *testing.T) {
    // Test implementation
}
```

## Metrics & Monitoring

### Success Criteria
- Smoke tests: **100% pass rate** (blocking)
- Full integration: **95%+ pass rate** (non-blocking, investigate failures)
- Slow tests: **90%+ pass rate** (informational, may have timing flakes)

### Performance Targets
- Smoke tests: < 2 minutes total
- Full integration: < 5 minutes total
- Slow tests: < 10 minutes total

### Flaky Test Policy
If a test fails intermittently (> 2 times in 7 days):
1. Add retry logic or timing tolerance
2. Move to `integration_slow` tier
3. Or remove if not providing value

## Best Practices

### DO ✅
- Test **observable behavior**, not implementation
- Use flexible assertions (`assert.GreaterOrEqual` vs exact counts)
- Validate status codes and behavior, not exact error messages
- Keep smoke tests < 500ms each
- Use descriptive test names with risk levels in comments
- Tag tests correctly for CI optimization

### DON'T ❌
- Don't test exact timing (use ranges: `< 2s` not `== 1.5s`)
- Don't assert on exact error text (check error type or code)
- Don't add slow tests to `integration_fast` tier
- Don't skip flaky tests without investigating root cause
- Don't hardcode port numbers (use httptest.NewServer)

## References

- [Integration Testing Handbook 2025](https://martinfowler.com/articles/practical-test-pyramid.html)
- [Go Testing Best Practices](https://go.dev/doc/tutorial/add-a-test)
- [Risk-Based Testing](https://en.wikipedia.org/wiki/Risk-based_testing)
- Martin Fowler: "Test Observable Behavior, Not Implementation"

## Maintenance

This testing strategy should be reviewed quarterly and updated based on:
- CI/CD pipeline metrics (duration, flakiness)
- Production incident correlation
- Developer feedback
- New feature complexity
