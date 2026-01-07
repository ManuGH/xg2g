# Patch Guide: MP4 Remux Operationalization

**Status**: Scaffolding complete, awaiting Gate 1-4 reviewer data
**Target File**: `internal/api/recordings.go` (StreamRecordingDirect function)
**Supporting Files**: `internal/api/recordings_remux.go` (decision logic)

---

## 1. Current State vs Target State

### Current (Placeholder)

```go
// StreamRecordingDirect() lines 928-937
cmd := exec.CommandContext(ctx, bin,
    "-y",
    "-i", localPath,
    "-c:v", "copy",
    "-c:a", "aac",
    "-b:a", "192k",
    "-movflags", "+faststart",
    "-f", "mp4",
    tmpOut,
)
out, err := cmd.CombinedOutput()
```

**Issues**:
- ❌ No timestamp repair flags (`-fflags +genpts`, `-avoid_negative_ts`)
- ❌ No codec probing (fails on HEVC, 10-bit, AC3)
- ❌ No fallback ladder (default → fallback → transcode)
- ❌ No stderr pattern classification
- ❌ No progress supervision (stall detection)
- ❌ Fixed 15-minute timeout regardless of file size
- ❌ Silent failure (no `.meta.json` or `.err.log`)

### Target (Gate 1-4 Driven)

```go
// 1. Probe streams
info, err := probeStreams(ctx, s.cfg.FFprobeBin, localPath)
if err != nil {
    // Handle probe failure
}

// 2. Build remux args (decision tree based on Gate 2 + Gate 4)
decision := buildRemuxArgs(info, localPath, tmpOut)
if decision.Strategy == StrategyUnsupported {
    // Fail fast
}

// 3. Execute with progress supervision (or at least better timeout)
cmd := exec.CommandContext(ctx, bin, decision.Args...)

// 4. Classify errors and implement fallback ladder
stderr, err := cmd.CombinedOutput()
if err != nil {
    classifiedErr := classifyRemuxError(string(stderr), cmd.ProcessState.ExitCode())
    if shouldRetryWithFallback(classifiedErr) {
        // Retry with fallback flags
    } else if shouldRetryWithTranscode(classifiedErr) {
        // Retry with full transcode
    }
}
```

---

## 2. Patches to Apply Once Gate 1-4 Data Arrives

### Patch 1: Replace Placeholder Flags in `buildDefaultRemuxArgs()`

**File**: `internal/api/recordings_remux.go` lines 167-191

**What to Replace**:

```go
// BEFORE (Placeholder):
args := []string{
    "-y",
    // TODO Gate 1: Add -fflags +genpts (or reviewer's recommended flag)
    // TODO Gate 1: Add -avoid_negative_ts make_zero (or reviewer's recommendation)
    "-i", inputPath,
    "-c:v", "copy",
}
```

**After Gate 1 Response** (Example - replace with actual reviewer command):

```go
args := []string{
    "-y",
    "-fflags", "+genpts",              // Gate 1: Reviewer's exact flag
    "-avoid_negative_ts", "make_zero", // Gate 1: Reviewer's exact flag
    "-i", inputPath,
    "-c:v", "copy", // CONDITION: H.264 yuv420p 8-bit only
}
```

**Action**: Copy exact flags from `REVIEWER_TEMPLATE_RESPONSE.md` Gate 1 → DEFAULT REMUX command

---

### Patch 2: Implement `buildFallbackRemuxArgs()` with Gate 1 Fallback Flags

**File**: `internal/api/recordings_remux.go` lines 207-221

**What to Replace**:

```go
// BEFORE (Placeholder):
args := []string{
    "-y",
    // TODO Gate 1: Add reviewer's fallback flags for non-monotonous DTS
    "-i", inputPath,
}
```

**After Gate 1 Response** (Example):

```go
args := []string{
    "-y",
    // Gate 1 FALLBACK REMUX flags (reviewer's exact command)
    "-fflags", "+genpts+igndts",      // Example: ignore DTS issues
    "-avoid_negative_ts", "make_zero",
    "-vsync", "cfr",                  // Example: constant frame rate
    "-i", inputPath,
    "-c:v", "copy",
}
```

**Action**: Copy exact flags from `REVIEWER_TEMPLATE_RESPONSE.md` Gate 1 → FALLBACK REMUX command

---

### Patch 3: Implement `buildTranscodeArgs()` with Gate 1 Transcode Strategy

