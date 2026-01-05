# Reviewer Response Template: VOD Remux Technical Review

**Reviewer Name:** _______________
**Date:** _______________

---

## GATE 1: ffmpeg Remux Flags (TS→MP4)

### Default Remux Command (95% use case)

```bash
ffmpeg -y \
    -fflags +genpts \
    -avoid_negative_ts make_zero \
    -i input.ts \
    -c:v copy \
    -c:a aac -b:a 192k -ar 48000 -ac 2 \
    -movflags +faststart \
    -sn -dn \
    output.mp4
```

**Flag Justifications:**

| Flag | Justification | Risk/Downside |
|------|---------------|---------------|
| `-fflags +genpts` | _______________ | _______________ |
| `-avoid_negative_ts make_zero` | _______________ | _______________ |
| `-movflags +faststart` | _______________ | _______________ |
| `-c:v copy` | _______________ (CONDITION: H.264 yuv420p 8-bit only) | _______________ |
| `-c:a aac ...` | _______________ (Chrome compatibility) | _______________ |

### Fallback Remux Command (Non-monotonous DTS detected)

```bash
ffmpeg -y \
    <YOUR_RECOMMENDED_FALLBACK_FLAGS> \
    -i input.ts \
    -c:v copy \
    -c:a aac -b:a 192k \
    output.mp4
```

**Changes vs Default:**
- _______________
- _______________

### Transcode Fallback (HEVC/10-bit OR remux failed)

```bash
ffmpeg -y \
    <YOUR_RECOMMENDED_TRANSCODE_FLAGS> \
    -i input.ts \
    -c:v libx264 -preset <PRESET?> -crf <CRF?> \
    -c:a aac -b:a 192k \
    output.mp4
```

**Transcode Parameters:**
- Preset: _______________ (WHY: _______________)
- CRF: _______________ (WHY: _______________)
- Other: _______________

### DO NOT USE (Absolute No-Gos)

- **`-copyts`**: Reason: _______________
- **Other**: _______________

### Conditional Flags

- **`-start_at_zero`**: Use when _______________ / Avoid when _______________
- **`-muxdelay`**: Use when _______________ / Avoid when _______________

---

## GATE 2: Codec Matrix (10 Real Enigma2 Files)

### Video Codec Distribution

| Codec | Count | % | Profile/Level | Pixel Format | Notes |
|-------|-------|---|---------------|--------------|-------|
| **H.264** | ___ / 10 | ___% | _______________ | yuv420p: ___ / yuv420p10le: ___ | _______________ |
| **HEVC** | ___ / 10 | ___% | _______________ | yuv420p: ___ / yuv420p10le: ___ | **Chrome incompatible** |
| **MPEG2** | ___ / 10 | ___% | _______________ | _______________ | _______________ |

### Audio Codec Distribution

| Codec | Count | % | Sample Rate(s) | Channels | Notes |
|-------|-------|---|----------------|----------|-------|
| **AAC** | ___ / 10 | ___% | _______________ | Stereo: ___ / 5.1: ___ | Copy-safe if 48kHz stereo |
| **AC3** | ___ / 10 | ___% | _______________ | _______________ | **Must transcode (Chrome)** |
| **EAC3/DD+** | ___ / 10 | ___% | _______________ | _______________ | **Must transcode** |
| **MP2** | ___ / 10 | ___% | _______________ | _______________ | **Must transcode** |

### Multi-track Audio

| Pattern | Count | Notes |
|---------|-------|-------|
| Single audio track | ___ / 10 | _______________ |
| Multiple tracks (multi-lang) | ___ / 10 | Track selection needed? _______________ |

### Subtitle/Data Streams

| Type | Count | Worth Preserving? | Browser Compatibility |
|------|-------|-------------------|----------------------|
| DVB Subtitles | ___ / 10 | Yes / No | _______________ |
| Teletext | ___ / 10 | Yes / No | _______________ |
| None | ___ / 10 | N/A | N/A |

**Attach:** `enigma2_samples_ffprobe.json` (all 10 files)

---

## GATE 3: Seek Test + Duration Delta (Empirical Results)

### Test Methodology

**Remux Command Used:**
```bash
<PASTE YOUR EXACT COMMAND HERE>
```

**Seek Test Definition:**
- "Seek success" means: _______________
  - Example: "Frame displayed within 2 seconds + A/V sync OK + no stall loop"

**Duration Measurement:**
- TS duration: `ffprobe -v error -show_entries format=duration input.ts`
- MP4 duration: `ffprobe -v error -show_entries format=duration output.mp4`

### Results Table

| File | Remux Exit | Warnings | Duration Δ | Seek OK | stderr Pattern |
|------|------------|----------|------------|---------|----------------|
| sample_01.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_02.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_03.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_04.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_05.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_06.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_07.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_08.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_09.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| sample_10.ts | 0 / 1 | ___ | ±___%  | ___ / 10 | _______________ |
| **TOTALS** | ___/10 | ___ | **Avg: ±___%** | **___/100** | See patterns below |

**Acceptance Check:**
- [ ] Remux success rate: ≥ 90% (9/10) → **ACTUAL: ___/10**
- [ ] Duration delta: ≤ 1% average → **ACTUAL: ±___%**
- [ ] Seek success: ≥ 95% (95/100) → **ACTUAL: ___/100**
- [ ] Critical errors: 0 → **ACTUAL: ___**

### stderr Pattern Catalog

