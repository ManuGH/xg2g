# Production Test Checklist: Patch 1 DVR Windowing

**Date**: 2026-01-05
**Patch**: DVR Windowing & Disk Trimming
**Duration**: 15 minutes active monitoring + 3h passive observation
**Risk Level**: Low (rollback ready)

---

## Pre-Flight Checks

### 1. Verify Build Artifacts

```bash
# Ensure binary was built with Patch 1
/tmp/xg2g --version
# Expected: Should show recent build timestamp

# Verify runner.go contains new segment duration logic
grep -A 5 "Patch 1: Segment Duration Policy" internal/pipeline/exec/ffmpeg/runner.go
# Expected: segmentDuration := 6

# Verify args.go contains validation logic
grep -A 5 "Patch 1: Ensure segment duration" internal/pipeline/exec/ffmpeg/args.go
# Expected: if segDur <= 0 { segDur = 6 }
```

### 2. Backup Current State

```bash
# Backup current binary (if deployed)
sudo cp /usr/local/bin/xg2g /usr/local/bin/xg2g.pre-patch1

# Note current session count
curl -s http://localhost:8080/api/v3/sessions | jq 'length'
# Expected: 0 (clean start preferred)
```

### 3. Deploy New Binary

```bash
# Option A: Direct replacement (single-host test)
sudo cp /tmp/xg2g /usr/local/bin/xg2g
sudo systemctl restart xg2g

# Option B: Canary (if you have multiple workers)
# Deploy to dedicated canary host only

# Verify startup
sudo journalctl -u xg2g -n 50 --no-pager | grep "server started"
# Expected: "server started" message within 5 seconds
```

---

## Phase 1: Immediate Validation (First 15 Minutes)

### Test 1: Start DVR Stream

**Duration**: 5 minutes

```bash
# 1.1. Start stream (default 3h DVR)
sessionID=$(curl -sX POST http://localhost:8080/api/v3/intents \
  -H "Content-Type: application/json" \
  -d '{
    "type": "stream.start",
    "profileID": "safari",
    "serviceRef": "1:0:1:445D:453:1:C00000:0:0:0:"
  }' | jq -r '.sessionID')

echo "Session ID: $sessionID"

# 1.2. Wait for READY state (should be < 30s)
for i in {1..30}; do
  state=$(curl -s "http://localhost:8080/api/v3/sessions/$sessionID" | jq -r '.state')
  echo "Attempt $i: State = $state"
  if [ "$state" = "READY" ]; then
    echo "✅ Stream READY"
    break
  fi
  sleep 1
done

# 1.3. Verify logs show new segment duration
sudo journalctl -u xg2g --since "1 minute ago" | grep "hls playlist configuration"
# Expected output example:
# "dvr_window_sec":10800,"segment_duration":6,"calculated_playlist_size":1800,"vod_mode":false,"llhls":false
```

**Success Criteria**:
- ✅ State transitions to `READY` within 30s
- ✅ Log shows `segment_duration":6`
- ✅ Log shows `calculated_playlist_size":1800`

**Failure Handling**:
```bash
# If stream fails to start:
sudo journalctl -u xg2g --since "2 minutes ago" | grep ERROR
# Check for FFmpeg errors, then rollback:
sudo systemctl stop xg2g
sudo cp /usr/local/bin/xg2g.pre-patch1 /usr/local/bin/xg2g
sudo systemctl start xg2g
```

---

### Test 2: Verify Playlist Contract

**Duration**: 3 minutes

```bash
# 2.1. Wait 60 seconds for playlist to populate
sleep 60

# 2.2. Fetch playlist
curl -s "http://localhost:8080/api/v3/hls/$sessionID/index.m3u8" > /tmp/playlist_t1.m3u8

# 2.3. Check segment count (should be ~10 after 60s)
segments=$(grep -c '^#EXTINF:' /tmp/playlist_t1.m3u8)
echo "Segment count: $segments (expected ~10 for 60s @ 6s/segment)"

# 2.4. Verify playlist type
grep '#EXT-X-PLAYLIST-TYPE:EVENT' /tmp/playlist_t1.m3u8
# Expected: Found

# 2.5. Verify program date time
grep '#EXT-X-PROGRAM-DATE-TIME:' /tmp/playlist_t1.m3u8 | head -1
# Expected: ISO 8601 timestamp (e.g., 2026-01-05T14:23:45.123Z)

# 2.6. Verify DVR hint (server-injected)
grep '#EXT-X-START:TIME-OFFSET=-10800' /tmp/playlist_t1.m3u8
# Expected: Found

# 2.7. Verify NO endlist
grep '#EXT-X-ENDLIST' /tmp/playlist_t1.m3u8
# Expected: NOT found (exit code 1)

# 2.8. Check target duration
grep '#EXT-X-TARGETDURATION:' /tmp/playlist_t1.m3u8
# Expected: #EXT-X-TARGETDURATION:6 (or 7 for rounding)
```