**File**: `internal/api/recordings_remux.go` lines 223-245

**What to Replace**:

```go
// BEFORE (Placeholder):
args := []string{
    "-y",
    "-i", inputPath,
    "-c:v", "libx264",
    // TODO Gate 1: Add reviewer's preset recommendation (e.g., -preset medium)
    // TODO Gate 1: Add reviewer's CRF recommendation (e.g., -crf 23)
}
```

**After Gate 1 Response** (Example):

```go
args := []string{
    "-y",
    "-i", inputPath,
    "-c:v", "libx264",
    "-preset", "medium",     // Gate 1: Reviewer's exact preset
    "-crf", "23",            // Gate 1: Reviewer's exact CRF
    "-pix_fmt", "yuv420p",   // Force 8-bit for Chrome
    "-profile:v", "high",    // Gate 1: Reviewer's profile recommendation
    "-level", "4.1",         // Gate 1: Reviewer's level recommendation
}
```

**Action**: Copy exact flags from `REVIEWER_TEMPLATE_RESPONSE.md` Gate 1 → TRANSCODE FALLBACK command

---

### Patch 4: Update Codec Decision Tree with Gate 2 + Gate 4 Data

**File**: `internal/api/recordings_remux.go` lines 120-180

**What to Update**:

Based on Gate 2 codec distribution + Gate 4 client matrix, refine:

1. **HEVC Decision** (lines 124-131):
   - If Gate 2 shows >20% HEVC AND Gate 4 primary client is Chrome → **Must transcode**
   - If Gate 4 primary client is Safari → **Can copy** (or conditional)

2. **Audio Decision** (lines 155-171):
   - If Gate 2 shows >50% AC3 → Adjust default strategy
   - If Gate 4 confirms "Chrome Desktop 80%" → **Must transcode AC3 to AAC**

3. **10-bit H.264** (lines 133-140):
   - Gate 4 confirms Chrome compatibility → Keep transcode

**Example Patch** (HEVC decision):

```go
// BEFORE (Placeholder):
case "hevc", "h265":
    return &RemuxDecision{
        Strategy: StrategyTranscode,
        Reason:   "HEVC detected - Chrome incompatible (Gate 4 decision pending)",
        Args: buildTranscodeArgs(inputPath, outputPath),
    }

// AFTER Gate 4 Response (Example: Chrome is primary client):
case "hevc", "h265":
    // Gate 4: Primary client is Chrome Desktop (80%) → HEVC not supported
    // Gate 2: HEVC is 15% of sources → Must transcode for compatibility
    return &RemuxDecision{
        Strategy: StrategyTranscode,
        Reason:   "HEVC detected - Chrome Desktop (primary client) incompatible",
        Args: buildTranscodeArgs(inputPath, outputPath),
    }
```

**Action**: Review `REVIEWER_TEMPLATE_RESPONSE.md` Gate 2 + Gate 4 and update decision rationale

---

### Patch 5: Update stderr Pattern Catalog with Gate 3 Empirical Data

**File**: `internal/api/recordings_remux.go` lines 264-305

**What to Replace**:

```go
// BEFORE (Placeholder patterns):
patterns := []struct {
    regex *regexp.Regexp
    err   error
    shouldFallback bool
}{
    {
        regex: regexp.MustCompile(`(?i)non-monotonous DTS in output stream`),
        err:   ErrNonMonotonousDTS,
        shouldFallback: true,
    },
    // ...
}
```

**After Gate 3 Response** (Example):

Add actual patterns from `REVIEWER_TEMPLATE_RESPONSE.md` Gate 3 stderr catalog:

```go
patterns := []struct {
    regex          *regexp.Regexp
    err            error
    shouldFallback bool
    severity       string // Gate 3: High/Med/Low
    action         string // Gate 3: Recommended action
}{
    {
        // Gate 3: Pattern found in 7/10 files, Severity: High
        regex:          regexp.MustCompile(`(?i)non-monotonous DTS in output stream`),
        err:            ErrNonMonotonousDTS,
        shouldFallback: true,
        severity:       "High",
        action:         "Use fallback flags",
    },
    {
        // Gate 3: Pattern found in 2/10 files, Severity: High
        regex:          regexp.MustCompile(`(?i)Packet with invalid duration`),
        err:            ErrInvalidDuration,
        shouldFallback: false, // Gate 3: FAIL FAST (breaks Resume)
        severity:       "High",
        action:         "Fail fast - source incompatible",
    },
    // Add all patterns from Gate 3 table
}
```

