# 🎵 Audio Transcoding (v1.3.0+)

## Overview

xg2g includes optional real-time audio transcoding for Enigma2 streams. This feature solves audio/video synchronization issues in media servers like Jellyfin by converting MP2/AC3 audio to AAC, enabling Direct Play without transcoding delays.

## Problem Statement

### The Challenge

Enigma2 receivers typically stream in MPEG-TS format with:
- **Video**: H.264 (widely supported)
- **Audio**: MP2 or AC3 (not supported by web browsers)

### Media Server Behavior (Without Audio Transcoding)

When Jellyfin receives these streams:

1. **Local Playback (WLAN)**:
   - Video: H.264 → **Direct Copy** (no delay)
   - Audio: MP2 → **Transcode to AAC** (3-6s delay)
   - Result: **Audio/Video desynchronization** ("Mixed-Mode Remuxing")

2. **Remote/Mobile Playback**:
   - Limited transcoding options due to mixed codecs
   - Higher CPU usage on server

### The Solution

xg2g transcodes audio to AAC **before** it reaches Jellyfin:

1. **Local Playback**:
   - Video: H.264 (direct)
   - Audio: AAC (direct)
   - Result: **Direct Play → No delay → Perfect sync**

2. **Remote/Mobile**:
   - Jellyfin transcodes both to AV1+AAC together
   - Result: **Synchronized, efficient compression**

## How It Works

```text
Enigma2 (Port 17999)
  ↓
  MPEG-TS: H.264 + MP2/AC3
  ↓
xg2g Proxy (Port 18000)
  ↓
  [FFmpeg Audio Transcoding]
  ↓
  MPEG-TS: H.264 + AAC
  ↓
Jellyfin/Media Server
  ↓
  Direct Play (no transcoding needed)
```

### Technical Details

- **Process**: FFmpeg pipes stream through audio transcoding
- **Video**: Passthrough (no re-encoding)
- **Audio**: Transcode to AAC (configurable bitrate)
- **Container**: MPEG-TS (unchanged)
- **Latency**: +100-200ms (negligible)

## Configuration

### Environment Variables

**Note:** As of Phase 6, audio transcoding and Rust remuxer are **ENABLED BY DEFAULT** for iOS Safari compatibility. You only need to set these variables if you want to override the defaults.

```bash
# Audio transcoding (default: true - ENABLED BY DEFAULT)
# Only set this to disable transcoding:
# XG2G_ENABLE_AUDIO_TRANSCODING=false

# Use Rust remuxer (default: true - ENABLED BY DEFAULT)
# Only set this to use FFmpeg instead:
# XG2G_USE_RUST_REMUXER=false

# Audio codec (optional, default: aac)
# Options: aac, mp3
XG2G_AUDIO_CODEC=aac

# Audio bitrate (optional, default: 192k)
# Examples: 128k, 192k, 256k, 320k
XG2G_AUDIO_BITRATE=192k

# Audio channels (optional, default: 2)
# Options: 1 (mono), 2 (stereo)
XG2G_AUDIO_CHANNELS=2

# FFmpeg path (optional, default: ffmpeg)
# Only needed if ffmpeg is not in PATH or using FFmpeg fallback
XG2G_FFMPEG_PATH=/usr/bin/ffmpeg
```

### Minimal Configuration (Recommended)

For most users, **no audio transcoding configuration is needed**:

```bash
# Only required settings:
export XG2G_OWI_BASE=http://RECEIVER_IP:80
export XG2G_PROXY_TARGET=http://RECEIVER_IP:17999
export LD_LIBRARY_PATH=/path/to/transcoder/target/release

# Audio transcoding is ENABLED BY DEFAULT!
./xg2g-daemon
```

### Docker Compose Example (Minimal - Recommended)

