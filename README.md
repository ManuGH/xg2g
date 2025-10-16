# xg2g - OpenWebIF to M3U/XMLTV Converter

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml)

**Convert Enigma2 OpenWebIF bouquets to M3U playlists and XMLTV EPG for use with Threadfin, Jellyfin, Plex, and other IPTV players.**

---

## Features

- ✅ **Direct TS Streaming** - Uses native Enigma2 stream URLs for best compatibility
- ✅ **Integrated Stream Proxy** - Built-in HEAD request handler for Threadfin/Jellyfin compatibility (no nginx needed!)
- ✅ **Sequential Channel Numbers** - Channels numbered by bouquet order (1, 2, 3...)
- ✅ **Channel Logos** - Automatically fetched from your receiver
- ✅ **EPG Data** - Full 7-day electronic program guide support
- ✅ **Multiple Bouquets** - Combine multiple bouquets in one playlist
- ✅ **Docker Ready** - Pre-built images available

---

## Quick Start

### Docker (Recommended)

```bash
docker run -d \
  --name xg2g \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_STREAM_PORT=8001 \
  -e XG2G_EPG_ENABLED=true \
  -v ./data:/data \
  ghcr.io/manugh/xg2g:latest
```

**Access your files:**
- M3U Playlist: `http://localhost:8080/files/playlist.m3u`
- XMLTV EPG: `http://localhost:8080/files/xmltv.xml`

### Docker Compose

See complete examples:
- **Simple Setup**: [examples/docker-compose/](examples/docker-compose/) - Just xg2g
- **Full Stack**: [examples/full-stack/](examples/full-stack/) - xg2g → Threadfin → Jellyfin
- **Live Test**: [examples/live-test/](examples/live-test/) - Complete test environment for Mac

Minimal example:

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_STREAM_PORT=8001
      - XG2G_EPG_ENABLED=true
    volumes:
      - ./data:/data
    restart: unless-stopped
```

---

## Configuration

### Required Settings

| Variable | Example | Description |
|----------|---------|-------------|
| `XG2G_OWI_BASE` | `http://192.168.1.100` | Your Enigma2 receiver IP |
| `XG2G_BOUQUET` | `Favourites` | Bouquet name (find in OpenWebif → EPG) |

### Optional Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_STREAM_PORT` | `8001` | Stream port (8001 for direct streams, 17999 for Stream Relay) |
| `XG2G_EPG_ENABLED` | `false` | Enable EPG data collection |
| `XG2G_EPG_DAYS` | `7` | Days of EPG to fetch (1-14) |
| `XG2G_XMLTV` | `xmltv.xml` | XMLTV output filename (auto-set when EPG enabled) |
| `XG2G_OWI_USER` | - | OpenWebif username (if auth required) |
| `XG2G_OWI_PASS` | - | OpenWebif password (if auth required) |

### Stream Proxy Settings (Advanced)

Only needed if Enigma2 Stream Relay (port 17999) doesn't support HEAD requests, causing EOF errors in Threadfin/Jellyfin.

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_ENABLE_STREAM_PROXY` | `false` | Enable integrated stream proxy |
| `XG2G_PROXY_PORT` | `18000` | Proxy listen port |
| `XG2G_PROXY_TARGET` | - | Target Enigma2 URL (e.g., `http://192.168.1.100:17999`) |
| `XG2G_STREAM_BASE` | - | Override stream URLs (e.g., `http://your-host:18000`) |

**When to use:** If you see "EOF" errors in Threadfin/Jellyfin logs when using port 17999, enable the integrated proxy. See [examples/live-test/STREAM_CONFIGURATION.md](examples/live-test/STREAM_CONFIGURATION.md) for detailed setup.

### Multiple Bouquets

Combine multiple bouquets into one playlist:

```bash
XG2G_BOUQUET="Favourites,Movies,Sports"
```

All channels will be merged with sequential numbering.

**For Threadfin users:** See detailed setup guide at [docs/guides/THREADFIN.md](docs/guides/THREADFIN.md)

---

## API Endpoints

- `GET /files/playlist.m3u` - Generated M3U playlist
- `GET /files/xmltv.xml` - Generated XMLTV EPG
- `GET /api/status` - Service status
- `POST /api/refresh` - Trigger manual refresh (requires API token)
- `GET /healthz` - Health check
- `GET /readyz` - Readiness check

### API Token for Manual Refresh

The `/api/refresh` endpoint requires authentication to prevent unauthorized refreshes:

```bash
# Generate a secure token
openssl rand -hex 16

# Set in environment or .env file
XG2G_API_TOKEN=your-generated-token-here

# Use with curl
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: your-generated-token-here"
```

**Note:** If `XG2G_API_TOKEN` is not set, the `/api/refresh` endpoint will be disabled (returns 401).

---

## Using with IPTV Software

Use xg2g with any IPTV software that supports M3U and XMLTV:

**M3U URL:** `http://your-host:8080/files/playlist.m3u`
**XMLTV URL:** `http://your-host:8080/files/xmltv.xml`

### Popular Software

- **Jellyfin/Plex**: Add as M3U Tuner + XMLTV Guide
- **Threadfin/xTeve**: Add as M3U + XMLTV source
- **Kodi**: Add via PVR IPTV Simple Client
- **VLC**: Open M3U playlist directly

For detailed setup guides, see [examples/](examples/) directory.

---

## Troubleshooting

### No Channels Found

Check your bouquet name:
```bash
# List available bouquets
curl http://192.168.1.100/api/bouquets
```

### Streams Don't Play

1. Verify stream port (usually `8001`, sometimes alternative ports like `17999`)
2. Test direct stream:
   ```bash
   curl -I http://192.168.1.100:8001/1:0:1:...
   ```
3. If you see "EOF" or "Empty reply" errors with port `17999`, you may need the integrated proxy:
   ```bash
   XG2G_ENABLE_STREAM_PROXY=true
   XG2G_PROXY_TARGET=http://192.168.1.100:17999
   XG2G_STREAM_BASE=http://your-host-ip:18000
   ```
4. Check firewall settings on your receiver

See [STREAM_CONFIGURATION.md](examples/live-test/STREAM_CONFIGURATION.md) for detailed troubleshooting.

### No EPG Data

Enable EPG collection:
```bash
XG2G_EPG_ENABLED=true
XG2G_EPG_DAYS=7
```

---

## Advanced Topics

For production deployments, monitoring, and performance tuning, see:
- [docs/ADVANCED.md](docs/ADVANCED.md) - Advanced configuration and tuning
- [docs/PRODUCTION.md](docs/PRODUCTION.md) - Production deployment & operations

---

## Development

### Build from Source

```bash
git clone https://github.com/ManuGH/xg2g.git
cd xg2g
go build ./cmd/daemon
```

### Run Tests

```bash
go test ./...
```

### Local Development

```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_BOUQUET=Favourites \
go run ./cmd/daemon
```

---

## Contributing

Contributions welcome! See [CONTRIBUTING.md](docs/CONTRIBUTING.md)

---

## License

MIT License - See [LICENSE](LICENSE)

---

## Support

- **Issues**: [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
- **Discussions**: [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