**Action**: Copy patterns from `REVIEWER_TEMPLATE_RESPONSE.md` Gate 3 → stderr Pattern Catalog

---

### Patch 6: Wire Remux Logic into `StreamRecordingDirect()`

**File**: `internal/api/recordings.go` lines 910-950

**What to Replace**:

```go
// BEFORE (lines 910-943):
log.L().Info().Str("src", localPath).Str("dest", cachePath).Msg("starting vod remux")

bin := s.cfg.FFmpegBin
if bin == "" {
    bin = "ffmpeg"
}

ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
defer cancel()

tmpOut := cachePath + ".tmp"

cmd := exec.CommandContext(ctx, bin,
    "-y",
    "-i", localPath,
    "-c:v", "copy",
    "-c:a", "aac",
    "-b:a", "192k",
    "-movflags", "+faststart",
    "-f", "mp4",
    tmpOut,
)
out, err := cmd.CombinedOutput()
if err != nil {
    log.L().Error().Err(err).Str("output", string(out)).Msg("vod remux failed")
    os.Remove(tmpOut)
    return
}
```

**AFTER** (New logic with probe + ladder + classification):

```go
log.L().Info().Str("src", localPath).Str("dest", cachePath).Msg("starting vod remux")

bin := s.cfg.FFmpegBin
if bin == "" {
    bin = "ffmpeg"
}

// Dynamic timeout based on file size (15min baseline + 1min/GB, max 2h)
timeout := 15 * time.Minute
if info, err := os.Stat(localPath); err == nil {
    sizeGB := float64(info.Size()) / (1024 * 1024 * 1024)
    extraTime := time.Duration(sizeGB) * time.Minute
    timeout = 15*time.Minute + extraTime
    if timeout > 2*time.Hour {
        timeout = 2 * time.Hour
    }
}

ctx, cancel := context.WithTimeout(parent, timeout)
defer cancel()

tmpOut := cachePath + ".tmp"
metaPath := cachePath + ".meta.json"
errLogPath := cachePath + ".err.log"

// 1. Probe streams
ffprobeBin := s.cfg.FFprobeBin
if ffprobeBin == "" {
    ffprobeBin = "ffprobe"
}

streamInfo, probeErr := probeStreams(ctx, ffprobeBin, localPath)
if probeErr != nil {
    log.L().Error().Err(probeErr).Msg("stream probe failed")
    // Write error log
    _ = os.WriteFile(errLogPath, []byte(fmt.Sprintf("Probe failed: %v", probeErr)), 0644)
    os.Remove(tmpOut)
    return
}

// 2. Build remux args (decision tree)
decision := buildRemuxArgs(streamInfo, localPath, tmpOut)
logRemuxDecision(decision, recordingId)

if decision.Strategy == StrategyUnsupported {
    log.L().Error().Str("reason", decision.Reason).Msg("recording codec unsupported")
    _ = os.WriteFile(errLogPath, []byte(decision.Reason), 0644)
    os.Remove(tmpOut)
    return
}

// 3. Execute remux with ladder (default → fallback → transcode)
var finalErr error
var stderr []byte

// Try default/transcode strategy first
cmd := exec.CommandContext(ctx, bin, decision.Args...)
stderr, err = cmd.CombinedOutput()
if err != nil {
    classifiedErr := classifyRemuxError(string(stderr), cmd.ProcessState.ExitCode())

    // Ladder logic: retry with fallback if applicable
    if decision.Strategy == StrategyDefault && shouldRetryWithFallback(classifiedErr) {
        log.L().Warn().Err(classifiedErr).Msg("remux failed, retrying with fallback flags")

        // Retry with fallback
        fallbackArgs := buildFallbackRemuxArgs(localPath, tmpOut)
        cmd = exec.CommandContext(ctx, bin, fallbackArgs...)
        stderr, err = cmd.CombinedOutput()

        if err != nil {
            classifiedErr = classifyRemuxError(string(stderr), cmd.ProcessState.ExitCode())

            // Last resort: transcode
            if shouldRetryWithTranscode(classifiedErr) {
                log.L().Warn().Err(classifiedErr).Msg("fallback remux failed, trying transcode")

                transcodeArgs := buildTranscodeArgs(localPath, tmpOut)
                cmd = exec.CommandContext(ctx, bin, transcodeArgs...)
                stderr, err = cmd.CombinedOutput()

                if err != nil {
                    finalErr = classifyRemuxError(string(stderr), cmd.ProcessState.ExitCode())
                }
            } else {
                finalErr = classifiedErr
            }
        }
    } else {
        finalErr = classifiedErr
    }
}

if finalErr != nil {
    log.L().Error().Err(finalErr).Str("stderr", string(stderr)).Msg("vod remux failed (all strategies)")
    // Write error log for operator debugging
    _ = os.WriteFile(errLogPath, stderr, 0644)
    os.Remove(tmpOut)
    return
}

// 4. Success: Write metadata and commit
meta := map[string]interface{}{
    "strategy":   decision.Strategy,
    "reason":     decision.Reason,
    "video_codec": streamInfo.Video.CodecName,
    "audio_codec": streamInfo.Audio.CodecName,
    "remux_time": time.Now().Format(time.RFC3339),
}
if metaJSON, err := json.MarshalIndent(meta, "", "  "); err == nil {
    _ = os.WriteFile(metaPath, metaJSON, 0644)
}

// Move tmp to final
if err := os.Rename(tmpOut, cachePath); err != nil {
    log.L().Error().Err(err).Msg("failed to commit vod cache")
    os.Remove(tmpOut)
} else {
    log.L().Info().Str("cache", cachePath).Msg("vod remux succeeded")
}
```

