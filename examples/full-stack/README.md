# xg2g → Threadfin → Jellyfin Full Stack

Complete IPTV pipeline: Enigma2 receiver → M3U/XMLTV → Threadfin → Jellyfin

## Architecture

```text
┌─────────────┐      ┌─────────┐      ┌───────────┐      ┌──────────┐
│  Enigma2    │──────▶│  xg2g   │──────▶│ Threadfin │──────▶│ Jellyfin │
│  OpenWebif  │ HTTP │ M3U+XML │ File  │  Proxy    │ M3U  │  Live TV │
└─────────────┘      └─────────┘      └───────────┘      └──────────┘
```

## Quick Start

### 1. Configure

```bash
cp .env.example .env
nano .env
```

Set your values:
- `XG2G_OWI_BASE` - Your Enigma2 receiver IP
- `XG2G_BOUQUET` - Your bouquet name
- `XG2G_STREAM_PORT` - Stream port (default: 8001)

### 2. Start

```bash
docker-compose up -d
```

### 3. Access Services

| Service   | URL                        |
|-----------|----------------------------|
| xg2g      | http://localhost:8080      |
| Threadfin | http://localhost:34400     |
| Jellyfin  | http://localhost:8096      |

## Setup

### Threadfin Configuration

1. Open http://localhost:34400
2. Create admin account
3. Add M3U source: `http://xg2g:8080/playlist.m3u`
4. Add XMLTV source: `http://xg2g:8080/xmltv.xml`
5. Create filter and enable channels
6. Note the filter URLs for Jellyfin:
   - M3U: `http://threadfin:34400/m3u/YOUR_FILTER_NAME.m3u`
   - XMLTV: `http://threadfin:34400/xmltv/YOUR_FILTER_NAME.xml`

**Tip**: Use filter name "threadfin" for simple URLs.

See [Threadfin Integration Guide](../../docs/guides/THREADFIN.md) for detailed setup.

### Jellyfin Configuration

1. Open http://localhost:8096
2. Complete initial setup and create admin account
3. Install **Live TV** plugin from catalog
4. Restart Jellyfin
5. Add M3U Tuner with Threadfin URL
6. Add XMLTV Guide with Threadfin URL
7. Map channels if needed

## Troubleshooting

**Channels not working?**
- Check receiver is in standby (not deep standby)
- Try alternative port: `XG2G_STREAM_PORT=17999`
- Verify stream: `curl -I http://YOUR_RECEIVER_IP:8001/SERVICE_REF`

**No EPG data?**
- Verify: `curl http://localhost:8080/files/xmltv.xml | grep -c '<programme'`
- Check Threadfin XMLTV mapping is correct

**Need help?**
- See main [README](../../README.md) for documentation
- Check [Threadfin Guide](../../docs/guides/THREADFIN.md) for integration details
- Visit [SUPPORT](../../SUPPORT.md) for getting help

## Management

```bash
# View logs
docker-compose logs -f

# Restart specific service
docker-compose restart xg2g
docker-compose restart threadfin
docker-compose restart jellyfin

# Stop everything
docker-compose down

# Reset (deletes all data)
docker-compose down -v
```

## Notes

- Tested with OpenATV/OpenPLi receivers
- Works with single and multiple bouquets
- Supports both standard (8001) and relay (17999) ports
