# xg2g

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml)

**Stream your Enigma2 satellite/cable receiver channels directly to Plex, Jellyfin, or any IPTV player.**

Convert OpenWebIF bouquets to M3U playlists and XMLTV EPG. HDHomeRun emulation for automatic discovery.

---

## What it does

- **Converts** Enigma2 bouquets to M3U playlists
- **Generates** XMLTV EPG (Electronic Program Guide)
- **Emulates** HDHomeRun for auto-discovery in Plex/Jellyfin
- **Streams** directly from your satellite/cable receiver
- **Transcodes** audio on-the-fly (optional)

---

## Quick Start

```bash
docker run -d \
  --name xg2g \
  -p 8080:8080 \
  -p 1900:1900/udp \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_EPG_ENABLED=true \
  -e XG2G_HDHR_ENABLED=true \
  -v ./data:/data \
  ghcr.io/manugh/xg2g:latest
```

**URLs:**
- M3U: `http://localhost:8080/files/playlist.m3u`
- EPG: `http://localhost:8080/xmltv.xml`
- HDHomeRun discovery: automatic (if enabled)

---

## Features

### Core
- ✅ M3U playlist generation
- ✅ XMLTV EPG (7 days)
- ✅ Channel logos (picons)
- ✅ Multiple bouquets support
- ✅ Smart stream detection
- ✅ Docker ready

### HDHomeRun Emulation (v1.4.0)
- ✅ Auto-discovery via SSDP/UPnP
- ✅ Plex/Jellyfin native integration
- ✅ No manual M3U configuration needed
- ✅ Perfect EPG matching

### Advanced
- ✅ Audio transcoding (MP2/AC3 → AAC)
- ✅ Integrated stream proxy
- ✅ Prometheus metrics
- ✅ Health checks

---

## Configuration

### Required

| Variable | Description | Example |
|----------|-------------|---------|
| `XG2G_OWI_BASE` | Enigma2 receiver URL | `http://192.168.1.100` |
| `XG2G_BOUQUET` | Bouquet name(s) | `Favourites` or `Movies,Sports` |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_EPG_ENABLED` | `false` | Enable EPG collection |
| `XG2G_EPG_DAYS` | `7` | Days of EPG (1-14) |
| `XG2G_HDHR_ENABLED` | `false` | Enable HDHomeRun emulation |
| `XG2G_HDHR_FRIENDLY_NAME` | `xg2g` | Name shown in Plex/Jellyfin |
| `XG2G_ENABLE_AUDIO_TRANSCODING` | `false` | Transcode audio to AAC |
| `XG2G_SMART_STREAM_DETECTION` | `false` | Auto-detect optimal stream port |

### OpenWebIF Authentication (if needed)

```bash
XG2G_OWI_USER=root
XG2G_OWI_PASS=yourpassword
```

---

## Usage

### With Plex/Jellyfin (HDHomeRun Mode)

1. Enable HDHomeRun emulation:
   ```bash
   XG2G_HDHR_ENABLED=true
   ```

2. Plex/Jellyfin will **automatically discover** xg2g as a TV tuner

3. EPG is automatically matched and populated

**No manual M3U configuration needed!**

### With Threadfin/xTeve (M3U Mode)

Add these URLs:
- **M3U**: `http://your-host:8080/files/playlist.m3u`
- **XMLTV**: `http://your-host:8080/xmltv.xml`

---

## Docker Compose

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g
    ports:
      - "8080:8080"      # HTTP API
      - "1900:1900/udp"  # SSDP discovery
    environment:
      # Required
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites

      # EPG
      - XG2G_EPG_ENABLED=true
      - XG2G_EPG_DAYS=7

      # HDHomeRun
      - XG2G_HDHR_ENABLED=true
      - XG2G_HDHR_FRIENDLY_NAME=Enigma2 xg2g

      # Optional: Audio transcoding
      - XG2G_ENABLE_AUDIO_TRANSCODING=false
    volumes:
      - ./data:/data
    restart: unless-stopped
```

---

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/files/playlist.m3u` | GET | M3U playlist |
| `/files/xmltv.xml` | GET | XMLTV EPG (legacy) |
| `/xmltv.xml` | GET | XMLTV EPG (remapped for Plex) |
| `/discover.json` | GET | HDHomeRun discovery |
| `/lineup.json` | GET | HDHomeRun channel lineup |
| `/device.xml` | GET | UPnP device description |
| `/api/status` | GET | Service status |
| `/api/refresh` | POST | Trigger manual refresh (auth required) |
| `/healthz` | GET | Health check |
| `/readyz` | GET | Readiness check |

---

## Troubleshooting

### Plex/Jellyfin doesn't find the tuner

1. Make sure UDP port 1900 is accessible:
   ```bash
   docker run -p 1900:1900/udp ...
   ```

2. Check if SSDP announcer is running:
   ```bash
   docker logs xg2g | grep -i ssdp
   ```

3. Manually add tuner in Plex with: `http://your-host:8080`

### Channels show "Unknown Program"

- Enable EPG: `XG2G_EPG_ENABLED=true`
- Use the remapped XMLTV endpoint: `http://your-host:8080/xmltv.xml`

### Audio/Video out of sync

Enable audio transcoding:
```bash
XG2G_ENABLE_AUDIO_TRANSCODING=true
```

See [docs/AUDIO_DELAY_FIX.md](docs/AUDIO_DELAY_FIX.md) for details.

### Streams don't play

1. Test direct stream:
   ```bash
   curl -I http://192.168.1.100:8001/1:0:1:...
   ```

2. Enable smart detection:
   ```bash
   XG2G_SMART_STREAM_DETECTION=true
   ```

3. Or enable integrated proxy if needed:
   ```bash
   XG2G_ENABLE_STREAM_PROXY=true
   XG2G_PROXY_TARGET=http://192.168.1.100:17999
   XG2G_STREAM_BASE=http://your-host:18000
   ```

---

## Building from Source

```bash
git clone https://github.com/ManuGH/xg2g.git
cd xg2g
go build ./cmd/daemon
```

Run:
```bash
./daemon
```

---

## License

MIT License - See [LICENSE](LICENSE)

---

## Support

- [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
- [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