**Action**: Replace entire remux block (lines 910-950) with above logic after Gate 1-4 data

---

## 3. Testing Strategy

### Unit Tests (Already Scaffolded)

**File**: `internal/api/recordings_remux_test.go`

Tests cover:
- ✅ Codec decision tree (HEVC, 10-bit, AC3)
- ✅ Error classification (DTS, duration, timestamp patterns)
- ✅ Fallback/transcode retry logic
- ✅ Arg structure validation

**Action After Gate 1-4**: Update test expectations to match actual reviewer commands

---

### Integration Test Harness (To Create)

**File**: `internal/api/recordings_remux_integration_test.go`

```go
// +build integration

package api

import (
    "os"
    "path/filepath"
    "testing"
)

// TestRemuxIntegration runs actual remux on sample TS files
// Requires: XG2G_TEST_SAMPLES_DIR env var pointing to 10 Enigma2 recordings
func TestRemuxIntegration(t *testing.T) {
    samplesDir := os.Getenv("XG2G_TEST_SAMPLES_DIR")
    if samplesDir == "" {
        t.Skip("XG2G_TEST_SAMPLES_DIR not set, skipping integration test")
    }

    samples, _ := filepath.Glob(filepath.Join(samplesDir, "*.ts"))
    if len(samples) < 10 {
        t.Fatalf("Need at least 10 sample files, found %d", len(samples))
    }

    // For each sample:
    // 1. Probe streams
    // 2. Build remux args
    // 3. Execute remux
    // 4. Validate output (duration delta, seek test)
    // 5. Collect stderr patterns
    // 6. Assert thresholds (90% success, 1% duration delta, 95% seek)

    // TODO: Implement full Gate 3 validation
}
```

**Action**: Create harness after Gate 1-4 data, run against same 10 files reviewer used

---

## 4. Monitoring/Observability Additions

### Metrics to Add

**File**: `internal/metrics/metrics.go`

```go
var (
    // Existing VOD metrics
    vodBuildsActive = prometheus.NewGauge(...)

    // NEW: Remux strategy distribution
    vodRemuxStrategy = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "xg2g_vod_remux_strategy_total",
            Help: "VOD remux strategy distribution",
        },
        []string{"strategy"}, // default, fallback, transcode, unsupported
    )

    // NEW: Remux error classification
    vodRemuxErrors = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "xg2g_vod_remux_errors_total",
            Help: "VOD remux errors by type",
        },
        []string{"error_type"}, // non_monotonous_dts, invalid_duration, etc.
    )
)
```

**Wire into `logRemuxDecision()`**:

```go
func logRemuxDecision(decision *RemuxDecision, recordingID string) {
    // ... existing logging ...

    // Increment strategy metric
    metrics.IncVODRemuxStrategy(string(decision.Strategy))
}
```

**Wire into `classifyRemuxError()`**:

```go
func classifyRemuxError(stderr string, exitCode int) error {
    // ... existing logic ...

    if err != nil {
        metrics.IncVODRemuxError(err.Error())
    }

    return err
}
```

