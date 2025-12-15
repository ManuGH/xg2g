# Low-Latency HLS (LL-HLS)

LL-HLS is an **optional** HLS variant that reduces latency by using fragmented MP4 segments (`.m4s`) and partial segments.

## How to enable

LL-HLS is **opt-in** via query parameter:

```
http://<xg2g-proxy-host>:18000/hls/<serviceRef>/playlist.m3u8?llhls=1
```

If `llhls=1` is not set, xg2g serves the default HLS profile.

## Requirements

- iOS 14+ / macOS 11+ / Safari 14+ (LL-HLS capable clients)
- `ffmpeg` available inside the xg2g runtime (or override path)

## Configuration (ENV)

```bash
# Segment duration in seconds (1-2, default: 1)
XG2G_LLHLS_SEGMENT_DURATION=1

# Number of segments in playlist (6-10, default: 6)
XG2G_LLHLS_PLAYLIST_SIZE=6

# Pre-buffer segments before serving playlist (1-3, default: 2)
XG2G_LLHLS_STARTUP_SEGMENTS=2

# Partial segment size in bytes (default: 262144)
XG2G_LLHLS_PART_SIZE=262144

# FFmpeg path override (fallback: XG2G_WEB_FFMPEG_PATH)
XG2G_LLHLS_FFMPEG_PATH=/usr/bin/ffmpeg
```

### Optional: HEVC inside LL-HLS

```bash
XG2G_LLHLS_HEVC_ENABLED=true
XG2G_LLHLS_HEVC_ENCODER=hevc_vaapi
XG2G_LLHLS_HEVC_BITRATE=6000k
XG2G_LLHLS_HEVC_PEAK=8000k
XG2G_LLHLS_HEVC_PROFILE=main
XG2G_LLHLS_HEVC_LEVEL=5.0
XG2G_LLHLS_VAAPI_DEVICE=/dev/dri/renderD128
```

## Troubleshooting

- If you see `llhls profile not ready`, check `ffmpeg` availability and permissions.
- If playback fails only in Safari: try without `llhls=1` first to confirm baseline HLS works.
