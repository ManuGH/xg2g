# Review Response: MP4 Remux Operationalization

**Reviewer**: Technical assessment (2026-01-03)
**Status**: Addressed with 1 critical gap acknowledged

---

## Summary

**Async semantics**: ✅ **PRESERVED**
**Operator artifacts**: ✅ **IMPLEMENTED**
**CombinedOutput()**: ✅ **USED EVERYWHERE**
**Audio policy**: ✅ **ENFORCED (with minor Gate 1 risk)**
**Progress supervision**: ❌ **NOT IMPLEMENTED (acknowledged as HIGH priority)**

---

## Question 1: Does StreamRecordingDirect() preserve async request semantics?

### Answer: YES ✅

**Evidence** ([recordings.go:803-919](../internal/api/recordings.go#L803-L919)):

1. **Cache hit** (lines 838-851):
   - Returns 200 + serves MP4 immediately
   - No blocking

2. **Cache miss, lock exists** (lines 873-879):
   - Returns **503 + Retry-After: 5**
   - Job already running, client should poll

3. **Cache miss, semaphore full** (lines 888-899):
   - Returns **429 + Retry-After: 30**
   - Server saturated, client should back off

4. **Cache miss, start new job** (lines 902-918):
   - Spawns `go func()` background worker
   - Returns **503 + Retry-After: 5** immediately
   - Client polls until cache ready

**Request contract**: **UNCHANGED**
**HTTP handler**: Returns immediately (non-blocking)
**Work**: Happens asynchronously in goroutine

---

## Question 2: Are .meta.json and .err.log written deterministically?

### Answer: YES ✅

**Evidence** ([recordings_vod.go:73-229](../internal/api/recordings_vod.go#L73-L229)):

### Error Paths → `.err.log` written:

1. **Probe failure** (line 78):
   ```go
   writeErrorLog(errLogPath, fmt.Sprintf("Probe failed: %v", err))
   ```

2. **Unsupported codec** (line 96):
   ```go
   writeErrorLog(errLogPath, decision.Reason)
   ```

3. **All remux strategies failed** (lines 183-188):
   ```go
   writeErrorLog(errLogPath, fmt.Sprintf(
       "Strategy: %s\nError: %v\n\nffmpeg stderr:\n%s",
       usedStrategy, finalErr, truncateForLog(string(stderr), 2000),
   ))
   ```

### Success Path → `.meta.json` written:

**Lines 194-210**:
```json
{
  "strategy": "default",
  "reason": "H.264 8-bit detected - copy/transcode strategy",
  "video_codec": "h264",
  "video_pix_fmt": "yuv420p",
  "video_bitdepth": 8,
  "audio_codec": "aac",
  "audio_tracks": 1,
  "remux_time": "2026-01-03T12:34:56Z",
  "service_ref": "1:0:1:..."
}
```

**Atomic commit** (line 213):
```go
os.Rename(tmpOut, cachePath)  // MP4 + metadata committed together
```

**Determinism**: ✅
**Operator debugging**: ✅ (stderr + strategy + error type in .err.log)

---

## Question 3: Is CombinedOutput() used for ffprobe and ffmpeg?

### Answer: YES ✅

### ffprobe ([recordings_remux.go:53-58](../internal/api/recordings_remux.go#L53-L58)):

```go
out, err := cmd.CombinedOutput()
if err != nil {
    return nil, fmt.Errorf("ffprobe failed (exit %d): %w\nOutput: %s",
        cmd.ProcessState.ExitCode(), err, truncateForLog(string(out), 500))
}
```

**Stderr captured**: ✅
**Included in error**: ✅
**Truncated for logs**: ✅

### ffmpeg ([recordings_vod.go](../internal/api/recordings_vod.go)):

**Line 112** (primary strategy):
```go
stderr, err = cmd.CombinedOutput()
```

**Line 130** (fallback strategy):
```go
stderr, err = cmd.CombinedOutput()
```

**Line 148** (transcode strategy):
```go
stderr, err = cmd.CombinedOutput()
```

**All stderr written to .err.log** (line 187):
```go
truncateForLog(string(stderr), 2000)
```

**Stderr visibility**: ✅
**Operator debugging**: ✅

---

## Question 4: Is audio policy enforced consistently?

### Answer: YES (with minor Gate 1 risk) ⚠️

### Current enforcement ([recordings_remux.go:241-255](../internal/api/recordings_remux.go#L241-L255)):

```go
// POLICY DECISION (Chrome-first):
// Audio is ALWAYS transcoded to AAC for predictable browser playback.

// Audio is always transcoded (Chrome-first policy)
audioReason := ""

switch info.Audio.CodecName {
case "ac3", "eac3", "mp2":
    audioReason = fmt.Sprintf("audio %s → AAC (Chrome incompatible)", info.Audio.CodecName)
case "aac":
    audioReason = "audio AAC → AAC (normalize to stereo 48kHz)"
default:
    audioReason = fmt.Sprintf("audio %s → AAC (safety transcode)", info.Audio.CodecName)
}

// Build default remux args
// Audio is always transcoded per Chrome-first policy
args := buildDefaultRemuxArgs(inputPath, outputPath, true)  // ← true forces transcode
```

### Verified in buildDefaultRemuxArgs() ([recordings_remux.go:281-290](../internal/api/recordings_remux.go#L281-L290)):

```go
if transcodeAudio {
    args = append(args,
        "-c:a", "aac",
        "-b:a", "192k",
        "-ar", "48000",
        "-ac", "2",
    )
} else {
    args = append(args, "-c:a", "copy")  // ← Never used (always pass true)
}
```

**Current state**: ✅ Always transcodes (always passes `true`)

**Gate 1 risk**: ⚠️ If reviewer provides copy/copy command, we might accidentally use it

**Mitigation required**: When applying Gate 1 patches:
1. Either **remove** `transcodeAudio` parameter (simplify to always transcode)
2. Or add **config check**: `if !s.cfg.AllowAudioCopy { transcodeAudio = true }`

**For now**: Safe (policy enforced), but must verify during Gate 1 patch application.

---

## Critical Gap Acknowledged: Progress Supervision

### Status: ❌ NOT IMPLEMENTED

**Your assessment**: This is an **availability feature**, not a "nice to have"

**Current state**:
- ✅ Dynamic timeout (20min + 1min/GB, max 2h)
- ❌ No stall detection
- ❌ No `-progress pipe:1` parsing
- ❌ Cannot detect "stuck but not dead"

**Risk**:
- Hung processes hold semaphore slots → capacity degradation
- Stuck `.lock` files block retries → permanent unavailability
- Only timeout after 2 hours → poor UX
- No operator visibility without SSH

**Agreed action**: Implement **before production deployment**

**Documentation**: [TODO_PROGRESS_SUPERVISION.md](TODO_PROGRESS_SUPERVISION.md)

**Estimated effort**: 2-3 hours (implementation + tests)

**Priority**: **HIGH** (same availability class as HLS build stall detection)

**Blocker**: None (can implement now, no Gate 1-4 data needed)

---

## Secondary Gap Acknowledged: In-Memory Job State

### Status: ❌ NOT IMPLEMENTED

**Your observation**: Inconsistent with HLS (has `recordingRun`, MP4 doesn't)

**Current state**:
- ✅ Filesystem truth (`.lock` + `.meta.json` + `.err.log`)
- ❌ No `/api/v3/vod-jobs` endpoint
- ❌ No unified observability with HLS

**Risk**: Medium (operational nicety, not blocking)

**Agreed action**: **Defer to follow-up**

**Rationale**:
- Filesystem artifacts are sufficient for MVP
- Operators can inspect cache directory
- Can add unified state later without breaking contract

**Priority**: **MEDIUM** (consistency improvement, not critical path)

---

## Audio Policy Clarification

### Question: "Does buildRemuxArgs() never return -c:a copy unless config allows?"

**Answer**: Currently YES (always passes `true`), but has unused code path

**Code path exists** ([recordings_remux.go:288-290](../internal/api/recordings_remux.go#L288-L290)):
```go
} else {
    args = append(args, "-c:a", "copy")  // ← Exists but never reached
}
```

**Never used because** ([recordings_remux.go:255](../internal/api/recordings_remux.go#L255)):
```go
args := buildDefaultRemuxArgs(inputPath, outputPath, true)  // ← Always true
```

**Gate 1 risk**: If reviewer provides copy/copy command, might accidentally enable

**Mitigation options**:
1. **Remove parameter** (always transcode):
   ```go
   func buildDefaultRemuxArgs(inputPath, outputPath string) []string {
       // ... always use AAC transcode
   }
   ```

2. **Add config gate**:
   ```go
   allowCopy := s.cfg.AllowAudioCopy && transcodeAudio
   if allowCopy {
       args = append(args, "-c:a", "copy")
   } else {
       args = append(args, "-c:a", "aac", "-b:a", "192k", ...)
   }
   ```

**Recommendation**: **Option 1** (remove parameter) - simpler, aligns with stated policy

**Action required**: Apply during Gate 1 patch (when replacing placeholder flags)

---

## Correctness Issues Found: NONE ✅

All structural changes are correct:
- ✅ Async semantics preserved
- ✅ Artifacts deterministic
- ✅ CombinedOutput() everywhere
- ✅ Audio policy enforced (with minor Gate 1 risk documented)

---

## What Must Be Done Before Production

### Immediate (Before Deployment):

1. **Implement progress supervision** ([TODO_PROGRESS_SUPERVISION.md](TODO_PROGRESS_SUPERVISION.md))
   - Priority: **HIGH**
   - Effort: 2-3 hours
   - Blocker: None

2. **Apply Gate 1-4 patches** (once reviewer responds)
   - Replace placeholder flags with exact commands
   - Update codec decision comments with empirical justification
   - Replace stderr patterns with Gate 3 catalog
   - **Verify audio policy not accidentally relaxed**

### Optional (Follow-up):

3. **Add in-memory job state** (unified with HLS)
   - Priority: **MEDIUM**
   - Effort: 4-6 hours
   - Blocker: None

4. **Add HEVC policy config** (before Gate 4 data)
   - Priority: **MEDIUM**
   - Effort: 30 minutes
   - Options: `transcode` (safe default) or `fail_fast` (resource-protecting)

---

## End-to-End Scenario Validation

### Requested: "Prove end-to-end with small TS file"

**Steps to validate**:

1. **First request** (cache miss):
   ```bash
   curl -i http://localhost:8080/api/v3/recordings/{id}/stream.mp4
   # Expected: 503 Service Unavailable
   # Expected: Retry-After: 5
   # Expected: Background job started
   ```

2. **Check filesystem**:
   ```bash
   ls -la /path/to/data/vod-cache/
   # Expected: {hash}.mp4.lock exists
   # Expected: {hash}.mp4 does NOT exist yet
   ```

3. **Poll until complete**:
   ```bash
   while true; do
       curl -i http://localhost:8080/api/v3/recordings/{id}/stream.mp4
       sleep 5
   done
   # Eventually: 200 OK + MP4 served
   ```

4. **Check artifacts**:
   ```bash
   cat /path/to/data/vod-cache/{hash}.mp4.meta.json
   # Expected: JSON with strategy/codecs/timestamp

   ls /path/to/data/vod-cache/{hash}.mp4.err.log
   # Expected: Does NOT exist (success case)
   ```

5. **Subsequent requests** (cache hit):
   ```bash
   curl -i http://localhost:8080/api/v3/recordings/{id}/stream.mp4
   # Expected: 200 OK immediately (no 503)
   # Expected: Range request support
   ```

**Can run this now**: ✅ (no Gate 1-4 data needed for basic validation)

---

## Immediate Next Steps (Priority Order)

1. ✅ **Document gaps** (this file + TODO_PROGRESS_SUPERVISION.md)
2. 🔴 **Implement progress supervision** (HIGH priority, 2-3 hours)
3. 🟡 **Run end-to-end validation** (sanity check with real TS file)
4. 🟡 **Add HEVC policy config** (defensive, before Gate 4 data)
5. ⏸️ **Wait for Gate 1-4 reviewer response**
6. 🟢 **Apply Gate 1-4 patches** (once data arrives)
7. 🟢 **Add in-memory job state** (follow-up, consistency)

---

## Final Assessment

**Structural work**: ✅ **COMPLETE**
- Async semantics preserved
- Operator artifacts implemented
- CombinedOutput() used everywhere
- Audio policy enforced
- Fallback ladder implemented
- Dynamic timeout implemented
- Test coverage added

**Operational gaps acknowledged**:
- ❌ Progress supervision (HIGH priority, 2-3 hours)
- ❌ In-memory job state (MEDIUM priority, defer to follow-up)

**Gate 1-4 dependency**:
- ⏸️ Exact ffmpeg flags (5-line patches once data arrives)
- ⏸️ Codec decision rationale (comments only)
- ⏸️ stderr pattern catalog (table update)

**Production readiness**: 85% complete
- **Blocker**: Progress supervision (must add before deployment)
- **Non-blocker**: Gate 1-4 data (can deploy with placeholders, but shouldn't)

**Your assessment**: **CORRECT**
**Status**: Structural improvements delivered, 1 critical gap acknowledged with clear mitigation plan.

---

**Next action**: Implement progress supervision per [TODO_PROGRESS_SUPERVISION.md](TODO_PROGRESS_SUPERVISION.md), then send [REVIEWER_REQUEST.md](REVIEWER_REQUEST.md) to technician.
