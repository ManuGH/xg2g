# ADR-001: HLS Delivery Strategy & Safari Compatibility

## Status

Accepted – Implemented – Production

## Context

Delivering Live TV (DVB / MPEG-TS sources) to modern web browsers—especially Safari—is inherently problematic.

Reasons:

- Broadcast streams are not web-native (variable bitrate, interlacing, loose timestamps).
- Safari enforces strict container, codec, and timing compliance.
- Naive passthrough leads to unstable playback, broken timelines, or decode failures.

Observed failure modes include:

- MediaError 3 (decode failure)
- MediaError 4 (unsupported source)

## Decision 1: Remux-First with Transcode Fallback

We adopt a Remux-First strategy and fall back to Transcoding only when required.

### Rationale

Why not “Original Passthrough”?
Directly exposing the broadcast stream to the browser is unreliable:

- Audio codecs: AC3, DTS, MP2 are common but not browser-safe.
- Video format: Interlaced H.264 (1080i) is frequent and problematic.
- GOP structure: Missing IDR frames at segment boundaries.
- Timing: Jittery or invalid PTS/DTS.

Result: Safari fails unpredictably.

### Strategy

1. **Probe the source**
2. **Remux if compatible**
   - No re-encoding
   - Original quality
   - Minimal CPU usage
3. **Transcode if incompatible**
   - Codec normalization
   - Deinterlacing if required
   - Safari-safe bitstream

This maximizes quality and performance without sacrificing reliability.

## Decision 2: Strict fMP4 for Safari

For Safari clients, we enforce Fragmented MP4 (fMP4) HLS.

### Why fMP4 (not MPEG-TS)?

- Clear codec signaling via init.mp4
- Better Safari stability
- Predictable decoder behavior
- Cleaner error semantics

### Scope

- **Live**: Always fMP4 for Safari.
- **Recording/VOD**: fMP4 used when Transcoding is active (e.g. for Safari); TS passthrough remains available for legacy/other clients.

### Enforced Properties

- **Container**: fMP4 (.m4s)
- **Initialization segment**: init.mp4
- **Video**:
  - H.264 (High Profile)
  - yuv420p
- **Audio**:
  - AAC-LC
- **MIME types**:
  - video/mp4 for init.mp4 and .m4s
- **Compression**:
  - No gzip / deflate (explicit Content-Encoding: identity)

This eliminates known Safari MediaError 4 causes.

## Decision 3: DVR-First Live Streaming

All Live TV streams expose a DVR-capable sliding window (PROGRAM-DATE-TIME + large retention window).

### Implementation

- **Live-DVR**:
  - HLS playlist type: None (Standard Live / Sliding Window)
  - `hls_list_size` based on DVRWindowSec (e.g., 4 hours)
  - `delete_segments` enabled to enforce retention policy
  - **Constraint**: `EVENT` type is avoided as it disables windowing in FFmpeg.
  - **Safari**: `PROGRAM-DATE-TIME` triggers DVR UI.
- **VOD/Recordings**:
  - HLS playlist type: VOD
  - Infinite Window (`hls_list_size 0`)
  - No segment deletion (preserves full integrity)

### Rationale

- **Live**: Infinite streams require a sliding window to prevent disk overflow.
- **VOD**: Recordings are finite; integrity (completeness) is prioritized over disk formatting.
- **Safari**: Handles both Sliding Window (Live) and Growing Playlist (VOD) correctly.

### Safari DVR Contract

- **Segment Duration**: 6 seconds (Live)
- **Window Length**: 2–4 hours
- **Timeline**: `#EXT-X-PROGRAM-DATE-TIME` present (triggers Time Machine UI)
- **Target Duration**: Correctly signaled
- **Compression**: No gzip/deflate on segments
- **Headers**: Proper MIME types (`video/mp4`) and `Content-Type`

### Robust DVB VOD Strategy

- **Input Cleaning**: Force seek (`-ss 3`) to skip initial DVB pre-buffer garbage (missing keyframes/PPS).
- **Audio Cleanliness**: No aggressive resampling (`async` disabled) to prevent distortion. Revert to standard format conversion.
- **Timestamp Normalization**: Global `-avoid_negative_ts make_zero` ensures valid start times for Safari.

### Consequence

- Retention policy is enforced for Live TV.
- VOD sessions consume disk proportional to content duration (accepted).

## Decision 4: System Start Verification

All release-critical validation is done via System Start, not Dev Start.

### System Start

`systemctl restart xg2g`

- Real systemd environment
- Real file limits
- True daemon lifecycle
- Correct I/O, locking, and timing behavior

### Dev Start

`make dev / run_dev.sh`

- Useful for logic iteration
- Insufficient for HLS/Safari validation

Production behavior must be verified under production conditions.

## Consequences

### Positive

- Stable Safari playback
- Predictable HLS behavior
- Efficient CPU usage via remux
- Unified Live/DVR/VOD pipeline

### Trade-offs

- Disk usage grows without retention
- Slight complexity in probe/decision logic
- Transcoding still required for “dirty” sources

These are intentional and accepted trade-offs.

## Status

- Safari playback verified stable
- MediaError 3/4 eliminated for compliant sources
- Remux/Transcode fallback verified in production
- DVR semantics confirmed functional

## Notes

This architecture intentionally prioritizes:

1. Playback stability
2. User experience
3. Operational predictability

over theoretical purity or raw passthrough.
