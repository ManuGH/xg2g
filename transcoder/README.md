# xg2g-transcoder

**High-performance GPU transcoder for xg2g using VAAPI**

Rust-basierter Transcoding-Service mit Hardware-Beschleunigung für AMD/Intel GPUs.

---

## Features

- ⚡ **VAAPI Hardware-Beschleunigung** (AMD/Intel GPUs)
- 🎯 **H.264 GPU Encoding** mit `h264_vaapi`
- 🔄 **Deinterlacing auf GPU** mit `deinterlace_vaapi`
- 🚀 **Schneller Stream-Start** (2-3 Sekunden)
- 📡 **HTTP API** für einfache Integration
- 🔧 **Timestamp-Korrektur** für Enigma2-Streams
- 📊 **Health Checks** mit VAAPI-Status

---

## Quick Start

```bash
# Build
docker build -t xg2g-transcoder .

# Run
docker run -d \
  --name transcoder \
  --device /dev/dri:/dev/dri \
  --group-add video \
  -p 8081:8081 \
  -e VAAPI_DEVICE=/dev/dri/renderD128 \
  -e VIDEO_BITRATE=5000k \
  xg2g-transcoder

# Test
curl http://localhost:8081/health
```

---

## API

### `GET /health`

Health check with VAAPI status.

**Response:**

```json
{
  "status": "ok",
  "vaapi_available": true,
  "version": "2.0.1"
}
```

### `GET /metrics`

Prometheus metrics endpoint.

**Response:** Prometheus text format

**Example metrics:**

```text
xg2g_transcoder_requests_total 1234
xg2g_transcoder_success_total 1200
xg2g_transcoder_errors_total 34
xg2g_transcoder_active_sessions 5
xg2g_transcoder_duration_seconds_sum 4567.8
xg2g_transcoder_bytes_total 123456789012
```

### `GET /transcode?source_url=<url>`

Transcode a stream from the given URL.

**Parameters:**

- `source_url` (required): Source stream URL
- `video_bitrate` (optional): Override video bitrate (e.g., `3000k`)
- `audio_bitrate` (optional): Override audio bitrate (e.g., `128k`)

**Response:** MPEG-TS stream (`video/mp2t`)

**Example:**

```bash
curl "http://localhost:8081/transcode?source_url=http://enigma2:17999/stream" | ffplay -
```

### `POST /transcode/stream`

Transcode a stream from request body (stdin).

**Request:** MPEG-TS stream in body
**Response:** Transcoded MPEG-TS stream

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VAAPI_DEVICE` | `/dev/dri/renderD128` | VAAPI device path |
| `VIDEO_BITRATE` | `5000k` | Video bitrate (3000k=SD, 5000k=HD, 8000k=FHD) |
| `AUDIO_CODEC` | `aac` | Audio codec |
| `AUDIO_BITRATE` | `192k` | Audio bitrate |
| `AUDIO_CHANNELS` | `2` | Audio channels (2=stereo) |
| `ANALYZE_DURATION` | `2000000` | FFmpeg analyzeduration (microseconds) |
| `PROBE_SIZE` | `2000000` | FFmpeg probesize (bytes) |
| `RUST_LOG` | `info` | Log level (debug, info, warn, error) |
| `FFMPEG_PATH` | `ffmpeg` | Path to FFmpeg binary |

---

## Development

### Prerequisites

- Rust 1.84+
- FFmpeg 7.0+ with VAAPI support
- AMD/Intel GPU with VAAPI drivers

### Build

```bash
cargo build --release
```

### Run

```bash
# Set environment
export VAAPI_DEVICE=/dev/dri/renderD128
export VIDEO_BITRATE=5000k
export RUST_LOG=debug

# Run
cargo run --release
```

### Test

```bash
# Health check
curl http://localhost:8081/health

# Transcode test stream
curl "http://localhost:8081/transcode?source_url=http://devimages.apple.com/iphone/samples/bipbop/bipbopall.m3u8" \
  | ffplay -

# Test with vainfo
vainfo
```

---

## Architecture

```
┌─────────────────────────────────────────┐
│         Axum HTTP Server                │
│         (Port 8081)                     │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│      VaapiTranscoder                    │
│  • FFmpeg Process Management            │
│  • VAAPI Hardware Acceleration          │
│  • Stream Processing                    │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│           FFmpeg                        │
│  • Input: HTTP/Pipe                     │
│  • Decode: VAAPI                        │
│  • Deinterlace: deinterlace_vaapi       │
│  • Encode: h264_vaapi                   │
│  • Output: MPEG-TS                      │
└─────────────────────────────────────────┘
```

---

## FFmpeg Command

The transcoder generates the following FFmpeg command:

```bash
ffmpeg \
  -analyzeduration 2000000 -probesize 2000000 \
  -fflags +genpts+igndts \
  -i <source_url> \
  -hwaccel vaapi \
  -hwaccel_device /dev/dri/renderD128 \
  -hwaccel_output_format vaapi \
  -filter_hw_device /dev/dri/renderD128 \
  -vf deinterlace_vaapi \
  -c:v h264_vaapi -b:v 5000k -maxrate 5000k -bufsize 10M \
  -c:a aac -b:a 192k -ac 2 \
  -async 1 -start_at_zero -avoid_negative_ts make_zero \
  -muxdelay 0 -muxpreload 0 \
  -f mpegts pipe:1
```

**Key features:**

- Fast stream analysis (2s instead of 15s)
- Timestamp regeneration (`+genpts+igndts`)
- GPU deinterlacing and encoding
- CBR-like encoding for stable streaming

---

## Troubleshooting

### VAAPI not available

```bash
# Check device
ls -la /dev/dri/

# Test VAAPI
vainfo

# Install drivers (Debian/Ubuntu)
sudo apt install mesa-va-drivers intel-media-va-driver

# Add user to video group
sudo usermod -a -G video $USER
sudo usermod -a -G render $USER
```

### Permission denied

```bash
# Check permissions
ls -la /dev/dri/renderD128

# Should show: crw-rw----+ 1 root video

# Add to group
sudo usermod -a -G video $USER
```

### FFmpeg errors

```bash
# Check FFmpeg VAAPI support
ffmpeg -hwaccels

# Should list: vaapi

# Test encoding
ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 \
  -i input.mp4 -c:v h264_vaapi output.mp4
```

---

## Performance

### Benchmark (AMD RX 7900 XTX)

| Resolution | Bitrate | CPU | GPU | Streams |
|-----------|---------|-----|-----|---------|
| 720p | 3 Mbps | 5% | 30% | 10+ |
| 1080i | 5 Mbps | 8% | 45% | 8-10 |
| 1080p | 8 Mbps | 12% | 60% | 6-8 |

### Startup Time

- **Without GPU:** 15-20 seconds
- **With GPU:** 2-3 seconds

---

## License

Same as xg2g main project.

---

## Links

- [Main xg2g Repository](https://github.com/ManuGH/xg2g)
- [GPU Transcoding Documentation](../GPU_TRANSCODING.md)
- [FFmpeg VAAPI Guide](https://trac.ffmpeg.org/wiki/Hardware/VAAPI)
