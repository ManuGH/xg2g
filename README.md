# xg2g - OpenWebIF to M3U/XMLTV Converter

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml)
[![License](https://img.shields.io/github/license/ManuGH/xg2g)](LICENSE)

**Convert Enigma2 OpenWebIF bouquets to M3U playlists and XMLTV EPG for use with Threadfin, Jellyfin, Plex, and other IPTV players.**

---

## Features

- ✅ **Direct TS Streaming** - Uses native Enigma2 stream URLs for best compatibility
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
| `XG2G_STREAM_PORT` | `8001` | Stream port (default: 8001) |

### Optional Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_EPG_ENABLED` | `false` | Enable EPG data collection |
| `XG2G_EPG_DAYS` | `7` | Days of EPG to fetch (1-14) |
| `XG2G_XMLTV` | `xmltv.xml` | XMLTV output filename |
| `XG2G_OWI_USER` | - | OpenWebif username (if auth required) |
| `XG2G_OWI_PASS` | - | OpenWebif password (if auth required) |

### Multiple Bouquets

Combine multiple bouquets into one playlist:

```bash
XG2G_BOUQUET="Favourites,Movies,Sports"
```

All channels will be merged with sequential numbering.

---

## API Endpoints

- `GET /files/playlist.m3u` - Generated M3U playlist
- `GET /files/xmltv.xml` - Generated XMLTV EPG
- `GET /api/status` - Service status
- `POST /api/refresh` - Trigger manual refresh
- `GET /healthz` - Health check
- `GET /readyz` - Readiness check

---

## Using with IPTV Software

### Threadfin (xTeve Successor)

1. Add M3U source: `http://xg2g:8080/files/playlist.m3u`
2. Add XMLTV source: `http://xg2g:8080/files/xmltv.xml`
3. Create filter and enable Auto-Map

See complete guide: [examples/full-stack/README.md](examples/full-stack/README.md)

### Jellyfin / Plex

1. Add M3U Tuner: `http://xg2g:8080/files/playlist.m3u`
2. Add XMLTV Guide: `http://xg2g:8080/files/xmltv.xml`

**Note**: For best results, use Threadfin as middleware between xg2g and Jellyfin.

---

## Troubleshooting

### No Channels Found

Check your bouquet name:
```bash
# List available bouquets
curl http://192.168.1.100/api/bouquets
```

### Streams Don't Play

1. Verify stream port (usually `8001`, sometimes alternative ports)
2. Test direct stream:
   ```bash
   curl -I http://192.168.1.100:8001/1:0:1:...
   ```
3. Check firewall settings on your receiver

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
- [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) - Production deployment guides

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
