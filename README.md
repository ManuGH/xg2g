# xg2g

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Coverage](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml)
[![Integration Tests](https://github.com/ManuGH/xg2g/actions/workflows/integration-tests.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/integration-tests.yml)
[![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml)
[![CodeQL](https://github.com/ManuGH/xg2g/actions/workflows/codeql.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![Go Reference](https://pkg.go.dev/badge/github.com/ManuGH/xg2g.svg)](https://pkg.go.dev/github.com/ManuGH/xg2g)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Enigma2 to IPTV Gateway** - Stream your satellite/cable receiver to Plex, Jellyfin, or any IPTV player.

M3U playlists · XMLTV EPG · HDHomeRun emulation · OSCam Streamrelay support

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
- ✅ **Smart stream detection** with OSCam Streamrelay support
- ✅ Docker ready

### HDHomeRun Emulation (v1.4.0)
- ✅ Auto-discovery via SSDP/UPnP
- ✅ Plex/Jellyfin native integration
- ✅ No manual M3U configuration needed
- ✅ Perfect EPG matching

### Advanced
- ✅ Audio transcoding (MP2/AC3 → AAC)
- ✅ **GPU transcoding** with VAAPI (AMD/Intel) - [See Production Deployment Guide](PRODUCTION_DEPLOYMENT.md)
- ✅ Integrated stream proxy
- ✅ Prometheus metrics
- ✅ Health checks

### OSCam Streamrelay Support
- ✅ **Automatic channel detection** via whitelist_streamrelay
- ✅ **Smart port selection**: Channels requiring OSCam use port 17999, others use port 8001
- ✅ **Zero configuration** - Works out of the box if OSCam is configured on your receiver
- ✅ **Deterministic** - No race conditions, reliable port assignment

> **Note:** xg2g automatically detects which channels are routed through OSCam Streamrelay on your receiver. It reads the receiver's whitelist_streamrelay file to determine the correct port for each channel.

---

## Configuration

xg2g supports multiple configuration methods with **precedence** (highest to lowest):

1. **Environment Variables** (highest priority)
2. **Configuration File** (YAML)
3. **Defaults** (lowest priority)

### Using Configuration Files (v1.5.0+)

**Recommended for production deployments.**

```bash
# Start with config file
xg2g --config /etc/xg2g/config.yaml

# Docker with config file
docker run -v /path/to/config.yaml:/config.yaml \
  ghcr.io/manugh/xg2g:latest --config /config.yaml
```

**Example config file** ([examples/config.example.yaml](examples/config.example.yaml)):

```yaml
dataDir: /data

openWebIF:
  baseUrl: http://192.168.1.100
  username: root
  password: ${XG2G_OWI_PASSWORD}  # Use ENV for secrets
  streamPort: 8001

bouquets:
  - Favourites
  - Premium

epg:
  enabled: true
  days: 7
  maxConcurrency: 5

api:
  token: ${XG2G_API_TOKEN}
  listenAddr: :8080
```

**See also:**
- [examples/config.minimal.yaml](examples/config.minimal.yaml) - Minimal setup
- [examples/config.production.yaml](examples/config.production.yaml) - Production-ready template

### Environment Variables (Legacy)

**Still fully supported** - ENV vars override config file values.

#### Required

| Variable | Description | Example |
|----------|-------------|---------|
| `XG2G_OWI_BASE` | Enigma2 receiver URL | `http://192.168.1.100` |
| `XG2G_BOUQUET` | Bouquet name(s) | `Favourites` or `Movies,Sports` |

#### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_EPG_ENABLED` | `false` | Enable EPG collection |
| `XG2G_EPG_DAYS` | `7` | Days of EPG (1-14) |
| `XG2G_HDHR_ENABLED` | `false` | Enable HDHomeRun emulation |
| `XG2G_HDHR_FRIENDLY_NAME` | `xg2g` | Name shown in Plex/Jellyfin |
| `XG2G_ENABLE_AUDIO_TRANSCODING` | `false` | Transcode audio to AAC |
| `XG2G_GPU_TRANSCODE` | `false` | Enable GPU transcoding (full video+audio) |
| `XG2G_TRANSCODER_URL` | `http://localhost:8085` | GPU transcoder service URL |
| `XG2G_SMART_STREAM_DETECTION` | `false` | Auto-detect optimal stream port |
| `XG2G_API_TOKEN` | - | API authentication token |

#### OpenWebIF Authentication

```bash
XG2G_OWI_USER=root
XG2G_OWI_PASS=yourpassword
```

**Best Practice:** Use config file for static settings, ENV vars for secrets and environment-specific overrides.

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

### Option 1: Using Config File (Recommended)

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g
    command: ["--config", "/config/config.yaml"]
    ports:
      - "8080:8080"      # HTTP API
      - "1900:1900/udp"  # SSDP discovery
    environment:
      # Secrets (override config file)
      - XG2G_OWI_PASSWORD=${XG2G_OWI_PASSWORD}
      - XG2G_API_TOKEN=${XG2G_API_TOKEN}
    volumes:
      - ./data:/data
      - ./config.yaml:/config/config.yaml:ro
    restart: unless-stopped
```

**config.yaml:**
```yaml
openWebIF:
  baseUrl: http://192.168.1.100
  username: root
  password: ${XG2G_OWI_PASSWORD}
bouquets:
  - Favourites
epg:
  enabled: true
  days: 7
api:
  token: ${XG2G_API_TOKEN}
```

### Option 2: Using Environment Variables (Legacy)

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

### Versioned API (v1.5.0+)

xg2g now supports versioned APIs for better stability and backwards compatibility.

**Current stable version**: `/api/v1`

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/status` | GET | No | Service status |
| `/api/v1/refresh` | POST | Yes | Trigger manual refresh |

**Authentication**: Use `X-API-Token` header with your configured token.

**Example**:
```bash
# Get status
curl http://localhost:8080/api/v1/status | jq .

# Trigger refresh (requires token)
curl -X POST http://localhost:8080/api/v1/refresh \
  -H "X-API-Token: $XG2G_API_TOKEN"
```

📚 **Full API documentation**: [docs/API_V1_CONTRACT.md](docs/API_V1_CONTRACT.md)

### Legacy Endpoints (Deprecated)

⚠️ **Deprecated**: The following endpoints are deprecated and will be removed in v2.0.0 (sunset: 2025-12-31).

| Endpoint | Method | Replacement |
|----------|--------|-------------|
| `/api/status` | GET | `/api/v1/status` |
| `/api/refresh` | POST | `/api/v1/refresh` |

Legacy endpoints return deprecation headers (`Deprecation`, `Sunset`, `Link`) per [RFC 8594](https://datatracker.ietf.org/doc/html/rfc8594).

**Migration guide**: Replace `/api/` with `/api/v1/` in all your integrations.

### Other Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/files/playlist.m3u` | GET | M3U playlist |
| `/xmltv.xml` | GET | XMLTV EPG (remapped for Plex) |
| `/discover.json` | GET | HDHomeRun discovery |
| `/lineup.json` | GET | HDHomeRun channel lineup |
| `/device.xml` | GET | UPnP device description |
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

**With OSCam Streamrelay** (automatic, recommended):
```bash
XG2G_SMART_STREAM_DETECTION=true
```
xg2g automatically detects which channels use OSCam Streamrelay and routes them to port 17999.

**Manual port configuration** (if needed):
```yaml
openWebIF:
  baseUrl: "http://192.168.1.100"  # ⚠️ NO PORT HERE!
  streamPort: 8001  # or 17999 for OSCam channels
```

**Troubleshooting**:
- Standard channels use port 8001
- OSCam Streamrelay channels use port 17999
- Check receiver's `/etc/enigma2/whitelist_streamrelay` for channel routing

**📚 See [Stream Port Configuration Guide](docs/STREAM_PORTS.md) for detailed explanation.**

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