| Pattern | Count (out of 10) | Severity | Recommended Action |
|---------|-------------------|----------|-------------------|
| `Non-monotonous DTS in output stream` | ___ | High / Med / Low | _______________ |
| `Application provided invalid, non monotonically increasing dts to muxer` | ___ | High / Med / Low | _______________ |
| `Packet with invalid duration` | ___ | High / Med / Low | _______________ |
| `Past duration ... too large` | ___ | High / Med / Low | _______________ |
| `timestamps are unset in a packet` | ___ | High / Med / Low | _______________ |
| Other: _______________ | ___ | High / Med / Low | _______________ |

**Attach:** All `stderr_*.log` files

---

## GATE 4: Target Clients & Playback Stack

### Deployment Environment

**Primary Client (most common):**
_______________ (e.g., "Chrome Desktop 80%")

**Client Support Matrix:**

| Client/Platform | Playback Method | H.264 Support | HEVC Support | AC3 in MP4 | Notes |
|-----------------|-----------------|---------------|--------------|------------|-------|
| **Chrome/Edge Desktop** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | _______________ |
| **Safari macOS** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | _______________ |
| **Safari iOS/iPadOS** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | _______________ |
| **Plex iOS/tvOS** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | Server transcodes? ___ |
| **Plex Web** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | _______________ |
| **Plex Android** | _______________ | ✅ / ❌ | ✅ / ❌ | ✅ / ❌ | _______________ |

### Playback Path Details

1. **Progressive HLS Playback:**
   - Chrome/Edge use: Native HLS / **hls.js library** / Other: _______________
   - Safari uses: **Native HLS player** / hls.js / Other: _______________
   - Segment format: Currently TS-HLS / Should migrate to fMP4-HLS? _______________

2. **Finished VOD Playback:**
   - Served via: HTML5 `<video src="stream.mp4">` direct / MSE / Other: _______________
   - Plex behavior: Requests MP4 direct / Transcodes server-side / _______________

### Codec Decision Matrix

**Fill based on client support above:**

| Scenario | Client Support | Recommended Action |
|----------|----------------|-------------------|
| H.264 yuv420p 8-bit in MP4 | All clients ✅ / Partial | `-c:v copy` safe / Transcode |
| H.264 yuv420p10le (10-bit) | All clients ✅ / **Chrome ❌** | `-c:v copy` safe / **Transcode to 8-bit** |
| HEVC yuv420p in MP4 | Safari ✅ / **Chrome ❌** | **Transcode to H.264** / Fail fast / Other: ___ |
| AAC stereo 48kHz | All clients ✅ / Partial | `-c:a copy` safe / Transcode |
| AC3 in MP4 | Safari ✅ / **Chrome ❌** | **Must transcode to AAC** |
| EAC3/DD+ in MP4 | Limited support | **Must transcode to AAC** |

### Critical Decision

**Based on Gate 2 codec distribution + Gate 4 client support:**

If HEVC is >20% of sources AND primary client is Chrome:
- [ ] **Transcode all HEVC → H.264** (HIGH CPU cost, but required)
- [ ] **Fail fast on HEVC** (reject playback, inform user)
- [ ] **Conditional**: Serve HEVC to Safari, H.264 to Chrome (complex, double cache)

**Your Recommendation:** _______________

---

## SUMMARY & RECOMMENDATION

### Overall Assessment

- [ ] **GO** - Current strategy (TS→MP4 remux + TS-HLS) is sound with these flag changes
- [ ] **GO with changes** - Strategy OK but requires:
  - Flag adjustments (see Gate 1)
  - Codec decision tree (see Gate 4)
  - Error classifier (see Gate 3 patterns)
- [ ] **NO-GO** - Fundamental issue requiring redesign: _______________

### Key Findings

1. **Video Codec Reality:**
   - H.264: ___% (safe for remux)
   - HEVC: ___% (**requires transcode for Chrome**)
   - 10-bit content: ___% (**Chrome compatibility?**)

2. **Audio Codec Reality:**
   - AAC: ___% (copy-safe if stereo 48kHz)
   - AC3/EAC3: ___% (**must transcode for Chrome**)

3. **Remux Success Rate:**
   - Default flags: ___/10 succeeded
   - Duration accuracy: ±___%
   - Seek reliability: ___/100

4. **Primary Client Impact:**
   - Primary client: _______________
   - HEVC playback: ✅ Supported / ❌ **Requires transcode**
   - AC3 playback: ✅ Supported / ❌ **Requires transcode**

### Recommended Implementation

**Default Remux Strategy (copy/copy):**
```bash
# Use when: H.264 yuv420p 8-bit + AAC stereo 48kHz
<PASTE COMMAND FROM GATE 1>
```

**Fallback Remux Strategy (copy/transcode):**
```bash
# Use when: H.264 OK but audio needs transcode
<PASTE COMMAND>
```

**Transcode Strategy:**
```bash
# Use when: HEVC detected OR remux failed
<PASTE COMMAND FROM GATE 1>
```

### HLS Policy Recommendation

**TS-HLS vs fMP4-HLS:**
- [ ] **Keep TS-HLS** - Reason: _______________
- [ ] **Migrate to fMP4-HLS** - Reason: _______________ | Timeline: _______________
- [ ] **Conditional (both)** - Reason: _______________

### Risk Assessment

**High Risk:**
- _______________

**Medium Risk:**
- _______________

**Low Risk / Acceptable:**
- _______________

---

## ATTACHMENTS REQUIRED

Please attach the following files to this response:

1. **`enigma2_samples_ffprobe.json`** - ffprobe output for all 10 test files
2. **`stderr_*.log`** (10 files) - ffmpeg stderr from remux tests
3. **Optional:** Screenshots of browser playback tests showing seek behavior

---

**Reviewer Signature:** _______________
**Approval Date:** _______________
