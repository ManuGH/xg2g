# Request for Technical Review: VOD Remux Strategy

**To**: Technician/Reviewer
**From**: Development Team
**Date**: 2026-01-03
**Subject**: Empirical validation needed for TS→MP4 remux operationalization

---

## Background

We're implementing a VOD recording system for xg2g (Enigma2 streaming) with two playback paths:

1. **HLS Progressive** (during build): TS-HLS segments via `.m3u8` ✅ **Complete**
2. **Direct MP4** (finished): Remuxed MP4 via `/stream.mp4` ⚠️ **Awaiting your data**

The HLS path is hardened and tested. However, the Direct MP4 path requires **empirical validation** to avoid common TS→MP4 remux pathologies:

- Non-monotonous DTS/PTS issues
- Invalid duration (breaks Resume/Continue Watching)
- HEVC browser incompatibility (Safari yes, Chrome no)
- AC3 audio incompatibility (Safari yes, Chrome no)
- 10-bit H.264 issues (Chrome incompatible)

---

## What We Need From You

Please fill out the attached **structured template** with empirical data from **10 real Enigma2 recording files**.

**Template**: [REVIEWER_TEMPLATE_RESPONSE.md](REVIEWER_TEMPLATE_RESPONSE.md)

The template is designed to ensure your response is **patchable** (not "it depends" answers). It has 4 gates:

### Gate 1: ffmpeg Remux Flags (TS→MP4)

Provide **THREE exact commands**:

1. **DEFAULT REMUX** (95% use case - clean H.264 + compliant audio):
   ```bash
   ffmpeg -y \
       -fflags <???> \
       -avoid_negative_ts <???> \
       -i input.ts \
       -c:v copy \
       -c:a <???> \
       -movflags <???> \
       output.mp4
   ```

2. **FALLBACK REMUX** (when non-monotonous DTS detected):
   ```bash
   ffmpeg -y \
       <different flags?> \
       -i input.ts \
       -c:v copy \
       -c:a aac \
       output.mp4
   ```

3. **TRANSCODE FALLBACK** (when remux failed OR HEVC/10-bit detected):
   ```bash
   ffmpeg -y \
       <flags> \
       -i input.ts \
       -c:v libx264 -preset <???> -crf <???> \
       -c:a aac \
       output.mp4
   ```

**Explicitly state**:
- Which flags to NEVER use (e.g., `-copyts`)
- Which flags are conditional (when to use/avoid)

---

### Gate 2: Codec Matrix (Real Enigma2 Data)

Analyze **10 representative files** from actual Enigma2 recordings:

| Metric | Count | % | Notes |
|--------|-------|---|-------|
| **Video Codec** | | | |
| H.264 | ___ / 10 | ___% | **PixFmt**: yuv420p: ___ / yuv420p10le: ___ |
| HEVC/H.265 | ___ / 10 | ___% | **Chrome incompatible** |
| MPEG2 | ___ / 10 | ___% | |
| **Audio Codec** | | | |
| AAC | ___ / 10 | ___% | Sample rate(s): _______________ |
| AC3 | ___ / 10 | ___% | **Must transcode for Chrome** |
| EAC3/DD+ | ___ / 10 | ___% | **Must transcode** |
| MP2 | ___ / 10 | ___% | **Must transcode** |

**Attach**: Raw `ffprobe` JSON output for all 10 files

**Critical**: Include **pixel format** (yuv420p vs yuv420p10le) and **bit depth** - 10-bit H.264 is Chrome-incompatible even though codec is H.264.

---

### Gate 3: Seek Test + Duration Delta (Empirical)

For **each of the 10 files**:

1. Remux with your recommended DEFAULT flags
2. Perform **10 random seeks**: 0%→50%→90%→25%→75%→10%→60%→95%→30%→80%
3. Record failures + stderr

**Results Table**:

| File | Remux Exit | Duration Δ | Seek Failures (out of 10) | stderr Pattern |
|------|------------|------------|---------------------------|----------------|
| sample_01.ts | 0 / 1 | ±___% | ___ / 10 | _______________ |
| sample_02.ts | 0 / 1 | ±___% | ___ / 10 | _______________ |
| ... | | | | |
| **TOTALS** | ___/10 | **Avg: ±___%** | **___/100** | See catalog below |

**Acceptance Thresholds**:
- [ ] Remux success: **≥90%** (9/10)
- [ ] Duration delta: **≤1%** average
- [ ] Seek success: **≥95%** (95/100)
- [ ] Critical errors: **0**

**stderr Pattern Catalog**:

| Pattern | Count (out of 10) | Severity | Action |
|---------|-------------------|----------|--------|
| `Non-monotonous DTS in output stream` | ___ | High/Med/Low | Use fallback flags / Fail fast / Warn only |
| `Packet with invalid duration` | ___ | High/Med/Low | _______________ |
| `Past duration ... too large` | ___ | High/Med/Low | _______________ |
| `timestamps are unset in a packet` | ___ | High/Med/Low | _______________ |

**Attach**: All 10 `stderr_*.log` files

---

### Gate 4: Target Clients & Playback Stack

**Define deployment environment**:

| Client/Platform | Playback Method | HEVC Support? | AC3 Support? | Notes |
|-----------------|-----------------|---------------|--------------|-------|
| **Chrome Desktop** | HLS.js / Direct MP4 | ✅ / ❌ | ✅ / ❌ | Primary client? |
| **Safari macOS** | Native HLS / Direct MP4 | ✅ / ❌ | ✅ / ❌ | |
| **Safari iOS** | Native HLS / Direct MP4 | ✅ / ❌ | ✅ / ❌ | |
| **Plex (iOS/tvOS)** | Direct / Transcode? | ✅ / ❌ | ✅ / ❌ | Server-side transcode? |
| **Plex (Web)** | Browser rules? | ✅ / ❌ | ✅ / ❌ | |
| **Plex (Android)** | Direct / Transcode? | ✅ / ❌ | ✅ / ❌ | ExoPlayer? |

**Critical Questions**:

1. What is the **primary target client** (most common user agent)?
   - Example: "Chrome Desktop 80%"

2. Progressive Playback (HLS):
   - Chrome/Edge: Using `hls.js` library? (supports both TS and fMP4)
   - Safari: Native HLS player? (prefers fMP4 but supports TS)

3. Finished Playback (Direct MP4):
   - Served via HTML5 `<video src="stream.mp4">` direct?
   - Or via MSE (Media Source Extensions)?
   - Does Plex request MP4 directly or transcode server-side?

4. **HEVC Decision Tree** (fill in):
   ```
   IF (Gate 2 shows >20% HEVC sources):
       IF (primary client supports HEVC natively):
           → Can use -c:v copy for HEVC
       ELSE:
           → MUST transcode HEVC → H.264 (or mark as incompatible)
   ELSE:
       → HEVC edge case, handle as fallback
   ```

---

## Why This Matters

Without your empirical data:

1. **Gate 1 missing** → We don't know which flags to use → Remux fails on real files
2. **Gate 2 missing** → We don't know codec distribution → Can't build decision tree
3. **Gate 3 missing** → We don't know actual failure modes → Can't classify errors
4. **Gate 4 missing** → We don't know target clients → Can't decide HEVC/AC3 strategy

Example failure scenario without Gate 4:
- You say "use `-c:v copy` for HEVC" (technically correct)
- We deploy
- 80% of users are Chrome Desktop → **HEVC doesn't play** (Chrome incompatible)
- System is "technically correct" but operationally broken

---

## What We'll Do With Your Data

Once you fill the template, we will:

1. **Apply exact flags** from Gate 1 to remux commands
2. **Build codec decision tree** based on Gate 2 distribution + Gate 4 client matrix
3. **Implement error classifier** using Gate 3 stderr patterns → typed errors → retry ladder
4. **Write ADR** (Architecture Decision Record) with empirical justification
5. **Create integration tests** validating your 90%/1%/95% thresholds

Everything is **scaffolded and ready** - we just need your data to operationalize.

**Files Ready**:
- [internal/api/recordings_remux.go](../internal/api/recordings_remux.go) - Decision logic (TODOs marked)
- [internal/api/recordings_remux_test.go](../internal/api/recordings_remux_test.go) - Tests (passing)
- [docs/PATCH_GUIDE_MP4_REMUX.md](PATCH_GUIDE_MP4_REMUX.md) - Exact patches to apply

---

## Deliverables Expected From You

**Required**:
1. ✅ Filled [REVIEWER_TEMPLATE_RESPONSE.md](REVIEWER_TEMPLATE_RESPONSE.md) with all 4 gates
2. ✅ Attached `ffprobe` JSON for 10 files
3. ✅ Attached 10 `stderr_*.log` files from remux tests

**Optional (but helpful)**:
- Screenshots of browser playback tests showing seek behavior
- Notes on any edge cases or surprises

---

## Timeline

**No rush** - take the time needed to run the tests properly. Quality over speed.

**Estimated effort**:
- Gate 1: 30 minutes (command research + testing)
- Gate 2: 20 minutes (ffprobe 10 files, analyze output)
- Gate 3: 1-2 hours (remux 10 files, 100 seek tests, collect logs)
- Gate 4: 30 minutes (document client matrix, playback paths)

**Total**: ~3 hours of hands-on testing

---

## Questions?

If anything in the template is unclear:

1. Read [TECHNICAL_REVIEW_VOD_REMUX.md](TECHNICAL_REVIEW_VOD_REMUX.md) for detailed context
2. Ask for clarification before starting (better to align upfront)

---

## Bottom Line

We need **concrete, patchable data** to make the Direct MP4 remux production-ready.

The template is designed to prevent "it depends" answers and ensure your output translates directly into:
- ✅ Exact ffmpeg commands
- ✅ Typed error handling
- ✅ Justified codec decisions
- ✅ Validated acceptance thresholds

**Your data drives the implementation** - no guesswork, no "we think this will work."

Thank you for your expertise! 🙏

---

**Attachments**:
- [REVIEWER_TEMPLATE_RESPONSE.md](REVIEWER_TEMPLATE_RESPONSE.md) - Fill this out
- [TECHNICAL_REVIEW_VOD_REMUX.md](TECHNICAL_REVIEW_VOD_REMUX.md) - Context and rationale
- [IMPLEMENTATION_STATUS.md](IMPLEMENTATION_STATUS.md) - Current state of the codebase