**Audio transcoding is ENABLED BY DEFAULT** - minimal configuration needed:

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
      - "18000:18000"  # Proxy port
    environment:
      # Required: Enigma2 connection
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Premium

      # Required: Stream proxy configuration
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_TARGET=http://192.168.1.100:17999
      - XG2G_STREAM_BASE=http://192.168.1.50:18000

      # Required: Rust library path
      - LD_LIBRARY_PATH=/app/transcoder/target/release

      # Audio transcoding ENABLED BY DEFAULT (no config needed!)
      # Only set these to override defaults:
      # - XG2G_ENABLE_AUDIO_TRANSCODING=false  # To disable
      # - XG2G_USE_RUST_REMUXER=false          # To use FFmpeg
      # - XG2G_AUDIO_CODEC=aac                  # Default: aac
      # - XG2G_AUDIO_BITRATE=192k               # Default: 192k
      # - XG2G_AUDIO_CHANNELS=2                 # Default: 2
```

## Performance

### Resource Usage (Rust Remuxer - Default)

**Per Active Stream:**
- CPU: **~0%** (native Rust - near-zero overhead)
- Memory: **~1-2MB** per stream
- Network: Same as input (no additional bandwidth)
- Latency: **+5ms** (negligible)

**System Requirements:**
- Rust library (`libac_remuxer.so`) in `LD_LIBRARY_PATH`
- For 10+ concurrent streams: Still **0% CPU**, ~20MB RAM total

**Legacy FFmpeg Performance (if Rust remuxer disabled):**
- CPU: ~10-15% per stream
- Memory: ~20MB per stream
- Latency: +100-200ms
- For 5 concurrent streams: ~50-75% CPU, ~100MB RAM

### Latency Impact

| Implementation | Latency Added | CPU per Stream |
|---------------|---------------|----------------|
| **Rust Remuxer (default)** | **+5ms** | **0%** |
| FFmpeg fallback | +100-200ms | ~10-15% |

**Recommendation:** Use default Rust remuxer for best performance.

## Codec Recommendations

### AAC (Recommended) ✅

**Pros:**
- Universal browser support (Chrome, Firefox, Safari, Edge)
- Better quality at same bitrate vs MP3
- Native support in HLS/DASH streaming
- Lower CPU usage for decoding

**Cons:**
- Slightly higher encoding CPU vs MP3

**Use when:** General use, Jellyfin, Plex, Emby

### MP3

**Pros:**
- Slightly lower encoding CPU
- Universal compatibility (legacy devices)

**Cons:**
- Lower quality at same bitrate
- Larger file sizes for same quality

**Use when:** Legacy device compatibility required

## Bitrate Recommendations

| Use Case | Recommended Bitrate | Notes |
|----------|---------------------|-------|
| **Standard TV** | 128k-192k | Good quality, low bandwidth |
| **HD Channels** | 192k-256k | Recommended for most users |
| **Premium/Music** | 256k-320k | Maximum quality |

**Note:** Higher bitrates increase CPU usage and bandwidth slightly.

## Jellyfin Integration

### Before Audio Transcoding

```text
Jellyfin Playback Info:
┌─────────────────────────────────┐
│ Video: H264 (direct)            │  ← No transcoding
│ Audio: AAC                      │  ← Transcoded (delayed!)
├─────────────────────────────────┤
│ Result: Audio delayed 3-6s      │
└─────────────────────────────────┘
```

### After Audio Transcoding

```text
Jellyfin Playback Info:
┌─────────────────────────────────┐
│ Video: H264 (direct)            │  ← No transcoding
│ Audio: AAC (direct)             │  ← No transcoding
├─────────────────────────────────┤
│ Direct Play - Perfect sync!     │
└─────────────────────────────────┘
```

### Enabling AV1 Transcoding (Mobile)

With audio transcoding enabled, you can now enable AV1 in Jellyfin for mobile:

1. **Dashboard → Playback → Transcoding**
2. Enable: **Allow AV1 encoding**
3. Set: **Hardware acceleration: VAAPI** (if available)

Result:
- **Local**: Direct Play (H264+AAC)
- **Mobile**: AV1+AAC (both transcoded together, synchronized)

## Troubleshooting

### Audio still delayed

**Check:**
1. Verify transcoding is active:
   ```bash
   docker logs xg2g 2>&1 | grep "audio transcoding enabled"
   ```

2. Check stream uses proxy port (18000, not 8001):
   ```bash
   curl -s http://localhost:8080/files/playlist.m3u | grep "http://"
   ```

3. Verify Jellyfin receives AAC:
   - Play stream in Jellyfin
   - Check playback info: Audio should show "AAC (direct)"

### FFmpeg errors

**Symptom:** Streams fail to start or break
**Solution:** Check FFmpeg logs:
```bash
docker logs xg2g 2>&1 | grep "ffmpeg"
```

Common issues:
- **"ffmpeg not found"**: Ensure image is v1.3.0+ (includes FFmpeg)
- **"Invalid audio codec"**: Check `XG2G_AUDIO_CODEC` is `aac` or `mp3`

### High CPU usage

**If CPU usage is too high:**

1. **Lower bitrate:**
   ```bash
   XG2G_AUDIO_BITRATE=128k  # Instead of 192k
   ```

2. **Reduce concurrent streams:**
   - Monitor: `docker stats xg2g`
   - Limit clients in media server

3. **Use hardware transcoding in Jellyfin** (for mobile):
   - VAAPI (AMD/Intel)
   - NVENC (NVIDIA)
   - QuickSync (Intel)

### No audio in playback

**Check:**
1. Original stream has audio:
   ```bash
   ffprobe http://192.168.1.100:17999/[service-ref]
   ```

2. FFmpeg can access stream:
   ```bash
   docker exec xg2g ffmpeg -i "http://192.168.1.100:17999/[service-ref]" -t 5 -f null -
   ```

## Architecture

### Component Flow

```text
┌─────────────────────────────────────────────────────┐
│ xg2g Proxy Server (Port 18000)                      │
├─────────────────────────────────────────────────────┤
│                                                     │
│  Incoming Request                                   │
│         ↓                                           │
│  ┌──────────────┐                                   │
│  │ Request Type │                                   │
│  └──────┬───────┘                                   │
│         │                                           │
│    ┌────┴─────┬──────────────┐                     │
│    │          │              │                     │
│  HEAD        GET           POST                    │
│    │          │              │                     │
│    ↓          ↓              ↓                     │
│  Answer   Transcode?      Proxy                    │
│  200 OK       │              │                     │
│            ┌──┴──┐           │                     │
│           Yes   No            │                     │
│            │     │            │                     │
│            ↓     ↓            ↓                     │
│        ┌─────────────────────────┐                 │
│        │ Proxy to Enigma2        │                 │
│        │ http://IP:17999/ref     │                 │
│        └──────────┬──────────────┘                 │
│                   │                                 │
│                   ↓                                 │
│        ┌─────────────────────────┐                 │
│        │ MPEG-TS Stream          │                 │
│        │ H.264 + MP2/AC3         │                 │
│        └──────────┬──────────────┘                 │
│                   │                                 │
│                   ↓                                 │
│        ┌─────────────────────────┐                 │
│        │ FFmpeg Transcoding      │                 │
│        │ • Copy video            │                 │
│        │ • Transcode audio→AAC   │                 │
│        └──────────┬──────────────┘                 │
│                   │                                 │
│                   ↓                                 │
│        ┌─────────────────────────┐                 │
│        │ MPEG-TS Stream          │                 │
│        │ H.264 + AAC             │                 │
│        └──────────┬──────────────┘                 │
│                   │                                 │
└───────────────────┼─────────────────────────────────┘
                    ↓
              Client/Jellyfin
