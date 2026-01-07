# Technical Review: TS→MP4 Remux + HLS Policy (xg2g Recordings VOD)

**Status:** Pending Review
**Date:** 2026-01-03
**Reviewer:** [TBD]
**Context:** Enigma2/NAS TS recordings → Browser playback design validation

---

## 0) Context / Target Design

**Source:** Enigma2 records to NAS as `.ts` files (Transport Stream, typically H.264/H.265 + AC3/AAC/MP2)

**Browser Playback Requirements:**
- Stable duration (Continue Watching/Resume functionality)
- Clean seeking behavior
- Low server CPU overhead
- Robust against "typical TS problems" (PCR/PTS issues, discontinuities, corrupt segments)

---

## 1) Current Strategy (Under Review)

### A) Live/Progressive Playback (during build / not yet finished)

**HLS Build via ffmpeg with TS segments (TS-only Policy):**
```go
// Current implementation: internal/api/recordings.go:1687-1742
segmentPattern := ffmpegexec.SegmentPattern(cacheDir, ".ts")
args := []string{
    "-nostdin", "-hide_banner", "-loglevel", "error",
    "-ignore_unknown",
    "-fflags", "+genpts+discardcorrupt",
    "-err_detect", "ignore_err",
    "-probesize", probeSize, "-analyzeduration", analyzeDur,
    // ... input handling (concat or direct)
    "-map", "0:v:0?", "-map", "0:a:0?",
    "-sn", "-dn",  // Drop subtitles and data streams
}

// Transcode mode:
if transcode {
    args = append(args,
        "-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
        "-x264-params", "keyint=100:min-keyint=100:scenecut=0",
        "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-profile:a", "aac_low",
        "-filter:a", "aresample=async=1:first_pts=0,aformat=channel_layouts=stereo",
    )
} else {
    args = append(args,
        "-c:v", "copy",
        "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-profile:a", "aac_low",
        "-filter:a", "aresample=async=1:first_pts=0,aformat=channel_layouts=stereo",
    )
}

args = append(args,
    "-f", "hls", "-hls_time", "6", "-hls_list_size", "0",
    "-hls_flags", hlsFlags,  // "append_list+temp_file" or "independent_segments+..."
    "-hls_segment_filename", segmentPattern,
    livePlaylist,  // index.live.m3u8
    "-progress", "pipe:1",
)
```

**Segment Policy:**
- Strict allow-list: **only** `seg_*.ts` (no `init.mp4`, no `.m4s`/`.cmfv`)
- Playlist ready check: `index.live.m3u8` + at least 1 valid segment exists

### B) Finished Playback (completed recording)

**When local file present and "stable" (not growing):**

**Direct MP4 via Remux Job** (cached):
```go
// Current remux strategy (conceptual - actual implementation may vary)
// Goal: Fast remux with -c:v copy, audio always transcoded to AAC
ffmpeg -y \
    -i input.ts \
    -c:v copy \
    -c:a aac -b:a 192k -ar 48000 -profile:a aac_low \
    -movflags +faststart \
    -sn -dn \
    stream.mp4
```

**Concurrency Control:**
- Lock file → `503 Service Unavailable` + `Retry-After`
- Semaphore full → `429 Too Many Requests` + `Retry-After`

---

## 2) Review Questions (Concrete Answers Required)

### 2.1 Container/Codec Compatibility

#### Q1: Is "TS → MP4 Remux" with `-c:v copy` fundamentally correct for Enigma2 TS?

**Action Required:**
- Validate typical Enigma2 video codecs (H.264/H.265) are MP4-compatible
- Identify H.264/H.265 profiles/levels that typically cause problems in MP4:
  - Specific profiles (High 4:2:2, High 10 Intra, etc.)?
  - Level constraints (5.1+, 6.0+)?
  - Non-standard parameter sets?

