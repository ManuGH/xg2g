# N≥3 Validation Checklist: MP4 Remux Production Readiness

**Date**: 2026-01-03
**Operator**: Technician (xg2g Host)
**Environment**: xg2g instance (`/root/xg2g`)
**NFS Mount**: `/media/nfs-recordings` (mergerfs, 11TB, 74% used)

---

## Executive Summary

**Goal**: Reduce risk from **MEDIUM to LOW** by validating MP4 remux with N≥3 diverse sources.

**Current Status**:
- ✅ N=1 validation complete (ORF1 HD, 0.01% delta)
- ⚠️ **Only 2 recordings available** (1 in trash)
- ⚠️ Fallback/Transcode paths **untested** (no DTS/HEVC sources)

**Reality**: Cannot achieve full N≥3 without additional recordings.

**Pragmatic Approach**:
1. **Test what's available** (ORF1 HD + trash recovery if needed)
2. **Execute mandatory stall test** (fake-ffmpeg)
3. **Deploy to staging** with Go-Live monitoring
4. **Collect N≥3 organically** from first 10-20 production remuxes

---

## Prerequisites

### Environment Check

```bash
# Verify xg2g instance
hostname  # Should be: xg2g
pwd       # Should be: /root/xg2g

# Verify NFS mount
df -h /media/nfs-recordings
# Expected: mergerfs, 11TB, mounted

# Verify ffmpeg/ffprobe available
which ffmpeg ffprobe
# Should return: /usr/bin/ffmpeg, /usr/bin/ffprobe

# Verify xg2g running
systemctl status xg2g 2>/dev/null || docker ps | grep xg2g || ps aux | grep xg2g
```

### Available Recordings

```bash
# List all TS files (excluding trash)
find /media/nfs-recordings -name "*.ts" -type f -size +500M 2>/dev/null | grep -v -i trash

# Current known recordings:
# 1. /media/nfs-recordings/20251217 1219 - ORF1 HD - Monk.ts (2.9GB) ✅ TESTED
# 2. (Need to find more or recover from trash)
```

---

## Phase 1: Recording Inventory (15 min)

### Task 1.1: Find Additional Recordings

**Option A**: Check other directories
```bash
# Search for TS files in other common locations
find /media -name "*.ts" -type f -size +500M 2>/dev/null | grep -v -i trash | head -20

# Check receiver's local storage (if accessible)
ssh root@10.10.55.64 "find /media -name '*.ts' -size +500M 2>/dev/null | head -20"
```

**Option B**: Recover from trash (if safe)
```bash
# Check trash contents
ls -lh /media/nfs-recordings/.Trash/*.ts 2>/dev/null

# Known file:
# /media/nfs-recordings/.Trash/20251217 0957 - ORF2N HD - Schlosshotel Orth_001.ts
# → Different channel (ORF2N HD vs ORF1 HD) = good for diversity

# If safe, temporarily copy out (DO NOT MOVE - keep backup)
cp "/media/nfs-recordings/.Trash/20251217 0957 - ORF2N HD - Schlosshotel Orth_001.ts" \
   "/media/nfs-recordings/TEST_ORF2N_HD_validation.ts"
```

**Option C**: Create new recordings
```bash
# Trigger 3 new recordings on receiver (different channels):
# 1. ORF2 HD (or ServusTV HD) - 30min recording
# 2. ARD HD or ZDF HD - 30min recording
# 3. Any channel - 2h+ recording (for long-duration test)

# Wait for recordings to complete, then proceed with validation
```

**Deliverable**: List of 3+ TS files for testing

---

## Phase 2: Core Validation Tests (2-3 hours)

### Test 2.1: ORF1 HD (Baseline - Already Validated)

**File**: `/media/nfs-recordings/20251217 1219 - ORF1 HD - Monk.ts`
**Status**: ✅ **PASS** (0.01% duration delta, seek works, Chrome playback confirmed)

**Result from previous test**:
- Source: 2424.70s, H.264 yuv420p, AC3 5.1
- Output: 2424.40s (delta: -0.30s = 0.01%)
- Warnings: PES/corrupt (non-fatal, correctly classified)