```

### Code Structure

**[transcoder.go](../../internal/proxy/transcoder.go)**
- `Transcoder` struct: Manages FFmpeg processes
- `TranscodeStream()`: Pipes stream through FFmpeg
- Environment helpers: `GetTranscoderConfig()`, `IsTranscodingEnabled()`

**[proxy.go](../../internal/proxy/proxy.go)**
- Integrates transcoder into request handler
- Routes GET requests through transcoder if enabled
- Fallback to direct proxy on errors

## Best Practices

### 1. Production Setup

```yaml
# Recommended production configuration
environment:
  # Always enable proxy with transcoding
  - XG2G_ENABLE_STREAM_PROXY=true
  - XG2G_ENABLE_AUDIO_TRANSCODING=true

  # Use standard AAC bitrate
  - XG2G_AUDIO_CODEC=aac
  - XG2G_AUDIO_BITRATE=192k
  - XG2G_AUDIO_CHANNELS=2

  # Smart stream detection for optimal ports
  - XG2G_SMART_STREAM_DETECTION=true
```

### 2. Monitoring

**Health Check:**
```bash
# Check if transcoding is active
curl http://localhost:8080/api/status | jq '.features.audio_transcoding'
```

**Metrics (Prometheus):**

```promql
# CPU usage per stream
process_cpu_seconds_total{job="xg2g"}