**Test Command:**
```bash
ffprobe -v error -select_streams v:0 \
    -show_entries stream=codec_name,profile,level,pix_fmt,extradata_size \
    -of json input.ts
```

**Failure Modes to Document:**
- [ ] B-frames/pyramid structures incompatible with MP4
- [ ] Missing SPS/PPS in stream
- [ ] Non-monotonic DTS causing MP4 muxer errors
- [ ] Other: _______________

#### Q2: Audio - Is "always AAC transcode" sensible or should we use conditional copy?

**Current:** Audio is **always** transcoded to AAC
- `-c:a aac -b:a 192k -ar 48000 -profile:a aac_low`
- Filter: `aresample=async=1:first_pts=0,aformat=channel_layouts=stereo`

**Alternative Strategy:**
```bash
# Conditional audio handling (pseudo-code)
if audio_codec == "aac" && sample_rate in [44100, 48000] && channels <= 2:
    use -c:a copy
elif audio_codec in ["ac3", "eac3", "mp2", "mp3"]:
    use -c:a aac -b:a 192k ...
else:
    fallback to transcode
```

**Question for Reviewer:**
- Is always-transcode wasteful (CPU/quality) for already-AAC sources?
- What audio codecs from Enigma2 are MP4-compatible and should be copied?
- Risks of copying audio (sync issues, incompatible metadata)?

**Test Command:**
```bash
ffprobe -v error -select_streams a:0 \
    -show_entries stream=codec_name,sample_rate,channels,channel_layout,bit_rate \
    -of json input.ts
```

#### Q3: Subtitles/Data - We drop `-sn -dn`. Correct or losing relevant data?

**Dropped:**
- Subtitles (`-sn`)
- Data streams (`-dn`)

**Question:**
- Do Enigma2 TS files commonly include:
  - DVB subtitles (bitmap/text)?
  - Teletext?
  - EPG/metadata streams?
- Should any of these be preserved for browser playback?
- MP4 subtitle format compatibility (WebVTT extraction needed)?

---

### 2.2 Seeking/Duration/Resume Accuracy

#### Q4: TS has unreliable duration/timebase. Recommended flags/workarounds?

**Current flags for HLS build:**
- `-fflags +genpts+discardcorrupt`
- `-err_detect ignore_err`

**Current flags for MP4 remux:**
- (Minimal - only `-movflags +faststart`)

**Flags to Evaluate:**

| Flag | Purpose | TS→MP4 Benefit? | Risk/Downside? |
|------|---------|-----------------|----------------|
| `-fflags +genpts` | Regenerate PTS if missing/broken | ✅ Fixes timestamp gaps | ❓ May desync A/V if timestamps fundamentally broken |
| `-avoid_negative_ts make_zero` | Shift negative timestamps to zero | ✅ MP4 requirement | ⚠️ Can break sync if source has valid negative TS |
| `-copyts` | Keep original timestamps | ❌ Usually harmful for remux | ⚠️ Can create negative TS in MP4 |
| `-start_at_zero` | Force output to start at 0 | ✅ Clean MP4 timeline | ❓ Interaction with genpts? |
| `-muxpreload 0 -muxdelay 0` | Disable A/V preload buffering | ❓ Cleaner sync? | ❓ Testing needed |
| `-max_interleave_delta 0` | Tighter A/V interleaving | ❓ Better seeking? | ⚠️ May fail on badly interleaved sources |

**Question for Reviewer:**
Which combination of flags is **optimal** for TS→MP4 remux with:
- Correct duration metadata
- Clean seeking (moov at start, proper keyframe index)
- Resume accuracy (±1 second acceptable)

**Recommended Test Command Template:**
```bash
ffmpeg -y \
    -fflags +genpts \
    -avoid_negative_ts make_zero \
    -i input.ts \
    -c:v copy \
    -c:a aac -b:a 192k \
    -movflags +faststart \
    -sn -dn \
    output.mp4
```

#### Q5: Review current ffmpeg remux flags - sufficient or missing critical flags?