**Action**: Skip (already validated) or re-run for consistency check.

---

### Test 2.2: Additional Source #1 (Different Channel/Transponder)

**File**: `[FILL IN FROM PHASE 1]`

**Execution**:
```bash
# Step 1: Probe source
SOURCE="/media/nfs-recordings/TEST_FILE.ts"
ffprobe -v quiet -show_streams -print_format json "$SOURCE" > /tmp/test2_probe.json

# Extract key info
jq -r '.streams[] | select(.codec_type=="video") | "\(.codec_name) \(.pix_fmt) \(.width)x\(.height)"' /tmp/test2_probe.json
jq -r '.streams[] | select(.codec_type=="audio") | "\(.codec_name) \(.channels)ch \(.sample_rate)Hz"' /tmp/test2_probe.json

# Step 2: Trigger MP4 remux via xg2g API
# (Assumes recording is in xg2g database - if not, import first)

# Get recording ID
RECORDING_ID="[GET FROM xg2g WebUI or API]"

# Trigger MP4 build (via curl to xg2g API)
curl -v "http://localhost:8080/api/v3/recordings/${RECORDING_ID}/vod/recording.mp4" \
     -o /tmp/test2_output.mp4 2>&1 | tee /tmp/test2_api.log

# Step 3: Collect artifacts
cp "/root/xg2g/data/cache/v3-recordings/${RECORDING_ID}/vod/recording.mp4.meta.json" /tmp/test2_meta.json 2>/dev/null || true
cp "/root/xg2g/data/cache/v3-recordings/${RECORDING_ID}/vod/recording.mp4.err.log" /tmp/test2_err.log 2>/dev/null || true

# Step 4: Validate output
DURATION_SRC=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$SOURCE")
DURATION_OUT=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 /tmp/test2_output.mp4)

echo "Source duration: ${DURATION_SRC}s"
echo "Output duration: ${DURATION_OUT}s"
python3 -c "print(f'Delta: {abs(float('$DURATION_SRC') - float('$DURATION_OUT')):.2f}s ({abs(float('$DURATION_SRC') - float('$DURATION_OUT')) / float('$DURATION_SRC') * 100:.2f}%)')"

# Step 5: Seek test (5 random positions)
for POS in 0 25 50 75 100; do
    SEEK_TIME=$(python3 -c "print(int(float('$DURATION_OUT') * $POS / 100))")
    ffmpeg -ss $SEEK_TIME -i /tmp/test2_output.mp4 -frames:v 1 -f null - 2>&1 | grep -q "frame=.*1" && echo "✅ Seek ${POS}% works" || echo "❌ Seek ${POS}% FAILED"
done
```

**Pass Criteria**:
- ✅ Duration delta < 1%
- ✅ Seek works at all 5 positions
- ✅ `.meta.json` exists with correct strategy
- ✅ No `.err.log` OR `.err.log` contains only non-fatal warnings

**Failure Handling**:
- If DEFAULT fails with DTS errors → ✅ **GOOD** (validates fallback path)
- If HEVC source → ✅ **GOOD** (validates transcode path)
- Collect `.err.log` for pattern analysis

---

### Test 2.3: Long-Duration Recording (≥2 hours)

**File**: `[FILL IN FROM PHASE 1]`

**Purpose**: Validate timeout logic (20min + 1min/GB, max 2h) and cache/IO under sustained load.

**Execution**: Same as Test 2.2, but monitor:
```bash
# Monitor during execution
watch -n 5 "ps aux | grep ffmpeg; df -h /root/xg2g/data/cache"

# Check timeout wasn't hit prematurely
grep -i timeout /tmp/test3_err.log || echo "No timeout issues"
```

**Pass Criteria**:
- ✅ Remux completes within expected timeout
- ✅ No stall detection triggered (check metric: `xg2g_vod_remux_stalls_total`)
- ✅ Cache space sufficient (no ENOSPC errors)

---

