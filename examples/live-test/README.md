# xg2g Live Test Setup

Complete test environment: xg2g → Threadfin → Jellyfin (Mac optimized)

## Quick Start

### 1. Configure

```bash
# Copy and edit environment file
cp .env.example .env
nano .env

# Set your values:
# - XG2G_OWI_BASE=http://YOUR_RECEIVER_IP
# - XG2G_BOUQUET=YOUR_BOUQUET
# - XG2G_STREAM_PORT=8001 (default)
```

### 2. Build Image

```bash
# Build from repo root
cd ../..
docker build -t xg2g:livetest -f Dockerfile .
cd examples/live-test
```

### 3. Start

```bash
docker-compose up -d
```

### 4. Access Services

| Service | URL |
|---------|-----|
| xg2g | http://localhost:8080 |
| M3U Playlist | http://localhost:8080/files/playlist.m3u |
| XMLTV EPG | http://localhost:8080/files/xmltv.xml |
| Threadfin | http://localhost:34400 |
| Jellyfin | http://localhost:8096 |

## Setup

### Threadfin Configuration

1. Open http://localhost:34400 and create admin account
2. Add XMLTV source: `http://xg2g-livetest:8080/files/xmltv.xml`
3. Add M3U playlist: `http://xg2g-livetest:8080/files/playlist.m3u`
4. Create filter and enable channels

See [Threadfin Integration Guide](../../docs/guides/THREADFIN.md) for details.

### Jellyfin Configuration

1. Open http://localhost:8096 and create admin account
2. Install **Live TV** plugin
3. Add M3U Tuner: `http://threadfin-livetest:34400/m3u/threadfin.m3u`
4. Add XMLTV Guide: `http://threadfin-livetest:34400/xmltv/threadfin.xml`

## Troubleshooting

**Streams not working?**
- Try port 17999 instead: `XG2G_STREAM_PORT=17999` in `.env`
- Ensure receiver is in standby (not deep standby)

**No EPG data?**
- Check: `curl http://localhost:8080/files/xmltv.xml | grep -c '<programme'`
- Should be > 0

**Need help?**
- See main [README](../../README.md) for full documentation
- Check [SUPPORT](../../SUPPORT.md) for getting help

## Management

```bash
# View logs
docker-compose logs -f

# Restart services
docker-compose restart

# Stop everything
docker-compose down

# Reset (deletes all data)
docker-compose down -v
rm -rf ./livetest-data/
```