**Action:** Provide **concrete recommended flag set** with justification.

**Validation Commands:**
```bash
# Check moov atom position (should be at start with +faststart)
ffprobe -v error -show_entries format_tags=major_brand,compatible_brands \
    -show_entries format=start_time,duration -of json output.mp4

# Verify keyframe index
ffprobe -v error -select_streams v:0 -show_frames \
    -show_entries frame=key_frame,pkt_pts_time \
    -of csv output.mp4 | grep ",1$" | head -20

# Check for timestamp warnings during remux
ffmpeg -v warning -i input.ts -c copy -f null - 2>&1 | grep -i "timestamp\|dts\|pts"
```

---

### 2.3 Failure Modes / Edge Cases

#### Q6: Which TS characteristics typically kill remux?

**Known Problem Patterns:**

| Issue | Symptom | Detection Method | Mitigation |
|-------|---------|------------------|------------|
| **PTS/DTS jumps** | "Non-monotonous DTS" errors | ffmpeg stderr | `-fflags +genpts` + `-avoid_negative_ts` |
| **PCR drift** | Duration mismatch, sync issues | ffprobe duration vs actual | ? |
| **Corrupted packets** | Remux fails mid-stream | Exit code + stderr | `-err_detect ignore_err` + segment copy |
| **Missing PAT/PMT** | Stream not recognized | ffprobe fails | Pre-validation + fallback |
| **Multiple audio tracks** | Wrong track selected | ffprobe stream count | Explicit `-map` selection |
| **Changing audio PIDs** | Mid-stream audio switch | ? | ? |
| **Discontinuities** | Timeline breaks (ad insertion points) | ? | ? |

**Question for Reviewer:**
- **Complete this table** with your experience
- **Prioritize** which issues are most common in DVB-T/S recordings
- **Recommend detection heuristics** for each

**Diagnostic Commands:**
```bash
# Detect discontinuities
ffprobe -v error -show_packets -select_streams v:0 input.ts | \
    grep -E "pts_time|dts_time" | head -100

# Check for multiple audio/video streams
ffprobe -v error -show_entries stream=index,codec_type,codec_name \
    -of csv input.ts

# Validate PAT/PMT presence
ffmpeg -i input.ts -c copy -f mpegts -y /dev/null 2>&1 | \
    grep -i "pat\|pmt\|program"
```

#### Q7: Best "robustness ladder" strategy?

**Proposed Fallback Cascade:**

```
(1) FAST: copy video + copy audio (if both compatible)
    ↓ (detect: ffprobe codec check)
(2) MODERATE: copy video + transcode audio
    ↓ (detect: video remux error)
(3) SLOW: transcode video+audio (full rebuild)
    ↓ (detect: fatal error)
(4) FAIL: report as unplayable
```

**Question for Reviewer:**
- Is this ladder correct?
- **Define detection heuristics** for each step:
  - When is audio "compatible" for copy? (codec, sample rate, channels)
  - When should we abort copy and transcode video? (specific error patterns)
  - What errors are **unrecoverable** and should fail immediately?

**Example Heuristic (needs validation):**
```python
# Pseudo-code detection logic
def select_audio_strategy(stream_info):
    codec = stream_info["codec_name"]
    sample_rate = stream_info["sample_rate"]
    channels = stream_info["channels"]

    if codec == "aac" and sample_rate in [44100, 48000] and channels <= 2:
        return "copy"
    elif codec in ["ac3", "eac3", "mp2", "mp3"]:
        return "transcode_aac"
    else:
        return "transcode_aac"  # safe default

def select_video_strategy(stream_info, remux_test_result):
    if remux_test_result.exit_code == 0:
        return "copy"
    elif "Non-monotonous DTS" in remux_test_result.stderr:
        return "transcode_h264"  # timestamps broken beyond repair
    elif "invalid" in remux_test_result.stderr.lower():
        return "transcode_h264"
    else:
        return "fail"
```