**Success Criteria**:
- ✅ Segment count between 8-12 (60s ± jitter)
- ✅ Playlist type = EVENT
- ✅ PDT present
- ✅ START offset = -10800
- ✅ NO ENDLIST tag

**Red Flags**:
- ❌ Segment count = 5 → Old behavior (FFmpeg using default hls_list_size)
- ❌ Missing PDT → Flags not applied correctly
- ❌ TARGETDURATION:2 → Segment duration not applied

---

### Test 3: Verify Disk Behavior

**Duration**: 5 minutes

```bash
# 3.1. Get session directory
sessionDir="/var/lib/xg2g/sessions/$sessionID"
ls -la "$sessionDir"

# 3.2. Count segment files
segmentCount=$(ls "$sessionDir"/seg_*.m4s 2>/dev/null | wc -l)
echo "Disk segment count: $segmentCount (expected ~10-15 after 60s)"

# 3.3. Monitor file growth over 3 minutes
echo "Monitoring segment count every 30s for 3 minutes..."
for i in {1..6}; do
  count=$(ls "$sessionDir"/seg_*.m4s 2>/dev/null | wc -l)
  timestamp=$(date +%H:%M:%S)
  echo "[$timestamp] Segments on disk: $count"
  sleep 30
done

# 3.4. Verify playlist size
playlistSize=$(stat -f%z "$sessionDir/index.m3u8" 2>/dev/null || stat -c%s "$sessionDir/index.m3u8")
echo "Playlist size: $playlistSize bytes (expected < 10 KB for ~30 segments)"
```

**Success Criteria**:
- ✅ Segment count grows linearly (~5 new segments per 30s)
- ✅ Playlist size < 10 KB after 3 minutes
- ✅ No error files (e.g., `.err`, `.log` artifacts)

**Red Flags**:
- ❌ Segment count explodes (> 100 after 3 min) → Old 2s duration used
- ❌ Playlist size > 50 KB → Possible line buffering issue

---

### Test 4: Safari UX Spot Check

**Duration**: 2 minutes (manual)

**Prerequisites**: macOS/iOS device with Safari

```bash
# 4.1. Open player URL
echo "Open this URL in Safari:"
echo "http://$(hostname -I | awk '{print $1}'):8080/?sref=1:0:1:445D:453:1:C00000:0:0:0:&profile=safari"

# 4.2. Manual validation steps:
# - Video plays inline (not fullscreen takeover) ✅
# - Enter fullscreen (tap fullscreen button)
# - Verify DVR timeline appears (scrubber at bottom) ✅
# - Drag scrubber backwards 30-60 seconds ✅
# - Verify playback continues from seek point ✅
# - Exit fullscreen (tap done)
# - Verify returns to inline mode ✅
```

**Success Criteria**:
- ✅ Inline playback works
- ✅ Fullscreen DVR timeline visible
- ✅ Scrubbing backwards works (no freeze/jump to live)

**Red Flags**:
- ❌ Scrubber jumps to live edge → Playlist window too small
- ❌ Video freezes after seek → Segment boundary issue

---

## Phase 2: Extended Observation (3 Hours Passive)

### Background Monitor Script

```bash
#!/bin/bash
# Save as /tmp/monitor_dvr.sh

sessionID="$1"
sessionDir="/var/lib/xg2g/sessions/$sessionID"
logFile="/tmp/dvr_monitor_$sessionID.log"

echo "Monitoring session $sessionID for 3 hours..." | tee "$logFile"
echo "Session directory: $sessionDir" | tee -a "$logFile"
echo "Start time: $(date)" | tee -a "$logFile"
echo "" | tee -a "$logFile"

# Monitor every 5 minutes for 3 hours (36 samples)
for i in {1..36}; do
  timestamp=$(date +%H:%M:%S)

  # Disk metrics
  segCount=$(ls "$sessionDir"/seg_*.m4s 2>/dev/null | wc -l)
  playlistSize=$(stat -c%s "$sessionDir/index.m3u8" 2>/dev/null)

  # Playlist metrics
  playlistSegCount=$(curl -s "http://localhost:8080/api/v3/hls/$sessionID/index.m3u8" | grep -c '^#EXTINF:')

  # Session state
  state=$(curl -s "http://localhost:8080/api/v3/sessions/$sessionID" | jq -r '.state')

  echo "[$timestamp] State=$state | Disk_Segments=$segCount | Playlist_Segments=$playlistSegCount | Playlist_Size=$playlistSize bytes" | tee -a "$logFile"

  # Alert on anomalies
  if [ "$segCount" -gt 2000 ]; then
    echo "⚠️ WARNING: Segment count exceeded 2000 (disk explosion?)" | tee -a "$logFile"
  fi

  if [ "$playlistSize" -gt 100000 ]; then
    echo "⚠️ WARNING: Playlist size exceeded 100 KB (trimming issue?)" | tee -a "$logFile"
  fi

  sleep 300  # 5 minutes
done

echo "" | tee -a "$logFile"
echo "End time: $(date)" | tee -a "$logFile"
echo "Monitor complete. Check $logFile for full history." | tee -a "$logFile"
```