### Test 2.4: Force Fallback Path (Manual)

**Purpose**: Validate `buildFallbackRemuxArgs()` if no natural DTS errors occur.

**Option A**: Find recording with DTS issues
```bash
# Test all available recordings for DTS errors
for f in /media/nfs-recordings/*.ts; do
    ffmpeg -y -i "$f" -c:v copy -c:a aac -f null - 2>&1 | grep -i "non-monotonous" && echo "✅ DTS issue in: $f"
done
```

**Option B**: Force fallback via test mode (if implemented)
```bash
# Temporarily patch code to force fallback
# (Or use environment variable if added: FORCE_REMUX_STRATEGY=fallback)
```

**Option C**: Defer to production monitoring
- Accept that fallback is **structurally correct** but untested
- Collect from first 10-20 production remuxes
- Risk: LOW (fallback flags are industry-standard)

---

## Phase 3: Stall Detection Test (30 min) **MANDATORY**

### Test 3.1: Create fake-ffmpeg

```bash
# Create fake binary that simulates stall
cat > /tmp/fake-ffmpeg <<'EOF'
#!/bin/bash
# Simulate ffmpeg startup + progress, then stall

# Write initial progress (simulates normal startup)
echo "frame=1" >&1
echo "fps=0.0" >&1
echo "stream_0_0_q=0.0" >&1
echo "bitrate=0.0kbits/s" >&1
echo "total_size=0" >&1
echo "out_time_us=0" >&1
echo "out_time_ms=0" >&1
echo "out_time=00:00:00.000000" >&1
echo "dup_frames=0" >&1
echo "drop_frames=0" >&1
echo "speed=0.0x" >&1
echo "progress=continue" >&1

sleep 1

# Write one update (to pass grace period)
echo "frame=50" >&1
echo "out_time_us=2000000" >&1
echo "total_size=1048576" >&1
echo "speed=1.0x" >&1
echo "progress=continue" >&1

# NOW STALL (no more progress output)
echo "Simulating stall: sleeping for 5 minutes (watchdog should kill before this)" >&2
sleep 300

# Should never reach here
echo "progress=end" >&1
exit 0
EOF

chmod +x /tmp/fake-ffmpeg

# Verify fake-ffmpeg works
timeout 5 /tmp/fake-ffmpeg | head -20
```

### Test 3.2: Trigger stall via xg2g (Requires Code Modification)

**Challenge**: xg2g hardcodes ffmpeg path (`/usr/bin/ffmpeg`).

**Option A**: Temporarily replace ffmpeg binary (DANGEROUS - backup first!)
```bash
# BACKUP ORIGINAL
sudo cp /usr/bin/ffmpeg /usr/bin/ffmpeg.real

# REPLACE (TEMPORARY - RESTORE AFTER TEST!)
sudo cp /tmp/fake-ffmpeg /usr/bin/ffmpeg

# Trigger MP4 build
curl "http://localhost:8080/api/v3/recordings/TEST_RECORDING/vod/recording.mp4" \
     -o /tmp/stall_test_output.mp4

# RESTORE ORIGINAL IMMEDIATELY
sudo mv /usr/bin/ffmpeg.real /usr/bin/ffmpeg
```

**Option B**: Add config parameter `ffmpeg_binary_path` (RECOMMENDED)

```yaml
# Add to config.yaml (requires code change in recordings_remux.go)
api:
  recordings:
    ffmpeg_binary_path: /tmp/fake-ffmpeg  # Default: /usr/bin/ffmpeg
```

Then rebuild and test.

**Option C**: Docker override (if xg2g runs in container)
```bash
# Mount fake-ffmpeg over real ffmpeg
docker run -v /tmp/fake-ffmpeg:/usr/bin/ffmpeg:ro ...
```

### Test 3.3: Validate Stall Behavior

**Expected**:
1. ✅ Process killed after **90 seconds** (not 2 hours)
2. ✅ Metric incremented: `xg2g_vod_remux_stalls_total{strategy="default"}` = 1
3. ✅ Log message contains:
   ```
   "msg":"vod remux stalled - killing ffmpeg"
   "since_progress":"90.XXXs"
   "last_out_time_us":"2000000"
   ```
