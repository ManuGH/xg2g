# Enigma2 Streaming Configuration Guide

This document details the critical configuration required for stable streaming from Enigma2 receivers using `xg2g`.

## Core Principle: Receiver-Resolved Stream URLs

For live playback, `xg2g` asks the receiver's OpenWebIF API to resolve the
effective stream URL for a service reference before FFmpeg starts. This is
critical because:

1. **Transcoding**: The receiver decides if and how to transcode the stream based on its capabilities and the request parameters.
2. **Port Mapping**: The correct streaming port (8001, 8002, 17999, etc.) is determined by the receiver's configuration.
3. **Authentication**: The `/web/stream.m3u` endpoint handles session tokens and anti-hijack tokens if configured.

### Critical Configuration Rule

> Do not set `XG2G_E2_STREAM_PORT` or `enigma2.streamPort` unless you are
> intentionally overriding the direct fallback port.

**Default Behavior (Recommended):**

- **Operator config**: leave `enigma2.streamPort` unset.
- **Registry default**: `enigma2.streamPort` remains `8001` for compatibility
  and is marked deprecated.
- **Live playback mechanism**: `xg2g` queries
  `/web/stream.m3u?ref=<ServiceRef>`, follows the URL returned by the receiver,
  and only builds a direct URL from `streamPort` if that receiver-side
  resolution fails.

**Direct Fallback Override (Advanced):**

- **Setting**: `enigma2.streamPort: 8001` or another explicit direct stream
  port.
- **Mechanism**: live playback still asks `/web/stream.m3u` first; if that
  fails, `xg2g` constructs `http://<ip>:<streamPort>/<ServiceRef>`.
- **Risk**: the fallback can bypass receiver relay logic and may fail when the
  actual stream is exposed through optional middleware such as port `17999`.

## Credentials & Authentication

Even if streaming ports (like 8001) are often unauthenticated, `xg2g` requires valid credentials to:

1. Query the `/web/stream.m3u` endpoint.
2. Inject basic auth into the resolved stream URL for FFmpeg (e.g., `http://user:pass@ip:port/...`) if the receiver requires it.

**Required Environment Variables:**
You must provide the login for your specific receiver. The system does not use hardcoded credentials.

```bash
export XG2G_E2_USER="root"              # Default for most OpenWebIf setups
export XG2G_E2_PASS="YOUR_PASSWORD"     # Your specific receiver password
```

## Troubleshooting Common Errors

### `R_PIPELINE_START_FAILED`

**Cause**: The streaming pipeline (FFmpeg) could not start.
**Fix**:

- Check logs for "exec: no command". Pass `XG2G_FFMPEG_BIN` or ensure `ffmpeg` is in `$PATH`.
- Check for `ResolveStreamURL called`, `Stream URL resolved via OpenWebIF`, or
  `OpenWebIF stream resolution failed, falling back to direct stream URL` in
  logs. A direct fallback warning usually means the receiver playlist endpoint
  failed before FFmpeg started.

### `R_PACKAGER_FAILED` w/ "No such file or directory"

**Cause**: FFmpeg tried to open the ServiceRef (`1:0:19...`) as a local file because it wasn't a valid URL.
**Fix**:

1. Verify `XG2G_E2_HOST`, `XG2G_E2_USER`, and `XG2G_E2_PASS` are correct so
   `ResolveStreamURL` can call OpenWebIF.
2. Leave `XG2G_E2_STREAM_PORT` unset unless you are deliberately testing a
   direct fallback.
3. If using a direct fallback port, ensure that port is reachable from the
   `xg2g` runtime host.

## HLS Segment Configuration (ADR-011)

`xg2g` uses a unified segmentation policy to ensure compatibility with Safari and iOS. This is configurable via:

- **`HLS.SegmentSeconds` (Default: 6)**: The target duration for each `.ts` or `.m4s` segment.
- **Setting**: `6` for standard performance, `1` for low-latency channel grazing.

**Example `config.yaml`:**

```yaml
hls:
  segmentSeconds: 6
```

> [!IMPORTANT]
> Changing this value mid-session will cause a buffer reset in most players.

## Adaptive Quality Budgets

Live sessions requested with the high-quality intent can be promoted from the
conservative HQ25 budget to HQ50 when the selected codec and transport are safe
for that client path. This is a runtime FFmpeg hardening step, not a static
profile rewrite.