**Usage**:
```bash
# Start background monitor
chmod +x /tmp/monitor_dvr.sh
nohup /tmp/monitor_dvr.sh "$sessionID" > /dev/null 2>&1 &

# Check progress
tail -f /tmp/dvr_monitor_$sessionID.log
```

**Expected Behavior Over 3 Hours**:
| Time | Disk Segments | Playlist Segments | Playlist Size |
|------|--------------|------------------|---------------|
| 5 min | ~50 | ~50 | ~2 KB |
| 30 min | ~300 | ~300 | ~15 KB |
| 1 hour | ~600 | ~600 | ~30 KB |
| 3 hours | **~1800** | **~1800** | **~90 KB** |
| 4 hours | **~1800** | **~1800** | **~90 KB** (stabilized!) |

**Success Criteria**:
- ✅ Segment count stabilizes at ~1800 after 3 hours
- ✅ Playlist size stabilizes at ~90 KB
- ✅ NO continuous growth beyond DVR window
- ✅ State remains `READY` throughout

**Red Flags**:
- ❌ Segment count > 2000 after 3h → Trimming not working
- ❌ Playlist size > 100 KB → Possible memory leak in playlist generation
- ❌ State changes to `FAILED` → Check logs for FFmpeg crash

---

## Post-Test Analysis

### 1. Final Playlist Inspection

```bash
# After 3+ hours, fetch final playlist
curl -s "http://localhost:8080/api/v3/hls/$sessionID/index.m3u8" > /tmp/playlist_final.m3u8

# Count segments
grep -c '^#EXTINF:' /tmp/playlist_final.m3u8
# Expected: ~1800 (±50 for edge cases)

# Check oldest segment timestamp (should be ~3h ago)
firstPDT=$(grep '#EXT-X-PROGRAM-DATE-TIME:' /tmp/playlist_final.m3u8 | head -1 | cut -d: -f2-)
echo "Oldest segment timestamp: $firstPDT"
# Compare to current time - should be ~10800s (3h) difference

# Verify no gaps (all segment IDs sequential)
grep '^seg_' /tmp/playlist_final.m3u8 | sort -t_ -k2 -n > /tmp/segments_sorted.txt
# Manual inspection: no missing IDs in sequence
```

### 2. Disk Cleanup Verification

```bash
# List all segment files with timestamps
ls -lt "$sessionDir"/seg_*.m4s | head -20
# Expected: Newest ~20 segments exist

ls -lt "$sessionDir"/seg_*.m4s | tail -20
# Expected: Oldest ~20 segments (should be ~3h old, not older)

# Verify total file count
ls "$sessionDir"/seg_*.m4s | wc -l
# Expected: ~1800 (±10%)
```

### 3. Resource Usage

```bash
# Check FFmpeg CPU usage
ps aux | grep ffmpeg | grep -v grep
# Expected: CPU ~10-50% (depends on transcoding profile)

# Check disk I/O wait
iostat -x 1 3 | grep sda
# Expected: %util < 80% (no I/O saturation)

# Check memory usage
free -h
# Expected: No significant leak (compare to baseline before test)
```

---

## Go/No-Go Decision Matrix

### ✅ GO: Proceed to Full Rollout

**All conditions met**:
- [x] Segment duration = 6s (verified in logs)
- [x] Playlist size stabilizes at ~1800 segments
- [x] Disk segments capped (no growth beyond window)
- [x] Safari DVR scrubber functional
- [x] No FFmpeg crashes or state transitions to FAILED
- [x] Resource usage stable (CPU, memory, I/O)

**Action**: Deploy to all production hosts

---

### ⚠️ CONDITIONAL: Requires Tuning

**Symptoms**:
- Segment count correct but Safari scrubber laggy → Consider 4s segments for LL-HLS
- Disk I/O high → Investigate filesystem (ext4 vs xfs) or storage backend
- Playlist parse time > 50ms → Add caching layer (future optimization)

**Action**: Document findings, create tuning tickets, proceed with caution

---

### 🛑 NO-GO: Rollback Required

**Critical failures**:
- Segment count unbounded (grows beyond 2000)
- FFmpeg crashes with VAAPI errors
- Safari playback broken (freeze/stutter)
- Disk I/O saturation (>90% util)