---

### 2.4 Security/Serving/Policy

#### Q8: TS-only HLS Policy - still sensible today?

**Current Decision:** HLS with TS segments only (no fMP4)
- Segments: `seg_*.ts`
- No `init.mp4`, no `.m4s`/`.cmfv`

**Rationale (assumed):**
- Universal compatibility
- Simpler validation (single file type)
- Avoids init segment complexity

**Question for Reviewer:**
Evaluate for **modern clients** (2026):

| Client/Platform | TS-HLS Support | fMP4-HLS Support | Recommendation |
|-----------------|----------------|------------------|----------------|
| **Safari/iOS** | Native | Native (preferred?) | ? |
| **tvOS** | Native | Native | ? |
| **Plex (Apple clients)** | ? | ? | ? |
| **Chrome/Edge (hls.js)** | Via hls.js | Via hls.js (preferred?) | ? |
| **Android (ExoPlayer)** | ? | ? | ? |

**If you recommend fMP4 HLS:**

**Additional Complexity:**

| Aspect | TS-HLS | fMP4-HLS | Delta |
|--------|--------|----------|-------|
| **Segments** | `seg_*.ts` | `seg_*.m4s` + `init.mp4` | Init segment management |
| **Readiness Check** | 1 file check | 2 file checks (init + segment) | More complex |
| **Content-Type** | `video/MP2T` | `video/iso.segment` + `video/mp4` | Dual handling |
| **Cache Validation** | Single pattern | Two patterns | More attack surface? |
| **ffmpeg Flags** | `-f hls` (default) | `-hls_segment_type fmp4` + `-hls_fmp4_init_filename` | Additional config |

**Question:**
- Is the added complexity of fMP4 **justified** by measurably better client support/performance?
- Or is TS-HLS "good enough" for the next 3-5 years?
- Any **specific client bugs** with TS-HLS we should know about?

---

## 3) Concrete Tests (Execute or Assess)

### 3.1 Stream Inspection (Sample Enigma2 File)

**Required Command:**
```bash
ffprobe -hide_banner -v error \
    -show_streams -show_format \
    -print_format json \
    input.ts > stream_analysis.json
```

**Checklist:**
- [ ] Video: `codec_name`, `profile`, `level`, `pix_fmt`, `extradata_size`
- [ ] Audio: `codec_name`, `sample_rate`, `channels`, `channel_layout`, `bit_rate`
- [ ] Format: `duration`, `start_time`, `bit_rate`, `probe_score`
- [ ] Streams: Count of video/audio/subtitle/data tracks

**Attach:** `stream_analysis.json` with review findings

---

### 3.2 Remux Simulation (Recommended Command)

**Current Default:**
```bash
ffmpeg -y -i input.ts \
    -c:v copy \
    -c:a aac -b:a 192k -ar 48000 -profile:a aac_low \
    -movflags +faststart \
    -sn -dn \
    output.mp4
```

**Reviewer Task:**
Provide your **recommended "best default"** remux command with:
- All necessary flags for robust TS→MP4
- Justification for each flag
- Fallback command for "problem files"

**Expected Output Format:**
```bash
# RECOMMENDED DEFAULT REMUX
ffmpeg -y \
    -fflags +genpts \
    -avoid_negative_ts make_zero \
    -i input.ts \
    -c:v copy \
    -c:a <CONDITIONAL> \  # specify logic
    -movflags +faststart \
    -max_interleave_delta 0 \  # if beneficial
    -sn -dn \
    output.mp4

# FALLBACK (if above fails)
ffmpeg -y ...
```

---

### 3.3 Acceptance Criteria

**Define HARD criteria for each:**

#### MP4 Browser Playback
- [ ] File opens in Chrome/Safari/Edge without errors
- [ ] MSE/HTML5 `<video>` initialization < 2 seconds
- [ ] No console errors related to codec/container
- [ ] **Pass/Fail Threshold:** _______________

