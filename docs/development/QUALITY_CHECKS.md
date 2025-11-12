# Quality Checks - Race Detection, Leak Detection & Profiling

This document describes the automated quality checks that run in CI/CD to ensure code quality and detect issues early.

## Overview

The `quality-checks.yml` workflow runs automatically on:
- **Every push to main**
- **Every pull request**
- **Daily at 03:30 UTC** (scheduled)
- **Manual trigger** via workflow_dispatch

## Quality Checks

### 1. Race Detector 🏁

**Purpose:** Detect data races in concurrent code

**How it works:**
- Runs all tests with `-race` flag
- Tests concurrent request handling
- Validates thread-safe operations
- Reports any data race warnings

**What it catches:**
- Concurrent map access without locking
- Shared variable access from multiple goroutines
- Unsafe pointer operations
- Channel race conditions

**Example output:**
```
✅ No data races detected
```

### 2. Memory Leak Detector 💧

**Purpose:** Detect memory leaks using heap profiling

**How it works:**
- Starts daemon with pprof enabled
- Takes heap snapshots before and after load
- Forces GC and compares memory usage
- Analyzes heap profiles for growing allocations

**What it catches:**
- Unclosed file descriptors
- Unreleased HTTP connections
- Growing caches without bounds
- Goroutine leaks holding memory

**Artifacts:**
- `heap-initial.pprof` - Initial heap state
- `heap-final.pprof` - Final heap state after GC
- `daemon-leak.log` - GC trace logs

### 3. Goroutine Leak Detector 🔍

**Purpose:** Detect goroutines that don't terminate

**How it works:**
- Uses `goleak` package (uber-go/goleak)
- Verifies all goroutines terminate after tests
- Checks for blocked goroutines

**What it catches:**
- HTTP servers not shutting down properly
- Background workers not stopping
- Blocked channel operations
- Infinite loops in goroutines

### 4. Benchmark Profiling 📊

**Purpose:** Track performance and identify bottlenecks

**How it works:**
- Runs benchmarks with CPU and memory profiling
- Generates pprof profiles
- Reports allocations and execution time
- Identifies hot paths in code

**What it provides:**
- Operations per second (throughput)
- Bytes allocated per operation
- Allocations per operation
- CPU profile of hot functions
- Memory allocation hot spots

**Example output:**
```
BenchmarkProxyRequest-10       16345      69499 ns/op    45360 B/op    139 allocs/op
BenchmarkProxyLargeResponse-10  3086     387285 ns/op  2707.51 MB/s   45950 B/op    180 allocs/op
```

### 5. Test Coverage Quality 📈

**Purpose:** Analyze test coverage quality

**How it works:**
- Generates detailed coverage reports
- Identifies low-coverage packages (< 50%)
- Highlights well-tested packages (>= 80%)
- Generates HTML coverage report

**What it provides:**
- Total test coverage percentage
- Per-package coverage breakdown
- Coverage trends over time
- HTML visualization

## Running Locally

### Race Detector
```bash
go test -race ./...
```

### Memory Profiling
```bash
# Build with pprof
go build -o bin/xg2g ./cmd/daemon

# Run and access pprof
./bin/xg2g &
curl http://localhost:8080/debug/pprof/heap > heap.pprof
go tool pprof -http=:8081 heap.pprof
```

### Benchmarks
```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# With profiling
go test -bench=. -cpuprofile=cpu.pprof -memprofile=mem.pprof ./internal/proxy

# Analyze profiles
go tool pprof -http=:8081 cpu.pprof
go tool pprof -http=:8081 mem.pprof
```

### Coverage
```bash
# Generate coverage
go test -coverprofile=coverage.out ./...

# View in browser
go tool cover -html=coverage.out

# Detailed report
go tool cover -func=coverage.out
```

## CI Artifacts

All quality check runs produce artifacts available for download:

| Artifact | Description | Retention |
|----------|-------------|-----------|
| `race-detector-output` | Full race detector output | 7 days |
| `memory-profiles` | Heap profiles (pprof) | 7 days |
| `benchmark-profiles` | CPU/memory profiles | 14 days |
| `goroutine-leak-output` | Leak detection logs | 7 days |
| `coverage-reports` | Coverage data + HTML | 14 days |

## Interpreting Results

### Race Detector Failures

If the race detector finds issues:
1. Look for `WARNING: DATA RACE` in output
2. Identify the conflicting goroutines
3. Add proper synchronization (mutex, channels, sync.Map)
4. Re-run tests to verify fix

### Memory Leaks

Signs of memory leaks:
- Heap growing continuously over time
- GC not reclaiming expected memory
- High allocation rates without corresponding frees

**Fix strategy:**
1. Identify allocation hot spots in pprof
2. Check for unclosed resources
3. Verify goroutines terminate
4. Use `defer` for cleanup

### Goroutine Leaks

Common causes:
- HTTP servers not shut down
- Blocked channel sends/receives
- Missing context cancellation
- Infinite loops

**Fix strategy:**
1. Use context for cancellation
2. Always shutdown servers in tests
3. Use buffered channels where appropriate
4. Set timeouts on operations

### Performance Regressions

If benchmarks show degradation:
1. Compare CPU profiles before/after
2. Check for new allocations
3. Look for algorithmic changes
4. Verify no unintended locking

## Best Practices

### Writing Race-Safe Code

```go
// ❌ Bad: Concurrent map access
var cache = make(map[string]string)

func Get(key string) string {
    return cache[key] // RACE!
}

// ✅ Good: Use sync.Map or mutex
var cache sync.Map

func Get(key string) string {
    v, _ := cache.Load(key)
    return v.(string)
}
```

### Preventing Memory Leaks

```go
// ❌ Bad: Goroutine leak
func StartWorker() {
    go func() {
        for {
            // Never stops!
            doWork()
        }
    }()
}

// ✅ Good: Cancellable context
func StartWorker(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
            default:
                doWork()
            }
        }
    }()
}
```

### Writing Benchmarks

```go
func BenchmarkMyFunction(b *testing.B) {
    // Setup (not timed)
    data := prepareData()

    b.ResetTimer()      // Start timing
    b.ReportAllocs()    // Report allocations

    for i := 0; i < b.N; i++ {
        MyFunction(data)
    }
}
```

## Integration with Development Workflow

1. **Local Development:**
   - Run `-race` tests before committing
   - Profile performance-critical code

2. **Pull Requests:**
   - Quality checks run automatically
   - Review artifacts before merging
   - Fix any detected issues

3. **Post-Merge:**
   - Monitor daily scheduled runs
   - Track performance trends
   - Address regressions promptly

## Resources

- [Go Race Detector](https://go.dev/blog/race-detector)
- [pprof Profiling](https://go.dev/blog/pprof)
- [goleak Documentation](https://pkg.go.dev/go.uber.org/goleak)
- [Effective Go - Concurrency](https://go.dev/doc/effective_go#concurrency)
