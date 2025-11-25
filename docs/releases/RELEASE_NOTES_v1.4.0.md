# xg2g v1.4.0 - HDHomeRun Emulation

## 🎉 Major New Feature: HDHomeRun Emulation

xg2g can now emulate an HDHomeRun TV tuner, allowing **automatic discovery** in Plex and Jellyfin!

### What's New

#### 🔍 Automatic Discovery
- **SSDP/UPnP announcements** - No manual configuration needed
- Plex and Jellyfin automatically detect xg2g as a native TV tuner
- Works across network subnets with multicast

#### 📺 HDHomeRun API
- `/discover.json` - Device discovery endpoint
- `/lineup.json` - Channel lineup with proper numbering
- `/lineup_status.json` - Tuner status
- `/device.xml` - UPnP device description

#### 📡 Perfect EPG Matching
- XMLTV channel IDs automatically remapped to match lineup
- No more "Unknown Program" issues in Plex
- Channels numbered correctly (1, 2, 3... instead of random IDs)

### Quick Start

Enable HDHomeRun emulation:

```bash
docker run -d \
  -p 8080:8080 \
  -p 1900:1900/udp \
  -e XG2G_HDHR_ENABLED=true \
  -e XG2G_HDHR_FRIENDLY_NAME="Enigma2 xg2g" \
  -e XG2G_EPG_ENABLED=true \
  ghcr.io/manugh/xg2g:latest
```

Plex/Jellyfin will automatically discover it!

### Configuration

New environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_HDHR_ENABLED` | `false` | Enable HDHomeRun emulation |
| `XG2G_HDHR_DEVICE_ID` | `XG2G1234` | Device ID |
| `XG2G_HDHR_FRIENDLY_NAME` | `xg2g` | Name shown in apps |
| `XG2G_HDHR_MODEL` | `HDHR-xg2g` | Model name |
| `XG2G_HDHR_FIRMWARE` | `xg2g-1.4.0` | Firmware version |
| `XG2G_HDHR_BASE_URL` | (auto) | Base URL for discovery |
| `XG2G_HDHR_TUNER_COUNT` | `4` | Number of tuners |

### Migration from v1.3.0

**No breaking changes!** HDHomeRun emulation is **optional** and disabled by default.

- M3U/XMLTV workflows continue to work unchanged
- Simply add `XG2G_HDHR_ENABLED=true` to enable auto-discovery
- Existing configurations are fully compatible

### Bug Fixes & Improvements

- **XMLTV remapping** - Channel IDs now match GuideNumber for proper EPG correlation
- **HEAD request support** - `/xmltv.xml` endpoint now supports HEAD requests
- **SSDP announcer** - Graceful shutdown with application context
- **Local IP detection** - Automatic detection for announcements

### Technical Details

- **Protocol**: SSDP (Simple Service Discovery Protocol) on UDP port 1900
- **Multicast**: 239.255.255.250:1900
- **Announcement interval**: 30 seconds
- **Device type**: `urn:schemas-upnp-org:device:MediaServer:1`

### Upgrade Instructions

#### Docker

```bash
docker pull ghcr.io/manugh/xg2g:latest
docker stop xg2g
docker rm xg2g
# Run with new config (add -p 1900:1900/udp if using HDHR)
```

#### Docker Compose

```bash
docker-compose pull
docker-compose up -d
```

Add to your `docker-compose.yml`:
```yaml
ports:
  - "1900:1900/udp"  # For SSDP discovery
environment:
  - XG2G_HDHR_ENABLED=true
```

### Known Issues

- SSDP discovery may not work across VLANs without multicast routing
- Some firewall configurations may block UDP port 1900
- **Workaround**: Manually add tuner in Plex with `http://your-host:8080`

### Full Changelog

- feat: Add HDHomeRun emulation for Plex/Jellyfin integration
- feat: Add SSDP/UPnP discovery for automatic detection
- feat: Add /xmltv.xml HTTP endpoint for EPG access
- feat: Remap XMLTV channel IDs to match lineup GuideNumbers
- fix: Allow HEAD requests for /xmltv.xml endpoint
- fix: Use tvg-id as GuideNumber for proper Plex EPG matching
- docs: Simplify README with clean feature overview

---

**Docker Image**: `ghcr.io/manugh/xg2g:v1.4.0` or `ghcr.io/manugh/xg2g:latest`

**Full Changelog**: https://github.com/ManuGH/xg2g/compare/v1.3.0...v1.4.0