**Immediate Action**:
```bash
# Stop all streams
curl -X POST http://localhost:8080/api/v3/sessions/stop-all

# Rollback binary
sudo systemctl stop xg2g
sudo cp /usr/local/bin/xg2g.pre-patch1 /usr/local/bin/xg2g
sudo systemctl start xg2g

# Verify rollback
grep -A 5 "DVR / VOD Config" internal/pipeline/exec/ffmpeg/runner.go
# Should show old code (segmentDuration := 2)

# Report issue
echo "Patch 1 rollback: $(date)" >> /var/log/xg2g/rollback.log
```

---

## Next Steps After Successful Test

### 1. Document Production Metrics

```bash
# Create baseline metrics file
cat > /tmp/patch1_baseline_metrics.txt <<EOF
Session ID: $sessionID
Test Duration: 3 hours
Segment Duration: 6s
Final Playlist Size: $(stat -c%s "$sessionDir/index.m3u8") bytes
Final Segment Count: $(ls "$sessionDir"/seg_*.m4s | wc -l)
CPU Avg: $(ps aux | grep ffmpeg | grep -v grep | awk '{print $3}')%
Memory: $(free -h | grep Mem | awk '{print $3}')
Disk I/O Avg: $(iostat -x 1 3 | grep sda | awk '{sum+=$14} END {print sum/3}')%
EOF

cat /tmp/patch1_baseline_metrics.txt
```

### 2. Enable Monitoring Alerts (if not already)

```yaml
# Prometheus alert example
- alert: DVRPlaylistOverflow
  expr: count(up{job="xg2g"}) > 0 and (disk_segment_count > 2100)
  for: 10m
  annotations:
    summary: "Playlist exceeds expected window (trimming failure)"

- alert: DiskIOSaturation
  expr: rate(node_disk_io_time_seconds_total[5m]) > 0.9
  for: 5m
  annotations:
    summary: "Disk I/O saturated (possible segment explosion)"
```

### 3. Proceed to Phase 2: Golden Tests

**Once production validation passes**:
- Implement Manifest Contract Tests
- Add Integration Test with real FFmpeg
- Document expected playlist structure
- Freeze behavior for regression prevention

**See**: [Next: Implement Golden Tests Phase 2]

---

## Troubleshooting Guide

### Issue 1: Segment Count Not Stabilizing

**Symptom**: Segment count grows beyond 2000 after 3h

**Diagnosis**:
```bash
# Check FFmpeg args actually used
sudo journalctl -u xg2g --since "3 hours ago" | grep "ffmpeg.*-hls_list_size"
# Expected: -hls_list_size 1800

# If missing or wrong value:
# 1. Verify runner.go calculation
grep "calculated_playlist_size" /var/log/xg2g/xg2g.log
# 2. Check args.go validation
grep "Patch 1: Playlist size" internal/pipeline/exec/ffmpeg/args.go
```

**Fix**: Ensure `playlistSize` calculation correct in runner.go

---

### Issue 2: Safari Scrubber Jumps to Live

**Symptom**: Dragging scrubber backwards always returns to live edge

**Diagnosis**:
```bash
# Check playlist window vs DVR offset
curl -s "http://localhost:8080/api/v3/hls/$sessionID/index.m3u8" > /tmp/test.m3u8
grep '#EXT-X-START:TIME-OFFSET=' /tmp/test.m3u8
# Expected: TIME-OFFSET=-10800

# Calculate actual window
segments=$(grep -c '^#EXTINF:' /tmp/test.m3u8)
window=$((segments * 6))  # 6s per segment
echo "Actual window: ${window}s (expected 10800s)"
```

**Fix**: If window < offset, playlist window calculation is wrong

---

### Issue 3: High Disk I/O Wait

**Symptom**: Server becomes unresponsive during peak

**Diagnosis**:
```bash
# Check concurrent sessions
curl -s http://localhost:8080/api/v3/sessions | jq 'length'
# If > 50: Disk I/O bottleneck likely

# Check write amplification
iostat -x 1 10 | grep sda
# High w/s (writes per second) = segment churn
```

**Fix**:
- Consider increasing segment duration to 8s (reduces write frequency)
- Upgrade to SSD if using HDD
- Implement segment pooling (future optimization)

---

## Summary

✅ **15-Minute Active Test**: Validates immediate correctness
✅ **3-Hour Passive Test**: Validates long-term stability
✅ **Go/No-Go Matrix**: Clear decision criteria
✅ **Rollback Plan**: Immediate recovery if needed

**Key Insight**: Production validation catches operational issues that unit tests cannot (I/O patterns, memory pressure, Safari UX).

**After Success**: Proceed to Phase 2 Golden Tests to lock in correct behavior.
