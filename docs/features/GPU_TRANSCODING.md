# GPU Transcoding

> [!WARNING]
> **GPU Transcoding is currently disabled / experimental.**
> The instructions below describe the intended architecture but the feature is currently disabled in the official Docker image.
 Integration Guide

## Overview

xg2g now supports full GPU-accelerated transcoding for Live TV streams via an integrated Rust service using VAAPI (AMD/Intel GPUs). This feature solves audio-video synchronization issues and provides hardware-accelerated transcoding for better performance.

## Architecture

### Before GPU Transcoding

```
Enigma2 → xg2g Proxy (Audio: MP2→AAC) → Jellyfin → Client
                                          ↓
                                    HEVC Transcode
                                          ↓
                                    Audio Delay! ❌
```

### With GPU Transcoding

```
Enigma2 → xg2g Proxy → Rust GPU Transcoder (VAAPI) → Client
           ↓                    ↓
      URL Delegation      Full Video+Audio
                          Deinterlacing
                          Timestamp Regen
                                ↓
                          Synchronized ✅
```

## Features

- **Full Video+Audio Transcoding**: Not just audio - complete stream processing
- **Hardware Acceleration**: Uses AMD/Intel VAAPI for GPU encoding
- **Deinterlacing**: Automatic yadif deinterlacing for 1080i content
- **Timestamp Regeneration**: Fixes broken DTS timestamps from Enigma2
- **Audio Sync**: Perfect audio-video synchronization
- **Priority Cascade**: Automatic fallback to audio-only or direct proxy on errors

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_GPU_TRANSCODE` | `false` | Enable GPU transcoding |
| `XG2G_TRANSCODER_URL` | `http://localhost:8085` | GPU transcoder service URL |
| `XG2G_ENABLE_AUDIO_TRANSCODING` | `false` | Must be `true` for GPU transcoding |
| `XG2G_ENABLE_STREAM_PROXY` | `false` | Must be `true` for GPU transcoding |
| `XG2G_STREAM_BASE` | - | Proxy URL (e.g., `http://10.10.55.50:18000`) |

### Docker Compose Example

```yaml
version: '3.8'

services:
  # Rust GPU Transcoder Service
  transcoder:
    image: xg2g-gpu-transcoder:production
    container_name: xg2g-transcoder
    restart: unless-stopped
    network_mode: host
    devices:
      - /dev/dri:/dev/dri
    environment:
      RUST_LOG: info
      VAAPI_DEVICE: /dev/dri/renderD128
      VIDEO_BITRATE: 5000k
      AUDIO_CODEC: aac
      AUDIO_BITRATE: 192k
      AUDIO_CHANNELS: 2
      PORT: 8085

  # xg2g Main Service (Go)
  xg2g:
    image: xg2g:latest
    container_name: xg2g
    restart: unless-stopped
    network_mode: host
    volumes:
      - xg2g-data:/data
    environment:
      # Enigma2 Receiver
      XG2G_OWI_BASE: http://10.10.55.57
      XG2G_OWI_USER: root
      XG2G_OWI_PASS: yourpassword
      XG2G_BOUQUET: Premium

      # HDHomeRun
      XG2G_HDHR_ENABLED: "true"
      XG2G_HDHR_FRIENDLY_NAME: xg2g-gpu

      # Stream Proxy with GPU Transcoding
      XG2G_ENABLE_STREAM_PROXY: "true"
      XG2G_PROXY_PORT: 18000
      XG2G_PROXY_TARGET: http://10.10.55.57:17999
      XG2G_STREAM_BASE: http://YOUR_SERVER_IP:18000

      # GPU Transcoding
      XG2G_ENABLE_AUDIO_TRANSCODING: "true"
      XG2G_GPU_TRANSCODE: "true"
      XG2G_TRANSCODER_URL: http://localhost:8085
    depends_on:
      - transcoder

volumes:
  xg2g-data:
    driver: local
```

## How It Works

### Request Flow

1. **Client Request**: Jellyfin/Plex requests stream via HDHomeRun lineup
2. **Proxy Routing**: xg2g Go service routes to appropriate handler
3. **Priority Check**:
   - If `XG2G_GPU_TRANSCODE=true` → Route to GPU Transcoder
   - Else if `XG2G_ENABLE_AUDIO_TRANSCODING=true` → FFmpeg audio-only
   - Else → Direct proxy to Enigma2

### GPU Transcoding Pipeline

```
xg2g Proxy (Port 18000)
    ↓
ProxyToGPUTranscoder()
    ↓
HTTP GET → Rust GPU Transcoder (Port 8085)
    ↓
/transcode?source_url=http://enigma2:17999/...
    ↓
Rust Service:
  1. Fetch MPEG-TS from Enigma2
  2. FFmpeg with VAAPI:
     - Hardware decode (H.264)
     - Deinterlace (yadif)
     - Hardware encode (H.264 or copy)
     - Audio transcode (MP2/AC3 → AAC)
     - Timestamp regeneration
  3. Stream MPEG-TS output
    ↓
Client receives synchronized stream
```

### Fallback Mechanism

**Priority Cascade**:

1. **GPU Transcoding** (if enabled and available)
   - On error → Logs error and falls back to #2
2. **Audio-Only Transcoding** (if enabled)
   - Uses FFmpeg in Go service
   - On error → Falls back to #3
3. **Direct Proxy**
   - No transcoding, direct pass-through
   - Always succeeds

## Deployment

### Prerequisites

1. **AMD or Intel GPU** with VAAPI support
2. **Docker** with GPU access configured
3. **Rust GPU Transcoder** image built
4. **xg2g** image with GPU transcoding support

### Build Images

```bash
# Build Rust GPU Transcoder
cd transcoder
docker build -t xg2g-gpu-transcoder:production .

# Build xg2g Go Service
cd ..
docker build -t xg2g:latest .
```

### Start Services

```bash
docker compose -f docker-compose.minimal.yml up -d
```

### Verify Deployment

```bash
# Check GPU Transcoder health
curl http://localhost:8085/health
# Expected: {"status":"ok","vaapi_available":true,"version":"1.0.0"}

# Check xg2g API
curl http://localhost:8080/api/status
# Expected: {"status":"ok",...}

# Check logs for GPU transcoding
docker logs xg2g 2>&1 | grep "GPU transcoding enabled"
# Expected: "GPU transcoding enabled (full video+audio)"

# Test stream through proxy
timeout 10 curl -s "http://localhost:18000/CHANNEL_REF" | head -c 1000000
# Should receive ~1MB of MPEG-TS data
```

## Jellyfin Integration

### Tuner Configuration

1. **Add Live TV Tuner**:
   - Go to: Dashboard → Live TV → Tuners
   - Select: "HDHomeRun" (auto-discovered)
   - Or manually add: `http://YOUR_SERVER_IP:8080`

2. **Tuner Settings**:
   - **Tuner Type**: HDHomeRun
   - **Streams**: Check all available channels
   - **EPG Source**: Use XMLTV from xg2g

3. **Transcoding Settings** (Dashboard → Playback → Transcoding):
   - **Hardware Acceleration**: VAAPI (AMD/Intel)
   - **Enable Hardware Encoding**: Yes
   - **Codecs**: H.264, HEVC
   - **Deinterlacing**: Hardware (VAAPI)

### Expected Behavior

- **Direct Play**: Stream passes through GPU transcoder (deinterlacing + audio sync)
- **Jellyfin Transcoding**: Adaptive bitrate for remote clients (H.264 → HEVC)
- **Audio**: Always synchronized with video
- **Performance**: GPU handles transcoding, low CPU usage

## Monitoring

### Log Messages

#### Startup

```
INFO: GPU transcoding enabled (full video+audio)
transcoder_url="http://localhost:8085"
```

#### Stream Request (Debug Level)

```
DEBUG: routing stream through GPU transcoder
path="/1:0:19:283D:3FB:1:C00000:0:0:0:"
target="http://10.10.55.57:17999/..."
```

```
DEBUG: proxying to GPU transcoder
source_url="http://10.10.55.57:17999/..."
transcoder_url="http://localhost:8085/transcode?source_url=..."
```

#### GPU Transcoder Logs

```
DEBUG: Starting transcoding for source: http://10.10.55.57:17999/...
DEBUG: FFmpeg: Stream #0:0: Video: h264
DEBUG: FFmpeg: Stream #0:1: Audio: aac
```

### Performance Metrics

Check GPU usage:

```bash
# AMD GPU
radeontop

# Intel GPU
intel_gpu_top

# Generic
watch -n 1 'cat /sys/class/drm/card*/device/gpu_busy_percent'
```

Check CPU usage:

```bash
docker stats xg2g xg2g-transcoder
```

Expected with GPU transcoding:

- **CPU**: 5-15% per stream
- **GPU**: 30-60% per stream
- **Memory**: ~200MB total

Without GPU (Jellyfin software transcoding):

- **CPU**: 80-150% per stream ❌
- **Memory**: ~500MB+ per stream ❌

## Troubleshooting

### GPU Transcoder Not Starting

**Symptom**: `docker logs xg2g-transcoder` shows errors

**Check**:

```bash
# Verify GPU device exists
ls -la /dev/dri/renderD128

# Check permissions
docker run --rm --device /dev/dri:/dev/dri alpine ls -la /dev/dri

# Test VAAPI
docker exec xg2g-transcoder vainfo
```

**Solution**:

- Ensure GPU drivers are installed
- Add user to `render` group: `usermod -aG render $USER`
- Restart Docker daemon

### Streams Not Using GPU Transcoder

**Symptom**: No "routing stream through GPU transcoder" in logs

**Check**:

```bash
# Verify configuration
docker exec xg2g printenv | grep -i transcode

# Check transcoder connectivity
docker exec xg2g curl -s http://localhost:8085/health
```

**Solution**:

- Ensure `XG2G_GPU_TRANSCODE=true`
- Ensure `XG2G_ENABLE_AUDIO_TRANSCODING=true`
- Restart xg2g service: `docker restart xg2g`

### Audio Still Out of Sync

**Symptom**: Audio delay persists in Jellyfin playback

**Check**:

