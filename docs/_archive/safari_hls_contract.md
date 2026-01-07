# Safari TS-HLS Stream Contract

This document defines the strict requirements for MPEG-TS HLS streams to ensure reliable playback on Safari clients. Any deviation from this contract is known to cause playback instability (stalls, "still frame" issues, MediaError 3).

## 1. Playlist Semantics

* **Type**: Must be `#EXT-X-PLAYLIST-TYPE:EVENT` for live streams (or VOD).
* **DVR Window**: Must retain prior segments (no sliding window pruning) to support rewinding and avoid "live-only" UI.
* **Timestamps**: `#EXT-X-PROGRAM-DATE-TIME` must use strict RFC 3339 formatting.
  * **Allowed**: `2026-01-04T16:00:00Z`, `2026-01-04T16:00:00+00:00`
  * **Forbidden**: `2026-01-04T16:00:00+0000` (ISO 8601 compact offset) - *Server normalizer enforces this.*
* **Independency**: `#EXT-X-INDEPENDENT-SEGMENTS` tag must be present.

## 2. Video Bitstream (H.264)

* **Codecs**: H.264 (High Profile) recommended.
* **Segment Boundary**: Every segment **MUST** start with a clean IDR frame (`key_frame=1`, `pict_type=I`).
  * FFmpeg args: `scenecut=0`, `keyint` == `min-keyint` == `GOP size`.
* **Parameter Sets**: SPS (NAL 7) and PPS (NAL 8) **MUST** be repeated in-band at the start of every segment.
  * FFmpeg args: `-x264-params repeat-headers=1`.
  * Muxer args: `-mpegts_flags +resend_headers+pat_pmt_at_frames`.
* **Timestamps**:
  * DTS/PTS must be strictly monotonic across segment boundaries.
  * `avoid_negative_ts=make_zero` to prevent initial negative timestamps.

## 3. Audio Bitstream (AAC)

* **Codec**: AAC-LC (`-profile:a aac_low`). Use of HE-AAC is discouraged for live HLS on Safari.
* **Sample Rate**: Locked to **48000 Hz** (`-ar 48000`).
* **Channels**: Locked to **Stereo** (`-ac 2`). 5.1/Surround can cause mix-down issues or stalls if not handled perfectly.

## 4. Delivery

* **Headers**:
  * Playlist: `Content-Type: application/vnd.apple.mpegurl`
  * Segment: `Content-Type: video/mp2t` (lowercase preferred)
  * Cache: `Cache-Control: no-store` (for live playlist)
* **Compression**: **NO** gzip/deflate on `.ts` segments. Identity encoding only.

## 5. Verification

To verify compliance:

1. **Check Manifest**: `curl -v index.m3u8` -> Verify `Z`/`+HH:mm` in DATE-TIME.
2. **Check Segment Start**: `ffprobe -show_frames -select_streams v:0 seg_0.ts | grep key_frame=1` (Must be first frame).
3. **Check Audio**: `ffprobe -show_streams seg_0.ts` -> Verify `aac`, `48000`, `stereo`.

## Browser Compatibility Fallback

To support browsers with strict HLS requirements (e.g., Safari), the backend implements a sticky fallback strategy.

### Detection & Trigger

If a client reports a specific decode error (MediaError 3), the backend can permanently switch the session to an fMP4 container.

1. Client encounters error (e.g., `MEDIA_ERR_DECODE`).
2. Client sends feedback via `POST /api/v3/sessions/{sessionId}/feedback` with `event="error"` and `code=3`.
3. Backend marks the session as `FallbackReason="client_report:code=3"` and sets `Profile.Container="fmp4"`.
4. Backend restarts the stream pipeline automatically.
5. Future starts for this session ID will use the fMP4 profile.

### Persistence

The fallback state is sticky per session. Once activated, the session remains in fMP4 mode to prevent oscillating errors. Idempotency ensures that multiple error reports do not cause multiple restarts.
