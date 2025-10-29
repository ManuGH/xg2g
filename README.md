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

### Verify Image Signature (Recommended)

All container images are signed with [cosign](https://github.com/sigstore/cosign) and include SLSA provenance attestation:

```bash
# Verify signature (requires cosign v2+)
cosign verify \
  --certificate-identity-regexp="https://github.com/ManuGH/xg2g" \
  ghcr.io/manugh/xg2g:latest

# View SLSA build provenance
cosign verify-attestation --type slsaprovenance \
  ghcr.io/manugh/xg2g:latest | jq '.payload | @base64d | fromjson'
```

**Why verify?** Ensures the image you're running was built by our official GitHub Actions workflow and hasn't been tampered with.

### Run Container

```bash
docker run -d \
  --name xg2g \
  -p 8080:8080 \
  -p 1900:1900/udp \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_OWI_USER=root \
  -e XG2G_OWI_PASS=yourpassword \
  -e XG2G_BOUQUET=Favourites \
  -v ./data:/data \
  ghcr.io/manugh/xg2g:latest
```

**That's it!** Everything works out of the box:
- ✅ M3U playlist with channel logos
- ✅ 7-day EPG guide (XMLTV)
- ✅ HDHomeRun emulation (Plex/Jellyfin auto-discovery)
- ✅ Smart stream detection (OSCam port 8001/17999)
- ✅ Enigma2 authentication

**Access Your Streams:**

| Method | URL | Port | Use Case |
|--------|-----|------|----------|
| **M3U Playlist** | `http://YOUR_IP:8080/files/playlist.m3u` | 8080 | VLC, Kodi, any IPTV player |
| **XMLTV EPG** | `http://YOUR_IP:8080/xmltv.xml` | 8080 | Electronic Program Guide |
| **HDHomeRun (Auto)** | Auto-discovered via SSDP | 1900/udp | Plex/Jellyfin (bare-metal/VM) |
| **HDHomeRun (Manual)** | `YOUR_IP:8080` | 8080 | Plex/Jellyfin (containers) |
| **Device Info** | `http://YOUR_IP:8080/discover.json` | 8080 | HDHomeRun device details |
| **Channel Lineup** | `http://YOUR_IP:8080/lineup.json` | 8080 | HDHomeRun channel list |

**Container Note:** HDHomeRun auto-discovery via SSDP multicast may not work reliably in LXC/Docker environments. Use manual configuration in Plex/Jellyfin with the IP address above.

**Authentication Note:** Most Enigma2 receivers use `root` as username. Remove auth lines if your receiver has no password.

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

### Environment Variables

**Required:**

```bash
XG2G_OWI_BASE=http://192.168.1.100   # Your Enigma2 receiver
XG2G_OWI_USER=root                   # Enigma2 username (standard: root)
XG2G_OWI_PASS=yourpassword           # Enigma2 password
XG2G_BOUQUET=Favourites              # Bouquet name
```

**Everything else is enabled by default:**
- ✅ EPG collection (7 days)
- ✅ Smart stream detection (OSCam auto-detection)
- ✅ HDHomeRun emulation (Plex/Jellyfin)
- ✅ Channel logos

#### Disable features (if needed)

```bash
XG2G_EPG_ENABLED=false                    # Disable EPG
XG2G_SMART_STREAM_DETECTION=false         # Disable auto port detection
XG2G_HDHR_ENABLED=false                   # Disable HDHomeRun emulation
```

#### Advanced configuration

See [config.example.yaml](config.example.yaml) for 30+ tuning options.

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
    user: "65532:65532"  # Run as non-root for security
    ports:
      - "8080:8080"
      - "1900:1900/udp"  # For Plex/Jellyfin discovery
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_OWI_USER=root
      - XG2G_OWI_PASS=yourpassword
      - XG2G_BOUQUET=Favourites
    volumes:
      - ./data:/data
    restart: unless-stopped
```

**Note:** Ensure the `./data` directory is writable by UID 65532:

```bash
mkdir -p data && chown -R 65532:65532 data
# Or use current user: sudo chown -R $(id -u):$(id -g) data
```

**All features enabled by default:**

- EPG (7 days), HDHomeRun, Smart stream detection, Channel logos, Authentication

**Using config file** (advanced):

See [examples/config.production.yaml](examples/config.production.yaml)

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