1. Verify GPU transcoding is active (check logs)
2. Test direct stream (bypass Jellyfin transcoding)
3. Check Jellyfin transcoding settings

**Solution**:

```bash
# Test direct GPU transcoder stream
timeout 30 curl -s "http://localhost:8085/transcode?source_url=http://ENIGMA2:17999/CHANNEL" | \
  ffprobe -v error -show_entries stream=codec_name -of csv=p=0 -

# Expected output:
# h264
# aac
```

If audio is synchronized with direct GPU transcoder but not with Jellyfin:

- Disable Jellyfin transcoding for Live TV (use Direct Play)
- Or adjust Jellyfin buffer settings

### High CPU Usage

**Symptom**: CPU usage still high with GPU transcoding enabled

**Check**:

```bash
# Verify GPU is actually encoding
radeontop  # AMD
intel_gpu_top  # Intel

# Check FFmpeg processes
docker exec xg2g-transcoder ps aux | grep ffmpeg
```

**Solution**:

- Ensure VAAPI device is correct: `VAAPI_DEVICE=/dev/dri/renderD128`
- Check FFmpeg hardware encoding: Look for `h264_vaapi` in logs
- Verify no software fallback is occurring

## Advanced Configuration

### CPU Optimizations

For AMD Ryzen CPUs, use optimized builds:

```bash
# Build with Zen 4 optimizations
docker build \
  --build-arg RUST_TARGET_CPU=znver4 \
  --build-arg GO_AMD64_LEVEL=v3 \
  -t xg2g:optimized .
```

See [CPU_OPTIMIZATIONS.md](CPU_OPTIMIZATIONS.md) for details.

### Custom Video Bitrate

Adjust GPU transcoder bitrate:

```yaml
environment:
  VIDEO_BITRATE: 8000k  # Higher quality (default: 5000k)
  AUDIO_BITRATE: 256k   # Higher audio quality (default: 192k)
```

### Multiple Concurrent Streams

Default configuration supports 4+ concurrent streams. For more:

```yaml
# Increase FFmpeg threads
environment:
  FFMPEG_THREADS: 4  # Per stream
```

## Performance Comparison

### Audio-Only Transcoding (Old)

- ✅ Low CPU usage
- ✅ Simple architecture
- ❌ Audio delay with Jellyfin HEVC transcoding
- ❌ No deinterlacing
- ❌ No timestamp fixing

### GPU Full Transcoding (New)

- ✅ Perfect audio-video sync
- ✅ Hardware deinterlacing
- ✅ Timestamp regeneration
- ✅ Low CPU usage (GPU accelerated)
- ✅ Scalable (4+ concurrent streams)
- ⚠️ Requires GPU with VAAPI

## Migration Guide

### From Audio-Only to GPU Transcoding

**Current Setup**:

```yaml
environment:
  XG2G_ENABLE_AUDIO_TRANSCODING: "true"
  XG2G_AUDIO_CODEC: aac
  XG2G_AUDIO_BITRATE: 192k
```

**New Setup** (add these):

```yaml
services:
  transcoder:
    image: xg2g-gpu-transcoder:production
    devices:
      - /dev/dri:/dev/dri
    # ... (see Docker Compose example above)

  xg2g:
    environment:
      # Keep existing settings
      XG2G_ENABLE_AUDIO_TRANSCODING: "true"

      # Add GPU transcoding
      XG2G_GPU_TRANSCODE: "true"
      XG2G_TRANSCODER_URL: http://localhost:8085
    depends_on:
      - transcoder
```

**Rollback** (if needed):

```yaml
environment:
  XG2G_GPU_TRANSCODE: "false"  # Disable GPU, keep audio-only
```

## Known Issues

### H.264 Decode Errors

**Symptom**: Logs show "non-existing PPS" or "decode_slice_header error"

**Impact**: Usually harmless - initial frames until stream stabilizes

**Solution**: Increase `ANALYZE_DURATION` and `PROBE_SIZE`:

```yaml
environment:
  ANALYZE_DURATION: 1000000  # Default: 500000
  PROBE_SIZE: 1000000        # Default: 500000
```

### Interlaced Content

**Status**: ✅ Automatically handled with yadif deinterlacing

**Verification**:

```bash
# Check deinterlacing in logs
docker logs xg2g-transcoder 2>&1 | grep yadif
```

## Support

- **Documentation**: See [PRODUCTION_DEPLOYMENT.md](../PRODUCTION_DEPLOYMENT.md)
- **CPU Optimizations**: See [CPU_OPTIMIZATIONS.md](CPU_OPTIMIZATIONS.md)
- **Issues**: https://github.com/ManuGH/xg2g/issues

## Changelog

### v1.4.1 (2025-10-21)

- ✨ Added `XG2G_GPU_TRANSCODE` feature flag
- ✨ Implemented GPU transcoding integration in Go proxy
- ✨ Added automatic fallback cascade (GPU → Audio → Direct)
- ✨ Improved logging for GPU transcoding routes
- 🐛 Fixed audio-video synchronization issues
- 📝 Complete GPU transcoding documentation

---

**Built with ❤️ for perfect Live TV streaming**
