# xg2g

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Release](https://img.shields.io/badge/release-v1.7.0-blue.svg)](https://github.com/ManuGH/xg2g/releases/tag/v1.7.0)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Stream your satellite/cable receiver to any device.**

Works with Plex, Jellyfin, iPhone, VLC, Kodi - everything.

---

## Install

```bash
docker run -d \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://RECEIVER_IP \
  -e XG2G_BOUQUET=Favourites \
  ghcr.io/manugh/xg2g:latest
```

**Done!** Now open: `http://YOUR_IP:8080/files/playlist.m3u`

---

## What It Does

✅ Converts your receiver into an IPTV server
✅ Works with iPhone/iPad Safari (audio fixed automatically)
✅ Includes TV guide (7 days)
✅ Auto-discovery in Plex/Jellyfin
✅ Channel logos included

---

## Use It

### In VLC/Kodi

Open this URL: `http://YOUR_IP:8080/files/playlist.m3u`

### In Plex/Jellyfin

Enable auto-discovery:
```bash
-e XG2G_HDHR_ENABLED=true
```

Plex/Jellyfin will find it automatically.

### On iPhone/iPad

Add stream proxy for working audio:
```bash
docker run -d \
  -p 8080:8080 \
  -p 18000:18000 \
  -e XG2G_OWI_BASE=http://RECEIVER_IP \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_ENABLE_STREAM_PROXY=true \
  -e XG2G_PROXY_TARGET=http://RECEIVER_IP:17999 \
  ghcr.io/manugh/xg2g:latest
```

**Audio works automatically** in Safari. Rust remuxer converts AC3/MP2 → AAC with **0% CPU overhead**.

No extra setup needed.

---

## Settings

### Must Set

```bash
XG2G_OWI_BASE=http://192.168.1.100    # Your receiver IP
XG2G_BOUQUET=Favourites               # Channel list name
```

### Nice to Have

```bash
XG2G_OWI_USER=root          # If receiver has password
XG2G_OWI_PASS=password      # Receiver password
```

### Turn Off (if needed)

```bash
XG2G_EPG_ENABLED=false      # No TV guide
XG2G_HDHR_ENABLED=false     # No Plex/Jellyfin auto-discovery
```

Everything else works automatically.

---

## 3 Deployment Modes

xg2g has **3 modes** for different use cases:

### MODE 1: Standard (VLC, Kodi, Plex)

**No audio transcoding.** Original AC3/MP2 audio. Desktop players handle this natively.

```bash
docker compose up -d
```

See: [docker-compose.yml](docker-compose.yml)

### MODE 2: Audio Proxy (iPhone/iPad)

**Audio transcoding** for mobile devices. AC3/MP2 → AAC for Safari compatibility.

```bash
docker compose -f docker-compose.audio-proxy.yml up -d
```

Access streams: `http://localhost:18000/1:0:19:...`

See: [docker-compose.audio-proxy.yml](docker-compose.audio-proxy.yml)

### MODE 3: GPU Transcoding

**Hardware-accelerated video + audio transcoding** using VAAPI. For low-power clients or bandwidth optimization.

```bash
docker compose -f docker-compose.gpu.yml up -d
```

**Requirements:**
- Intel Quick Sync (6th gen+) or AMD GPU with VAAPI support
- Host with `/dev/dri/renderD128` device
- Run `vainfo` on host to verify GPU support

Access streams: `http://localhost:18000/1:0:19:...` (routes through GPU transcoder)

See: [docker-compose.gpu.yml](docker-compose.gpu.yml)

---

### Quick Setup Examples

**Standard mode** (desktop players):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
```

**Audio Proxy mode** (iPhone/iPad):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
      - "18000:18000"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_TARGET=http://192.168.1.100:8001
```

**GPU Transcoding mode** (hardware acceleration):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
      - "18000:18000"
      - "8085:8085"
    devices:
      - /dev/dri:/dev/dri
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_ENABLE_GPU_TRANSCODING=true
      - XG2G_ENABLE_STREAM_PROXY=true
```

---

## Help

- **API Documentation:** [API Reference](https://manugh.github.io/xg2g/api.html)
- **How-to guides:** [docs/](docs/)
- **Questions:** [Discussions](https://github.com/ManuGH/xg2g/discussions)
- **Problems:** [Issues](https://github.com/ManuGH/xg2g/issues)

---

**MIT License** - Free to use