#### Seeking Stability
- [ ] 10 random seeks (0% → 50% → 90% → 25% → 75% → etc.)
- [ ] Each seek completes within 1 second
- [ ] No A/V desync > 200ms after seek
- [ ] No player stalls or buffering loops
- [ ] **Pass/Fail Threshold:** 9/10 seeks successful

#### Duration Accuracy
- [ ] ffprobe duration vs player-reported duration: `|Δ| < 1%`
- [ ] Duration stable across remux runs (not random)
- [ ] Resume position (e.g., 45:00 / 90:00) accurate within ±2 seconds
- [ ] **Pass/Fail Threshold:** _______________

#### Encode Warnings/Errors
- [ ] No "Non-monotonous DTS" warnings during remux
- [ ] No audio drift warnings
- [ ] No timestamp discontinuity errors
- [ ] stderr output < 10 warning lines (excluding info messages)
- [ ] **Pass/Fail Threshold:** Zero critical errors

**Test Script Template:**
```bash
#!/bin/bash
# acceptance_test.sh

INPUT="$1"
OUTPUT="output_test.mp4"

echo "=== REMUX TEST ==="
ffmpeg -y -i "$INPUT" \
    -c:v copy -c:a aac -b:a 192k \
    -movflags +faststart \
    -sn -dn \
    "$OUTPUT" 2>&1 | tee remux.log

echo "=== VALIDATION ==="
ffprobe -v error -show_entries format=duration -of default=nw=1:nk=1 "$OUTPUT"
echo "Warnings: $(grep -i 'warning\|error' remux.log | wc -l)"

echo "=== BROWSER TEST (manual) ==="
echo "Open file://$PWD/$OUTPUT in Chrome and validate:"
echo "  1. Plays immediately"
echo "  2. Seeking works (test 10 random positions)"
echo "  3. Duration matches expectation"
```

---

## 3.4 Hard Acceptance Gates (No Fluff - Data Only)

**GATE 1: ffmpeg Remux Flags (TS→MP4)**

Provide **TWO command sets** (Default + Fallback):

```bash
# DEFAULT REMUX (for 95% of cases - clean H.264 + compliant audio):
ffmpeg -y \
    -fflags <???> \          # WHY: _______________
    -avoid_negative_ts <???> \  # WHY: _______________
    -i input.ts \
    -c:v copy \              # CONDITION: H.264 yuv420p 8-bit
    -c:a <???> \             # CONDITION: _______________
    -movflags <???> \        # WHY: _______________
    <other flags?> \
    output.mp4

# FALLBACK REMUX (when Non-monotonous DTS / timestamp issues detected):
ffmpeg -y \
    <different flags?> \     # EXPLAIN: What changes vs default?
    -i input.ts \
    -c:v copy \
    -c:a <???> \
    output.mp4

# TRANSCODE FALLBACK (when remux fails OR HEVC/10-bit detected):
ffmpeg -y \
    <flags> \
    -i input.ts \
    -c:v libx264 \           # Specific preset/CRF?
    -c:a aac \
    output.mp4
```

**Explicitly state which flags NEVER to use (absolute no-gos only):**
- [ ] `-copyts` - Reason: _______________
- [ ] Other absolute no-gos: _______________

**Flags that are conditional (state when to use/avoid):**
- `-start_at_zero`: Use when _______________ / Avoid when _______________
- `-muxdelay`: Use when _______________ / Avoid when _______________

**GATE 2: Codec Matrix (Real Enigma2 Data)**

Analyze **minimum 10 representative files** from actual Enigma2 recordings:

