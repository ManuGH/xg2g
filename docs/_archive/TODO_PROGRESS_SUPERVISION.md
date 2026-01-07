# TODO: Add MP4 Remux Progress Supervision

**Priority**: HIGH (Availability Feature)
**Status**: ✅ COMPLETED (2026-01-03)
**Target**: Before production deployment

---

## Problem

Current MP4 remux only has:
- ✅ Dynamic timeout (20min + 1min/GB, max 2h)
- ❌ No stall detection
- ❌ No progress monitoring

**Risk**: "Stuck but not dead" ffmpeg processes that:
- Hold semaphore slots
- Block cache slots with `.lock` files
- Only timeout after 2 hours (worst case)
- Cannot be diagnosed without process inspection

---

## Solution

Add `-progress pipe:1` supervision (matching HLS build pattern).

### Step 1: Add progress pipe to ffmpeg commands

**File**: `internal/api/recordings_vod.go` (lines 111-173)

**Change**:
```go
// Current (lines 111-112):
cmd := exec.CommandContext(ctx, ffmpegBin, decision.Args...)
stderr, err = cmd.CombinedOutput()

// After:
// Create progress pipe for stall detection
progressR, progressW, err := os.Pipe()
if err != nil {
    return fmt.Errorf("failed to create progress pipe: %w", err)
}
defer progressR.Close()
defer progressW.Close()

// Add -progress to args
argsWithProgress := append([]string{"-progress", "pipe:1"}, decision.Args...)
cmd := exec.CommandContext(ctx, ffmpegBin, argsWithProgress...)
cmd.Stdout = progressW
cmd.Stderr = &stderrBuf  // Capture stderr separately

// Start watchdog
stallCtx, stallCancel := context.WithCancel(ctx)
defer stallCancel()
go watchFFmpegProgress(progressR, stallCtx, stallCancel, 90*time.Second)

// Execute
err = cmd.Start()
progressW.Close() // Close write end in parent
if err != nil {
    return fmt.Errorf("failed to start ffmpeg: %w", err)
}

err = cmd.Wait()
stderr = stderrBuf.Bytes()
```

**Apply to all 3 execution points**:
1. Line 111 (primary strategy)
2. Line 129 (fallback strategy)
3. Line 147 (transcode strategy)

---

### Step 2: Implement watchFFmpegProgress()

**File**: `internal/api/recordings_vod.go`

**New function**:
```go
// watchFFmpegProgress monitors ffmpeg progress output and kills on stall
func watchFFmpegProgress(r io.Reader, ctx context.Context, cancel context.CancelFunc, stallTimeout time.Duration) {
    scanner := bufio.NewScanner(r)
    lastProgress := time.Now()
    var lastOutTime int64

    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    // Grace period: allow 30s at startup before checking stalls
    gracePeriod := time.Now().Add(30 * time.Second)

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Check for stall
            if time.Now().After(gracePeriod) && time.Since(lastProgress) > stallTimeout {
                log.L().Warn().
                    Dur("stall_duration", time.Since(lastProgress)).
                    Int64("last_out_time_us", lastOutTime).
                    Msg("ffmpeg stalled, killing process")
                cancel() // Trigger context cancellation
                return
            }
        default:
            if scanner.Scan() {
                line := scanner.Text()

                // Parse out_time_us to detect progress
                if strings.HasPrefix(line, "out_time_us=") {
                    if newTime, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_us="), 10, 64); err == nil {
                        if newTime != lastOutTime {
                            lastOutTime = newTime
                            lastProgress = time.Now()
                        }
                    }
                }

                // Alternative: parse total_size for copy-only remux
                if strings.HasPrefix(line, "total_size=") {
                    lastProgress = time.Now()
                }
            } else {
                // Scanner ended (EOF or error)
                return
            }
        }
    }
}
```

---

### Step 3: Update error handling

**Current** (line 119):
```go
classifiedErr := classifyRemuxError(string(stderr), exitCode)
```

**After**:
```go
// Check if killed due to stall
if ctx.Err() == context.Canceled {
    classifiedErr = ErrFFmpegStalled
} else {
    classifiedErr = classifyRemuxError(string(stderr), exitCode)
}
```

**Add to retry logic** (line 126):
```go
// Do NOT retry stalls with fallback (they're not timestamp issues)
if errors.Is(classifiedErr, ErrFFmpegStalled) {
    finalErr = classifiedErr
    usedStrategy = decision.Strategy
    break // Exit ladder
}

if decision.Strategy == StrategyDefault && shouldRetryWithFallback(classifiedErr) {
    // ... existing fallback logic
}
```

---

