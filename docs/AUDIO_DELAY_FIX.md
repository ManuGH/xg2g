# Audio Delay Fix - Complete Documentation

**Date:** 2025-10-19
**Problem:** 3-6 seconds audio delay in Jellyfin Live-TV
**Status:** ✅ **SOLVED**

---

## Problem Description

### Symptoms

- **VLC:** Stream perfectly synced, no audio delay
- **Jellyfin + Safari:** 3-6 seconds audio delay
- **Only Live-TV** affected, normal videos OK

### Root Cause

1. Safari cannot natively play **MP2/AC3 audio**
2. Jellyfin must transcode audio live: **MP2 → AAC**
3. Video (H.264) is copied → **0ms latency**
4. Audio is transcoded → **3-6s latency**
5. **Result:** Picture-sound desynchronization

---

## Solution: xg2g Audio Transcoding + Hardware Encoding

### Architecture

**Before:**

```
Enigma2:17999 (MP2)
    ↓
Jellyfin (Audio transcoding MP2→AAC live)
    ↓
Safari (3-6s audio delay)
```

**After:**

```
Enigma2:17999 (MP2)
    ↓
xg2g:18000 (Audio transcoding MP2→AAC upfront)
    ↓
Jellyfin (Direct Play or Hardware Transcoding)
    ↓
Safari (No audio delay!)
```

---

## Implementation

### 1. xg2g Code Changes

**File:** `internal/proxy/transcoder.go` (Lines 70-92)

**Problem:** Enigma2 streams have broken DTS timestamps

**Fix:**

```go
// CRITICAL: Do NOT use -copyts!
// Enigma2 streams have broken DTS timestamps. We must regenerate them.
// Using -start_at_zero + -fflags genpts instead of -copyts fixes audio sync issues.
args := []string{
    "-hide_banner",
    "-loglevel", "error",
    "-fflags", "+genpts+igndts",        // Generate PTS, ignore broken DTS
    "-i", "pipe:0",
    "-map", "0:v", "-c:v", "copy",       // Copy video
    "-map", "0:a", "-c:a", t.config.Codec, // Transcode audio
    "-b:a", t.config.Bitrate,
    "-ac", fmt.Sprintf("%d", t.config.Channels),
    "-async", "1",                       // Audio-video sync
    "-start_at_zero",                    // Start timestamps at zero
    "-avoid_negative_ts", "make_zero",   // Fix negative timestamps
    "-muxdelay", "0",
    "-muxpreload", "0",
    "-f", "mpegts",
    "pipe:1",
}
```

**Critical:**

- ❌ **NOT** `-copyts` (copies broken timestamps)
- ✅ **Instead** `-start_at_zero` (generates new ones)

### 2. Configuration

**Environment Variables:**

```bash
XG2G_ENABLE_STREAM_PROXY=true
XG2G_PROXY_PORT=18000
XG2G_PROXY_TARGET=http://10.10.55.57:17999
XG2G_STREAM_BASE=http://10.10.55.50:18000
XG2G_ENABLE_AUDIO_TRANSCODING=true
XG2G_AUDIO_CODEC=aac
XG2G_AUDIO_BITRATE=192k
XG2G_AUDIO_CHANNELS=2
```

---

## Results

### Performance Comparison

#### Before (Software only)

- ❌ 3-6 seconds audio delay
- CPU: ~26 fps @ 1.04x speed
- Transcoding: MP2 → AAC in Jellyfin (live)

#### After (With xg2g audio transcoding)

- ✅ **No audio delay!**
- GPU: ~28 fps @ 1.12x speed (8% faster!)
- Lower CPU load
- Audio: AAC Direct Play (no transcoding in Jellyfin)

### By Stream Type

**720p Progressive (e.g., Das Erste HD):**

```
Playback Info:
├── Method: Direct Play
├── Video: H264 (copy)
├── Audio: AAC (copy) ← from xg2g!
└── Transcoding: None
Performance: Minimal (only remuxing)
```

**1080i Interlaced (e.g., Sky Sport News):**

```
Playback Info:
├── Method: Transcoding
├── Video: H264 (h264_vaapi + yadif)
├── Audio: AAC (copy) ← from xg2g!
├── Bitrate: 20.2 Mbps
├── FPS: 28 fps @ 1.12x
└── Transcode Reason: Container + Interlaced
Performance: GPU-accelerated
```

---

## Known Issues & Workarounds

### ⚠️ Timestamp Warnings Remain

**Symptom:** FFmpeg shows DTS warnings in logs
**Impact:** **None** - Software deinterlacing tolerates these
**Action:** Ignore, works anyway!

---

## Test Commands

### Test xg2g Stream

```bash
# Check if AAC audio is present
curl -s 'http://10.10.55.50:18000/1:0:19:283D:3FB:1:C00000:0:0:0:' | \
  ffprobe -v error -show_entries stream=codec_name -of csv=p=0 -

# Expected output:
# h264
# aac
```

### Test Timestamp Issues

```bash
# Should show DTS warnings (is OK!)
timeout 10 curl -s 'http://10.10.55.50:18000/1:0:19:6C:C:85:C00000:0:0:0:' | \
  ffmpeg -v error -i - -t 5 -f null - 2>&1 | \
  grep -E 'dts|Application'
```

---

## What Works ✅

### 1. Audio Delay Completely Fixed ✅

- xg2g transcodes MP2 → AAC upfront
- Jellyfin does Direct Play (no audio transcoding)
- **No audio delay anymore!**

### 2. Hardware Encoding Active ✅

- H264-VAAPI for GPU-accelerated encoding
- Hybrid: Software deinterlacing + Hardware encoding
- Faster than pure software encoding

### 3. Lower CPU Load ✅

- GPU handles video encoding
- CPU only for deinterlacing
- More headroom for other services

### 4. Higher Performance ✅

- 28 fps @ 1.12x (instead of 26 fps @ 1.04x)
- 8% faster transcoding
- Smoother playback

### 5. Stable ✅

- No more crashes
- Works for 720p and 1080i
- Audio always synchronized

---

## Summary

**Problem Solved:** ✅ 3-6s audio delay completely fixed

**Optimal Configuration:**

- xg2g: Audio transcoding (MP2 → AAC)
- Jellyfin: VAAPI H264 encoding + yadif deinterlacing
- Container: MPEG-TS (not fMP4)
- TunerCount: 4 simultaneous streams

**Performance:**

- Direct Play: Minimal (only xg2g audio transcoding)
- Transcoding: GPU-accelerated (~28 fps @ 1.12x)
- Audio: Always synchronized, no delay

**Status:** Production-ready, stable, tested ✅