Default adaptive ceilings:

| Codec | Default maxrate | Default bufsize | Transport rule |
| :--- | :--- | :--- | :--- |
| AV1 | `14000k` | `28000k` | fMP4 only |
| HEVC | `14000k` | `28000k` | fMP4 only |
| H.264 / x264 | `16000k` | `32000k` | MPEG-TS or fMP4 |

Controls:

```bash
XG2G_ADAPTIVE_QUALITY_ENABLED=true
XG2G_ADAPTIVE_AV1_QUALITY_ENABLED=true
XG2G_ADAPTIVE_HEVC_QUALITY_ENABLED=true
XG2G_ADAPTIVE_H264_QUALITY_ENABLED=true
XG2G_ADAPTIVE_AV1_MAXRATE_K=14000
XG2G_ADAPTIVE_AV1_BUFSIZE_K=28000
XG2G_ADAPTIVE_HEVC_MAXRATE_K=14000
XG2G_ADAPTIVE_HEVC_BUFSIZE_K=28000
XG2G_ADAPTIVE_H264_MAXRATE_K=16000
XG2G_ADAPTIVE_H264_BUFSIZE_K=32000
```

The adaptive path does not override explicit HQ25 caps or service-reference
runtime overrides. AV1 keeps the legacy `XG2G_ADAPTIVE_AV1_QUALITY_ENABLED`
switch for existing deployments.

## DVR Window Disk Budget

The live DVR is a **rolling** window: `XG2G_HLS_DVR_WINDOW` controls how far back
you can rewind. ffmpeg keeps `ceil(window / segmentSeconds)` segments on disk and
prunes the oldest as new ones arrive (`-hls_flags delete_segments`), so an active
session never grows past **one window's worth**, and its segments are freed when
the session ends. Disk to provision therefore scales with `window × bitrate ×
concurrent streams`.

Rule of thumb (decimal GB):

```
GB per stream ≈ total_bitrate_Mbit × window_seconds ÷ 8000
```

For the **4 h 30 m** window (`XG2G_HLS_DVR_WINDOW=4h30m` → 16 200 s) at a typical
1080p sustained rate (video + ~0.2 Mbit AAC), per active stream and for **4
concurrent streams**:

| Codec | Typical sustained | GB / stream (4:30) | 4 streams |
| :--- | :--- | :--- | :--- |
| AV1 | ~5 Mbit/s | ~10.5 GB | **~42 GB** |
| HEVC (H.265) | ~7 Mbit/s | ~14.6 GB | **~58 GB** |
| H.264 / x264 | ~10 Mbit/s | ~20.6 GB | **~82 GB** |

Worst case, if a session sustains the configured **maxrate ceiling** (AV1/HEVC
`14000k`, H.264 `16000k`, see *Adaptive Quality Budgets* above):

| Codec | At maxrate | GB / stream (4:30) | 4 streams |
| :--- | :--- | :--- | :--- |
| AV1 / HEVC | 14 Mbit/s | ~28.7 GB | **~115 GB** |
| H.264 / x264 | 16 Mbit/s | ~32.8 GB | **~131 GB** |

Notes:

- **Plan for the worst case.** A pure-AV1 household is ~42 GB; a worst-case
  H.264-at-cap household is ~131 GB. Provision for the codecs and quality you
  actually run.
- **Direct-play (copy) channels** are not transcoded — their on-disk size equals
  the *source* bitrate (e.g. a 720p broadcast at ~3–5 Mbit/s ≈ 6–10 GB/stream for
  4:30), independent of the codec rows above.
- AV1's figure depends on the QVBR quality knob (`XG2G_AV1_QVBR_QUALITY`); an
  aggressive high-bitrate setting moves AV1 toward the maxrate row.
- These cover live DVR segments only — recordings are accounted separately.

## Summary Checklist

- [ ] `XG2G_E2_STREAM_PORT` is unset unless a direct fallback override is intentional.
- [ ] `XG2G_E2_USER` and `XG2G_E2_PASS` are set.
- [ ] `XG2G_FFMPEG_BIN` points to a valid binary (or `ffmpeg` is in PATH).

Legacy receiver env aliases such as `XG2G_STREAM_PORT` now fail startup and should be removed instead of carried forward.