4. ✅ `.err.log` contains: `ErrFFmpegStalled: no progress for 1m30s`
5. ✅ Semaphore released (next request doesn't block)

**Verification**:
```bash
# Check logs
journalctl -u xg2g -n 100 | grep -i stall

# Check metric (via Prometheus or internal endpoint)
curl -s http://localhost:9090/metrics | grep xg2g_vod_remux_stalls_total

# Check semaphore (no stuck .lock files)
ls -l /root/xg2g/data/cache/v3-recordings/*/vod/*.lock 2>/dev/null
```

**Pass Criteria**:
- ✅ All 5 expected behaviors confirmed
- ✅ No hung processes (`ps aux | grep ffmpeg` shows nothing after 2min)

---

## Phase 4: Metrics & Telemetry Check (15 min)

### Test 4.1: Verify Prometheus Metrics

```bash
# If Prometheus is running locally
curl -s http://localhost:9090/metrics | grep xg2g_vod

# Expected metrics:
# xg2g_vod_remux_stalls_total{strategy="default"} 0  (or 1 if stall test ran)
# xg2g_vod_builds_rejected_total{...} (if any failures)
```

### Test 4.2: Collect Operator Artifacts

```bash
# Find all .meta.json files
find /root/xg2g/data/cache/v3-recordings -name "*.meta.json" -type f -mtime -1

# Find all .err.log files
find /root/xg2g/data/cache/v3-recordings -name "*.err.log" -type f -mtime -1

# Collect for analysis
mkdir -p /tmp/validation_artifacts
cp /root/xg2g/data/cache/v3-recordings/**/vod/*.meta.json /tmp/validation_artifacts/ 2>/dev/null || true
cp /root/xg2g/data/cache/v3-recordings/**/vod/*.err.log /tmp/validation_artifacts/ 2>/dev/null || true

# Review
ls -lh /tmp/validation_artifacts/
cat /tmp/validation_artifacts/*.meta.json | jq .
```

---

## Phase 5: Rollback Plan Verification (10 min)

### Test 5.1: Verify Kill Switch Works

```bash
# Edit config to disable MP4 remux
cd /root/xg2g
vi data/config.yaml

# Add (if not present):
# api:
#   recordings:
#     direct_mp4_enabled: false

# Restart xg2g
systemctl restart xg2g 2>/dev/null || docker restart xg2g || killall -HUP xg2g

# Test: MP4 request should fallback to HLS
curl -I "http://localhost:8080/api/v3/recordings/TEST_RECORDING/vod/recording.mp4"
# Expected: 200 OK, but Content-Type: application/vnd.apple.mpegurl (HLS fallback)

# Re-enable
# api:
#   recordings:
#     direct_mp4_enabled: true

# Restart again
systemctl restart xg2g
```

**Pass Criteria**:
- ✅ Config change takes effect without code rebuild
- ✅ HLS fallback works when MP4 disabled
- ✅ Restart time < 30 seconds

---

## Summary: Pass/Fail Criteria

### MANDATORY (Production Blockers)

| Test | Criteria | Status |
|------|----------|--------|
| **ORF1 HD Baseline** | Duration delta < 1%, seek works | ✅ PASS |
| **Stall Detection** | Kills after 90s, metric increments | ⬜ TODO |
| **Rollback Plan** | Config toggle works, HLS fallback | ⬜ TODO |

### RECOMMENDED (Risk Reduction)

| Test | Criteria | Status | Risk if Skipped |
|------|----------|--------|-----------------|
| **N≥3 Diversity** | 2+ additional sources tested | ⬜ TODO | MEDIUM (limited validation) |
| **Long-Duration** | ≥2h recording completes | ⬜ TODO | LOW (timeout logic sound) |
| **Fallback Path** | DTS error triggers fallback | ⬜ TODO | LOW (flags are standard) |
| **Transcode Path** | HEVC triggers transcode | ⬜ TODO | LOW (no HEVC expected) |

---

## Realistic Outcome Assessment

### Scenario A: Only 2 Recordings Available (Current State)

**Tests You CAN Execute**:
1. ✅ ORF1 HD (already done)
2. ✅ Stall detection (fake-ffmpeg - independent)
3. ✅ Rollback plan
4. ⚠️ ORF2N HD from trash (if safe to recover)

**Result**: **N=2** (not N≥3)

**Risk Level**: **MEDIUM** (acceptable for staging, not ideal for production)

**Recommendation**:
- Deploy to **staging** with Go-Live monitoring
- Collect N≥3 **organically** from first 10-20 production remuxes
- Review telemetry after 1 week, adjust if needed

---

### Scenario B: Additional Recordings Created/Found

**Tests You CAN Execute**:
1. ✅ Full N≥3 validation
2. ✅ Long-duration test
3. ✅ Stall detection
4. ✅ Rollback plan

**Result**: **N≥3 COMPLETE**

**Risk Level**: **LOW** → **production-ready without reservations**

---

## Go-Live Monitoring Plan (First 48-72h)

**If deploying with N=2**:

### Monitor These Metrics

```promql
# Stall rate (should be <1%)
rate(xg2g_vod_remux_stalls_total[5m])

# Error rate (should be <10%)
rate(xg2g_vod_builds_rejected_total[5m])

# Success rate (should be >90%)
rate(xg2g_vod_builds_success_total[5m])
```

### Collect These Artifacts

```bash
# First 20 remuxes: collect .meta.json (codec distribution)
find /root/xg2g/data/cache/v3-recordings -name "*.meta.json" -type f -mtime -3 | head -20 | xargs cat | jq -s '.'

# All errors: collect .err.log (pattern validation)
find /root/xg2g/data/cache/v3-recordings -name "*.err.log" -type f -mtime -3 | xargs cat > /tmp/all_errors.log

# Analyze error patterns
grep -oE "(Non-monotonous|Invalid duration|PES packet|Packet corrupt)" /tmp/all_errors.log | sort | uniq -c
```

### Rollback Trigger

**IF any of**:
- Stall rate > 5%
- Error rate > 20%
- User complaints about playback (>10% of requests)

**THEN**:
```yaml
# config.yaml
api:
  recordings:
    direct_mp4_enabled: false  # Immediate rollback to HLS
```

---

## Bottom Line

**What's Actually Possible Today**:
- ✅ Stall detection test (fake-ffmpeg) - **MANDATORY**
- ✅ Rollback plan test - **MANDATORY**
- ⚠️ N=2 validation (ORF1 HD + ORF2N HD recovery) - **if trash file is safe**

**What's NOT Possible Without More Recordings**:
- ❌ Full N≥3 diversity
- ❌ Long-duration test (no 2h+ files)
- ❌ Natural fallback/transcode triggers (no DTS/HEVC sources)

**Pragmatic Deployment Path**:
1. Execute mandatory tests (stall + rollback)
2. Execute N=2 if possible (ORF2N HD recovery)
3. Deploy to **staging** with monitoring
4. Collect N≥3 organically from production
5. Review after 1 week, adjust if needed

**Risk**: MEDIUM (acceptable for controlled rollout with monitoring + rollback plan)

---

## Next Actions

**Immediate (Today)**:
1. Execute stall test (30 min) → **MANDATORY**
2. Execute rollback test (10 min) → **MANDATORY**
3. Decide: recover ORF2N HD from trash? (safe? worth it?)

**Short-Term (This Week)**:
4. Create 2-3 new recordings on receiver (different channels, 1 long-duration)
5. Re-run validation checklist with N≥3
6. Update FINAL_AUDIT_REPORT.md with results

**Go-Live (Next Week)**:
7. Deploy to staging
8. Monitor for 48-72h
9. Collect telemetry
10. Adjust patterns/policies based on real data

---

**Status**: Ready for controlled staging deployment with mandatory tests + monitoring. Full N≥3 to follow.
