# MP4 Remux: Empirical Validation Required

**To**: Technician (xg2g Host Access)
**Date**: 2026-01-03
**Status**: Awaiting Gate 1-4 Data

---

## Context

MP4 remux path is structurally complete:
- ✅ Probe-based codec detection
- ✅ Three-tier ladder (default → fallback → transcode)
- ✅ Progress supervision (stall detection)
- ✅ Operator artifacts (.meta.json, .err.log)
- ✅ Test coverage (19 tests passing)

**What's missing**: Empirical data to replace placeholders with production-validated values.

---

## What You Need To Do

Run commands **directly on xg2g host** (where ffmpeg/ffprobe are installed and TS files are accessible).

**Location**: You're already here (`/root/xg2g`)

### Gate 1: ffmpeg Flags (30 min)

**Test with ONE representative TS file**:

```bash
# Test 1: Default remux (clean copy)
ffmpeg -y -i sample.ts -c:v copy -c:a aac -b:a 192k -movflags +faststart output_default.mp4

# Test 2: With timestamp fixes (if Test 1 shows DTS errors)
ffmpeg -y -fflags +genpts -avoid_negative_ts make_zero -i sample.ts \
    -c:v copy -c:a aac -b:a 192k -movflags +faststart output_fallback.mp4

# Test 3: Full transcode (if copy fails)
ffmpeg -y -i sample.ts -c:v libx264 -preset medium -crf 23 \
    -c:a aac -b:a 192k -movflags +faststart output_transcode.mp4
```

**Answer these questions**:

1. Which flags **work reliably** on your TS files?
2. Which flags **break** (e.g., `-copyts`, `-noaccurate_seek`)?
3. Does `-avoid_negative_ts make_zero` fix DTS errors?

**Deliverable**: Exact command line that works, with explanation of why.

---

### Gate 2: Codec Distribution (10 min)

**Probe 10 representative TS files**:

```bash
for f in *.ts; do
    ffprobe -v quiet -show_streams -print_format json "$f" > "${f%.ts}_probe.json"
done
```

**Extract this info**:

| File | Video Codec | Pixel Format | Bit Depth | Audio Codec | Sample Rate |
|------|-------------|--------------|-----------|-------------|-------------|
| 1    | h264/hevc   | yuv420p/p10le| 8/10      | aac/ac3/mp2 | 48000       |
| ...  | ...         | ...          | ...       | ...         | ...         |

**Answer**:
- How many files are H.264 vs HEVC?
- How many are 8-bit vs 10-bit? (Critical for Chrome)
- How many have AAC vs AC3/MP2?

**Deliverable**: Simple table + attach all JSON files.

---

### Gate 3: Error Patterns (60 min)

**Remux all 10 files and collect stderr**:

```bash
for f in *.ts; do
    ffmpeg -y -i "$f" -c:v copy -c:a aac output.mp4 2> "${f%.ts}_stderr.log"
done
```

**Categorize errors**:

| Error Pattern | Count | Severity | Retry Strategy |
|---------------|-------|----------|----------------|
| `Non-monotonous DTS` | X/10 | High | Use fallback flags |
| `Invalid duration` | X/10 | High | Fail fast (breaks Resume) |
| `timestamps are unset` | X/10 | Med | Use `-fflags +genpts` |
| `Past duration ... too large` | X/10 | Low | Warn only |

**Deliverable**: Table + attach all 10 stderr logs.

---

### Gate 4: Target Client (5 min)

**Answer one question**:

> What is the **primary playback client**?
>
> Examples:
> - "Chrome Desktop (80% of traffic)"
> - "Safari iOS via Plex"
> - "Mix: 50% Chrome, 30% Safari, 20% Plex"

**Why this matters**:
- **Chrome Desktop**: HEVC doesn't work → must transcode
- **Safari/Plex**: HEVC works → can copy

**Deliverable**: One sentence stating primary client.

---

## Why This Is Quick

You don't need to:
- ❌ Setup test environment (you're already on xg2g)
- ❌ Download/upload files (TS files are local)
- ❌ Install tools (ffmpeg/ffprobe already configured)
- ❌ Write code (just run commands, paste output)

**Estimated time**: 2 hours total (mostly waiting for ffmpeg)

---

## What Happens Next

When you send the data:

1. **15-minute patch** - Replace placeholder flags with your Gate 1 commands
2. **10-minute patch** - Update codec decision tree with Gate 2 distribution
3. **20-minute patch** - Add stderr pattern classifier from Gate 3
4. **5-minute patch** - Finalize HEVC/AC3 policy based on Gate 4 client

**Total**: ~50 minutes to go from "scaffolded" to "production-validated"

---

## Template to Fill

Use [REVIEWER_TEMPLATE_RESPONSE.md](REVIEWER_TEMPLATE_RESPONSE.md) or just answer inline:

```markdown
### Gate 1: ffmpeg Flags

**DEFAULT (works):**
ffmpeg -y -i input.ts -c:v copy -c:a aac -b:a 192k -movflags +faststart output.mp4

**FALLBACK (for DTS errors):**
[your command here]

**TRANSCODE (last resort):**
[your command here]

**Flags to AVOID:**
- `-copyts` (why: ...)

---

### Gate 2: Codec Distribution

| Metric | Count |
|--------|-------|
| H.264 8-bit | 8/10 |
| H.264 10-bit | 1/10 |
| HEVC | 1/10 |
| AAC audio | 7/10 |
| AC3 audio | 3/10 |

**Attached**: `probe_*.json` files

---

### Gate 3: Error Patterns

| Pattern | Count | Action |
|---------|-------|--------|
| Non-monotonous DTS | 2/10 | Use fallback flags |
| Invalid duration | 0/10 | N/A |
| Unset timestamps | 1/10 | Use -fflags +genpts |

**Attached**: `stderr_*.log` files

---

### Gate 4: Target Client

**Primary client**: Chrome Desktop (estimated 80% of traffic)

**Implication**: HEVC must be transcoded (Chrome incompatible)
```

---

## Questions?

If unclear:
- Read [TECHNICAL_REVIEW_VOD_REMUX.md](TECHNICAL_REVIEW_VOD_REMUX.md) for context
- Ask before starting (alignment > speed)

---

## Bottom Line

**No rush. No complexity. Just run commands, paste output.**

We built the system to be data-driven. Your data makes it production-ready.

🎯 **Goal**: Replace "TODO: get from reviewer" with concrete values.

---

**Next**: When you're ready, run the commands above and send results (paste in email, attach files, or commit to repo).