| Metric | Count | Percentage | Notes |
|--------|-------|------------|-------|
| **Video Codec** | | | |
| H.264 | ___ / 10 | ___% | Profile/Level distribution |
| H.265/HEVC | ___ / 10 | ___% | Browser compatibility concern? |
| Other (MPEG2, etc.) | ___ / 10 | ___% | |
| **Audio Codec** | | | |
| AAC | ___ / 10 | ___% | Sample rates: _______________ |
| AC3 | ___ / 10 | ___% | Needs transcode for browser |
| EAC3/DD+ | ___ / 10 | ___% | Needs transcode |
| MP2 | ___ / 10 | ___% | Needs transcode |
| **Multi-track Audio** | | | |
| Single audio track | ___ / 10 | ___% | |
| Multiple tracks | ___ / 10 | ___% | Track selection needed? |
| **Subtitle/Data Streams** | | | |
| DVB subtitles | ___ / 10 | ___% | Worth preserving? |
| Teletext | ___ / 10 | ___% | |
| None | ___ / 10 | ___% | |

**Attach:** Raw `ffprobe` output for all 10 files as JSON array

**GATE 3: Seek Test + Duration Delta (Empirical)**

**Test each of the 10 files:**

```bash
# For each file:
# 1. Remux with recommended flags
# 2. Perform 10 random seeks: 0%→50%→90%→25%→75%→10%→60%→95%→30%→80%
# 3. Record failures + stderr

# Template:
for f in sample_{01..10}.ts; do
    ffmpeg -y <YOUR_RECOMMENDED_FLAGS> -i "$f" "test_${f%.ts}.mp4" 2>stderr_$f.log
    # Manual browser seek test or automated via ffprobe
done
```

**Results Table:**

| File | Remux Exit Code | Remux Warnings | Duration Delta (ffprobe vs TS) | Seek Failures (out of 10) | stderr Issues |
|------|-----------------|----------------|-------------------------------|---------------------------|---------------|
| sample_01.ts | 0 / 1 | ___ | ±___% | ___ / 10 | _______________ |
| sample_02.ts | 0 / 1 | ___ | ±___% | ___ / 10 | _______________ |
| ... | | | | | |
| sample_10.ts | 0 / 1 | ___ | ±___% | ___ / 10 | _______________ |
| **TOTALS** | ___/10 success | ___ warnings | **Avg: ±___%** | **___/100 seeks OK** | Pattern: _______________ |

**Acceptance Thresholds:**
- [ ] Remux success rate: **≥ 90%** (9/10 files)
- [ ] Duration delta: **≤ 1%** average
- [ ] Seek success: **≥ 95%** (95/100 seeks)
- [ ] Critical stderr errors: **0** (warnings acceptable if documented)

**Common stderr Patterns to Document:**

| Pattern | Count | Severity | Action |
|---------|-------|----------|--------|
| "Non-monotonous DTS in output stream" | ___ | High/Med/Low | _______________ |
| "Application provided invalid, non monotonically increasing dts to muxer" | ___ | High/Med/Low | _______________ |
| "Packet with invalid duration" | ___ | High/Med/Low | _______________ |
| "Past duration ... too large" | ___ | High/Med/Low | _______________ |
| "timestamps are unset in a packet for stream" | ___ | High/Med/Low | _______________ |

**Attach:** All 10 `stderr_*.log` files

---

**GATE 4: Target Clients & Playback Stack (Critical for Codec Decisions)**

**Define actual deployment environment:**

| Client/Platform | Playback Method | Must-Support Codecs | HEVC/H.265 Support? | Notes |
|-----------------|-----------------|---------------------|---------------------|-------|
| **Chrome/Edge (Desktop)** | HLS.js OR Direct MP4? | H.264: ___ / HEVC: ___ | ✅ / ❌ / ⚠️ Limited | MSE limitations? |
| **Safari (macOS)** | Native HLS OR Direct MP4? | H.264: ___ / HEVC: ___ | ✅ / ❌ | |
| **Safari (iOS/iPadOS)** | Native HLS OR Direct MP4? | H.264: ___ / HEVC: ___ | ✅ / ❌ | Hardware decode? |
| **Plex (iOS/tvOS Client)** | Plex transcoding OR Direct? | H.264: ___ / HEVC: ___ | ✅ / ❌ | Does Plex transcode server-side? |
| **Plex (Web Client)** | Browser rules apply? | H.264: ___ / HEVC: ___ | ✅ / ❌ | |
| **Plex (Android)** | Direct OR transcode? | H.264: ___ / HEVC: ___ | ✅ / ❌ | ExoPlayer capabilities? |
| **Other (specify)** | _______________ | _______________ | _______________ | _______________ |