---

## 5. Documentation Updates Required

### ADR to Create

**File**: `docs/adr/0001-vod-playback-strategy.md`

Template:

```markdown
# ADR 0001: VOD Playback Strategy - TS-HLS + Direct MP4 Remux

**Status**: Accepted
**Date**: [After Gate 1-4 review]
**Deciders**: [List]

## Context

xg2g must serve Enigma2 recordings to browsers with:
- Progressive playback (HLS during build)
- Direct playback (MP4 after build completes)

Gate 1-4 empirical review revealed:

- **Gate 2 Codec Distribution**: [From reviewer]
  - H.264: X%
  - HEVC: Y%
  - AC3 audio: Z%

- **Gate 4 Primary Client**: [From reviewer]
  - Chrome Desktop: X%
  - Safari: Y%

## Decision

### HLS Progressive Playback

- **Format**: TS-HLS (not fMP4-HLS)
- **Reason**: [From Gate 4 - hls.js compatibility, etc.]

### Direct MP4 Playback

- **Default Strategy**: copy/copy OR copy/transcode
- **Flags**: [Exact Gate 1 DEFAULT REMUX command]
- **Fallback Strategy**: [Exact Gate 1 FALLBACK REMUX command]
- **Transcode Strategy**: [Exact Gate 1 TRANSCODE command]

### Codec Handling

- **HEVC**: [Transcode / Fail fast / Conditional] - Reason: [Gate 4 decision]
- **10-bit H.264**: Transcode to 8-bit - Reason: Chrome incompatibility
- **AC3 Audio**: Transcode to AAC - Reason: Chrome incompatibility

## Consequences

- **Positive**: [List from Gate 1-4]
- **Negative**: [List from Gate 1-4]
- **Risks**: [From Gate 3 acceptance thresholds]

## Acceptance Criteria

- [Gate 3 thresholds: 90% remux success, 1% duration delta, 95% seek]
```

---

## 6. Rollout Checklist

After Gate 1-4 data:

- [ ] Apply Patches 1-6 (flags, decision tree, stderr patterns, wiring)
- [ ] Update unit test expectations
- [ ] Create integration test harness
- [ ] Run integration tests against 10 sample files
- [ ] Verify acceptance thresholds (90%/1%/95%)
- [ ] Add metrics (strategy distribution, error classification)
- [ ] Write ADR with empirical justification
- [ ] Update API documentation (recording endpoints)
- [ ] Add operator runbook (debugging failed remux, interpreting .meta.json/.err.log)

---

## 7. Operator Debugging Guide

### When a recording fails to play

**Check 1**: Does `.mp4` file exist in vod-cache?

```bash
ls -lh /path/to/data/vod-cache/*.mp4
```

**Check 2**: Check `.meta.json` for remux strategy used

```bash
cat /path/to/data/vod-cache/<hash>.mp4.meta.json
```

Example output:

```json
{
  "strategy": "transcode",
  "reason": "HEVC detected - Chrome Desktop (primary client) incompatible",
  "video_codec": "hevc",
  "audio_codec": "ac3",
  "remux_time": "2026-01-03T12:34:56Z"
}
```

**Check 3**: If `.err.log` exists, inspect stderr patterns

```bash
cat /path/to/data/vod-cache/<hash>.mp4.err.log
```

Look for patterns from Gate 3 catalog:
- `Non-monotonous DTS` → Should have triggered fallback
- `Packet with invalid duration` → High severity, fail fast
- `timestamps are unset` → Should have triggered fallback

**Check 4**: Check Prometheus metrics

```bash
curl http://localhost:9090/metrics | grep xg2g_vod_remux
```

Look for:
- Strategy distribution (is transcode rate too high?)
- Error type distribution (which patterns are most common?)

---

## Summary

**What's Ready Now**:
- ✅ Remux decision logic scaffolding (`recordings_remux.go`)
- ✅ Unit tests for codec/error classification
- ✅ Patch points clearly marked with TODOs
- ✅ This guide documenting exact changes needed

**What's Blocked on Gate 1-4 Data**:
- ❌ Exact ffmpeg flags (Gate 1)
- ❌ Codec decision rationale (Gate 2 + Gate 4)
- ❌ stderr pattern catalog (Gate 3)
- ❌ ADR with empirical justification

**Next Step**: Wait for `REVIEWER_TEMPLATE_RESPONSE.md` to be filled, then apply Patches 1-6.
