# Production Deployment Guide - xg2g GPU Transcoder

## 🎯 Overview

This guide covers the production deployment of the xg2g GPU transcoding system with AMD VAAPI hardware acceleration.

## ✅ System Requirements

- **GPU**: AMD GPU with VCN (Video Core Next) encoding support
  - Tested: AMD Radeon Graphics (gfx1103_r1)
  - Driver: Mesa Gallium 22.3.6+
- **OS**: Debian 12 (Bookworm) or compatible
- **Kernel**: 6.12.43+ with DRM 3.61+
- **Docker**: 20.10+ with docker-compose
- **FFmpeg**: 5.1.7+ with VAAPI support

## 📦 Quick Deployment

### 1. Clone and Build

```bash
cd /opt/stacks/xg2g-gpu
docker build -t xg2g-gpu-transcoder:production ./transcoder
```

### 2. Deploy Services

```bash
docker compose -f docker-compose.minimal.yml up -d
```

### 3. Verify Health

```bash
curl http://localhost:8081/health
```

Expected output:
```json
{
  "status": "ok",
  "vaapi_available": true,
  "version": "1.0.0"
}
```

## 🔧 Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VAAPI_DEVICE` | `/dev/dri/renderD128` | GPU device path |
| `VIDEO_BITRATE` | `5000k` | Video encoding bitrate |
| `AUDIO_CODEC` | `aac` | Audio codec (aac/mp3) |
| `AUDIO_BITRATE` | `192k` | Audio encoding bitrate |
| `AUDIO_CHANNELS` | `2` | Audio channel count |
| `ANALYZE_DURATION` | `500000` | FFmpeg analyzeduration (μs) |
| `PROBE_SIZE` | `500000` | FFmpeg probesize (bytes) |
| `RUST_LOG` | `info` | Logging level (debug/info/warn/error) |

### docker-compose.minimal.yml

```yaml
version: '3.8'

services:
  transcoder:
    image: xg2g-gpu-transcoder:production
    container_name: xg2g-transcoder
    restart: unless-stopped
    network_mode: host
    stdin_open: true
    tty: true
    devices:
      - /dev/dri:/dev/dri
    environment:
      RUST_LOG: info
      VAAPI_DEVICE: /dev/dri/renderD128
      VIDEO_BITRATE: 5000k
      AUDIO_CODEC: aac
      AUDIO_BITRATE: 192k
      AUDIO_CHANNELS: 2
      ANALYZE_DURATION: 500000
      PROBE_SIZE: 500000
```

## 🚀 Usage

### Transcoding Endpoint

**GET** `/transcode?source_url=<URL>`

Query Parameters:
- `source_url` (required): Input stream URL
- `video_bitrate` (optional): Override video bitrate
- `audio_bitrate` (optional): Override audio bitrate

Example:
```bash
curl --no-buffer "http://localhost:8081/transcode?source_url=http://10.10.55.57:17999/1:0:1:445D:453:1:C00000:0:0:0:" \
  | vlc -
```

**Important**: Use `--no-buffer` with curl to avoid buffering delays!

### Health Check

**GET** `/health`

Returns:
```json
{
  "status": "ok",
  "vaapi_available": true,
  "version": "1.0.0"
}
```

### Metrics

**GET** `/metrics`

Returns Prometheus-compatible metrics:
- Active transcoding sessions
- Success/error counts
- Request durations

## 📊 Performance

### Expected Results

- **Startup Time**: < 2 seconds
- **Encoding Speed**: 1.2x - 1.5x real-time
- **CPU Usage**: ~20-30% (decoding + deinterlacing)
- **GPU Usage**: ~40-60% (encoding)
- **Memory**: ~150MB per stream

### Benchmarks

Tested on AMD Radeon Graphics (gfx1103_r1):

| Input | Output | Speed | Quality |
|-------|--------|-------|---------|
| MPEG2 720x576@25fps | H.264 5Mbps | 1.2x | High Profile |
| DVB-S2 Stream | MPEG-TS | 1.4x | Level 4.1 |

## 🔍 Troubleshooting

### Issue: Container exits with code 0

**Cause**: stdin POLLHUP issue in detached mode

**Solution**: Ensure `stdin_open: true` and `tty: true` in docker-compose.yml

### Issue: 0 bytes received from transcode endpoint

**Cause**: Client buffering

**Solution**: Use `curl --no-buffer` or ensure client reads immediately

### Issue: FFmpeg hangs on startup

**Cause**: Problematic FFmpeg flags or wrong argument order

**Solution**: Verify `-init_hw_device` comes BEFORE `-i` in FFmpeg command

### Issue: VAAPI not available

**Cause**: GPU device not accessible

**Diagnostics**:
```bash
docker exec xg2g-transcoder vainfo
ls -la /dev/dri/
```

**Solution**: Ensure `/dev/dri` is mounted and has correct permissions

## 📝 Logging

View logs:
```bash
docker logs -f xg2g-transcoder
```

Set debug logging:
```bash
docker compose -f docker-compose.minimal.yml down
RUST_LOG=debug docker compose -f docker-compose.minimal.yml up -d
```

## 🔄 Updates

### Rebuild and Redeploy

```bash
cd /opt/stacks/xg2g-gpu
docker compose -f docker-compose.minimal.yml down
docker build --no-cache -t xg2g-gpu-transcoder:production ./transcoder
docker compose -f docker-compose.minimal.yml up -d
```

### Verify Update

```bash
docker logs xg2g-transcoder --tail 20
curl http://localhost:8081/health
```

## 🛡️ Security Considerations

1. **Network Isolation**: Uses host network for direct Enigma2 access
2. **GPU Access**: Container has access to `/dev/dri` (required for VAAPI)
3. **No External Exposure**: Transcoder listens on localhost only by default
4. **Process Isolation**: FFmpeg runs in separate process per request

## 📚 Technical Details

### FFmpeg Configuration

Minimal working configuration for live HTTP streams:

```bash
ffmpeg -hide_banner -loglevel error \
  -analyzeduration 500000 -probesize 500000 \
  -fflags +genpts+igndts+nobuffer \
  -init_hw_device vaapi=va:/dev/dri/renderD128 \
  -i <INPUT_URL> \
  -vf "yadif,format=nv12,hwupload" \
  -c:v h264_vaapi -b:v 5000k \
  -c:a aac -b:a 192k \
  -f mpegts pipe:1
```

**Critical Points**:
- `-init_hw_device` MUST come before `-i` for live streams
- Removed problematic flags: `-async 1`, `-start_at_zero`, `-avoid_negative_ts`
- Filter chain: CPU deinterlace (yadif) → GPU encode (hwupload)

### Architecture

```text
Enigma2 DVB-S2 Receiver
    ↓ (HTTP MPEG-TS)
Rust Transcoder (CPU Decode)
    ↓ (hwupload)
AMD GPU VAAPI (H.264 Encode)
    ↓ (MPEG-TS Stream)
HTTP Client
```

## 🎉 Success Criteria

Deployment is successful when:

1. ✅ Health endpoint returns `vaapi_available: true`
2. ✅ Transcode request returns H.264 MPEG-TS stream
3. ✅ Output is playable in VLC/ffplay
4. ✅ No errors in container logs
5. ✅ Startup time < 2 seconds

## 📞 Support

For issues or questions:
- Check logs: `docker logs xg2g-transcoder`
- Verify VAAPI: `docker exec xg2g-transcoder vainfo`
- Test FFmpeg: See troubleshooting section

---

**Last Updated**: 2025-10-21
**Version**: 1.0.0
**Status**: Production Ready ✅