**Critical Questions:**

1. **Progressive Playback (HLS):**
   - Chrome/Edge: Using `hls.js` library? (TS and fMP4 both supported)
   - Safari: Native HLS player? (prefers fMP4 but supports TS)
   - What is the **primary target** (most common user agent)?

2. **Finished Playback (Direct MP4):**
   - Served via HTML5 `<video src="stream.mp4">` direct?
   - Or via MSE (Media Source Extensions)?
   - Does Plex request MP4 directly or transcode server-side?

3. **HEVC/H.265 Decision Tree:**
   ```
   IF (Gate 2 shows >20% HEVC sources):
       IF (primary clients support HEVC natively):
           → Can use -c:v copy for HEVC
       ELSE:
           → MUST transcode HEVC → H.264 (or mark as incompatible)
   ELSE:
       → HEVC edge case, handle as fallback
   ```

**Expected Output:**

Fill this decision matrix:

| Scenario | Client Support | Action |
|----------|----------------|--------|
| H.264 in MP4 | All clients ✅ | `-c:v copy` safe |
| HEVC in MP4 | Safari ✅, Chrome ❌ | Transcode to H.264 OR fail fast? |
| AAC audio | All clients ✅ | `-c:a copy` if compatible |
| AC3 audio in MP4 | Safari ✅, Chrome ❌ | **Must transcode to AAC** |
| EAC3/DD+ in MP4 | Limited support | **Must transcode to AAC** |

**Why This Gate is Critical:**

Without client matrix, we cannot decide:
- Whether HEVC sources need universal transcode (huge CPU cost)
- Whether AC3 passthrough is acceptable (Safari works, Chrome fails)
- Whether TS-HLS vs fMP4-HLS matters (hls.js vs native player)

---

## 4) Expected Deliverable

**Reviewer Response Format:**

### Summary
- [ ] **GO** - Current strategy is sound
- [ ] **GO with changes** - Strategy OK but needs flag/heuristic adjustments
- [ ] **NO-GO** - Fundamental issue, redesign needed

### Recommended Changes

#### Flags (Concrete)
```bash
# Default remux command:
<insert exact command>

# Fallback command:
<insert exact command>
```

#### Detection Heuristics
```python
# Audio copy vs transcode decision:
<pseudo-code or logic>

# Video copy vs transcode fallback:
<pseudo-code or logic>
```

#### HLS Policy
- [ ] **Keep TS-only** - Rationale: _______________
- [ ] **Migrate to fMP4** - Rationale: _______________ | Timeline: _______________
- [ ] **Conditional** (TS for progressive, fMP4 for finished) - Rationale: _______________

### Test Results
- Attach `stream_analysis.json` from representative Enigma2 file
- Attach `remux.log` from test run
- Document any edge cases discovered

### Risk Assessment
- **High Risk Issues:** _______________
- **Medium Risk Issues:** _______________
- **Low Risk / Acceptable:** _______________

---

## 5) Next Steps (After Review)

Upon reviewer response, we will generate:
1. **ADR (Architectural Decision Record)** - Document rationale for final strategy
2. **Config/Flag Patch** - Update `runRecordingBuild()` and remux logic with reviewed flags
3. **Test Cases** - Add unit/integration tests for critical failure modes identified
4. **Monitoring/Metrics** - Add observability for detected failure patterns

---

**Reviewer Name:** _______________
**Review Date:** _______________
**Signature/Approval:** _______________