### Step 4: Add metrics

**File**: `internal/metrics/metrics.go`

```go
var (
    vodRemuxStalls = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "xg2g_vod_remux_stalls_total",
            Help: "VOD remux processes killed due to stall",
        },
        []string{"strategy"}, // default, fallback, transcode
    )
)

func IncVODRemuxStall(strategy string) {
    vodRemuxStalls.WithLabelValues(strategy).Inc()
}
```

**Wire into watchdog**:
```go
log.L().Warn().Msg("ffmpeg stalled, killing process")
metrics.IncVODRemuxStall(currentStrategy) // Pass from caller
cancel()
```

---

## Testing Strategy

### Unit Test

**File**: `internal/api/recordings_vod_test.go`

```go
func TestWatchFFmpegProgress_Stall(t *testing.T) {
    r, w, _ := os.Pipe()
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Write initial progress
    w.Write([]byte("out_time_us=1000000\n"))
    w.Write([]byte("progress=continue\n"))

    // Start watchdog with short stall timeout
    stallChan := make(chan bool, 1)
    go func() {
        watchFFmpegProgress(r, ctx, cancel, 2*time.Second)
        stallChan <- true
    }()

    // Wait for stall detection (should trigger after 2s + grace)
    select {
    case <-stallChan:
        // Expected - stall detected
    case <-time.After(5 * time.Second):
        t.Fatal("watchdog did not detect stall")
    }

    // Verify context was cancelled
    assert.Equal(t, context.Canceled, ctx.Err())
}
```

### Integration Test

**Manual test with intentional stall**:
```bash
# Create a fake ffmpeg that writes progress then hangs
cat > /tmp/fake-ffmpeg.sh <<'EOF'
#!/bin/bash
echo "out_time_us=1000000" >&1
echo "progress=continue" >&1
sleep 300  # Hang for 5 minutes
EOF
chmod +x /tmp/fake-ffmpeg.sh

# Point xg2g at fake ffmpeg
export XG2G_V3_FFMPEG_BIN=/tmp/fake-ffmpeg.sh

# Trigger remux, should kill after 90s stall
curl http://localhost:8080/api/v3/recordings/{id}/stream.mp4

# Verify:
# 1. Process killed after 90s (not 2h timeout)
# 2. .err.log contains "ffmpeg stalled"
# 3. Metrics show xg2g_vod_remux_stalls_total increment
```

---

## Acceptance Criteria

- [x] All 3 ffmpeg execution points use `-progress pipe:1` ✅ (runFFmpegWithProgress in recordings_vod.go)
- [x] Watchdog kills after 90s stall (post-30s grace period) ✅ (watchFFmpegProgress implementation)
- [x] Stall is logged with duration + last out_time_us ✅ (lines 292-299 in recordings_vod.go)
- [x] Stalls are classified as `ErrFFmpegStalled` (don't retry) ✅ (shouldRetryWithFallback rejects stalls)
- [x] Metric `xg2g_vod_remux_stalls_total` increments on stall ✅ (line 291 in recordings_vod.go)
- [x] Unit test validates stall detection ✅ (4 tests in recordings_vod_test.go)
- [ ] Integration test with fake-ffmpeg validates real behavior ⚠️ (DEFERRED: manual validation recommended)

---

## Rollout Plan

**Phase 1**: Implement (1-2 hours)
- Add progress pipe infrastructure
- Implement watchdog
- Wire into all 3 execution points

**Phase 2**: Test (30 minutes)
- Unit test stall detection
- Manual test with fake-ffmpeg
- Verify metrics

**Phase 3**: Deploy to staging (monitor)
- Watch for false positives (legitimate slow encodes killed)
- Tune grace period / stall timeout if needed
- Validate real-world behavior

**Phase 4**: Production
- Deploy with conservative timeouts (90s stall, 30s grace)
- Monitor `xg2g_vod_remux_stalls_total` metric
- Adjust based on empirical data

---

## Why This Matters

**Without stall detection**:
- Stuck processes hold semaphore slots → capacity degradation
- Stuck `.lock` files block retries → permanent unavailability
- Only timeout after 2 hours → poor user experience
- Operators cannot diagnose without SSH + process inspection

**With stall detection**:
- Fast failure (90s vs 2h) → better UX
- Automatic cleanup → no manual intervention
- Observable via metrics → operational visibility
- Matches HLS build robustness → consistency

---

**Status**: Ready to implement. Estimated effort: 2-3 hours (implementation + tests).

**Blocker**: None (can implement now, no Gate 1-4 data needed).

**Priority**: HIGH - Should be done before production deployment alongside Gate 1-4 patches.