# Memory usage
process_resident_memory_bytes{job="xg2g"}
```

### 3. Scaling

**For multiple users:**

1. **Vertical Scaling**: Increase CPU cores
   ```yaml
   deploy:
     resources:
       limits:
         cpus: '4'  # 4 cores = ~20-25 concurrent streams
   ```

2. **Horizontal Scaling**: Multiple xg2g instances
   - Load balance with nginx/traefik
   - Each instance handles subset of bouquets

## Migration Guide

### From v1.2.0 to v1.3.0

**If you had audio sync issues:**

1. Update to v1.3.0:
   ```bash
   docker pull ghcr.io/manugh/xg2g:latest
   ```

2. Add to docker-compose.yml:
   ```yaml
   - XG2G_ENABLE_AUDIO_TRANSCODING=true
   - XG2G_AUDIO_CODEC=aac
   - XG2G_AUDIO_BITRATE=192k
   ```

3. Restart container:
   ```bash
   docker-compose down && docker-compose up -d
   ```

4. Verify in Jellyfin:
   - Play a channel
   - Check playback info: "Audio: AAC (direct)"
   - Verify synchronization

**If using external nginx proxy:**

You can now remove nginx and use xg2g's integrated solution:

**Before (v1.2.0 + nginx):**

```text
Enigma2:17999 → nginx:18000 → xg2g:8080 → Jellyfin
                 (HEAD proxy)
```

**After (v1.3.0):**

```text
Enigma2:17999 → xg2g:18000 → Jellyfin
                (HEAD + Audio transcoding)
```

## FAQ

### Q: Does this re-encode video?
**A:** No, video is always copied (passthrough). Only audio is transcoded.

### Q: Will this increase network traffic?
**A:** No, output bitrate is similar to input. AAC at 192k is comparable to MP2 at 256k.

### Q: Can I disable transcoding for specific channels?
**A:** Currently no, it's all-or-nothing. Use `XG2G_ENABLE_AUDIO_TRANSCODING=false` to disable globally.

### Q: Does this work with 4K streams?
**A:** Yes, transcoding only affects audio. Video resolution is irrelevant.

### Q: What if original stream already has AAC?
**A:** FFmpeg detects this and may passthrough, but generally will re-encode to ensure consistency.

### Q: Can I use hardware audio transcoding?
**A:** Audio transcoding is very lightweight (~10% CPU). Hardware acceleration is not needed and not supported by FFmpeg for audio.

## Support

- **Issues**: [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
- **Discussions**: [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
- **Documentation**: [docs/](../../docs/)

## Changelog

### v1.3.0 (2025-01-XX)
- ✨ Added optional audio transcoding feature
- ✨ AAC/MP3 codec support
- ✨ Configurable bitrate and channels
- 📦 FFmpeg included in Docker image
- 📝 Comprehensive documentation

---

**Status**: ✅ Production Ready
**Performance**: ~10-15% CPU per stream
**Compatibility**: Jellyfin, Plex, Emby, VLC, all HLS clients
