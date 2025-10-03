# xg2g → Threadfin → Jellyfin Full Stack

Complete IPTV pipeline: Enigma2 receiver → M3U/XMLTV → Threadfin (filtering) → Jellyfin (Live TV)

## Architecture

```
┌─────────────┐      ┌─────────┐      ┌───────────┐      ┌──────────┐
│  Enigma2    │──────▶│  xg2g   │──────▶│ Threadfin │──────▶│ Jellyfin │
│  OpenWebif  │ HTTP │ M3U+XML │ File  │  Proxy    │ M3U  │  Live TV │
└─────────────┘      └─────────┘      └───────────┘      └──────────┘
```

## Quick Start

### Tested Configuration ✅

This setup was successfully tested with:
- **Enigma2**: OpenATV receiver at `10.10.55.57`
- **Stream Port**: `17999` (check your receiver, could be `8001`)
- **Bouquet**: `Premium` with 134 channels
- **Result**: All channels working with logos, EPG, and correct channel numbers!

### 1. Configure Environment

```bash
cp .env.example .env
nano .env
```

Fill in:
- `XG2G_OWI_BASE`: Your Enigma2 IP (e.g., `http://10.10.55.57`)
- `XG2G_BOUQUET`: Your bouquet name (e.g., `Premium`)
- `XG2G_STREAM_PORT`: Stream port (usually `8001` or `17999`)

### 2. Start Stack

```bash
docker-compose up -d
```

### 3. Access Services

| Service   | URL                        | Purpose                    |
|-----------|----------------------------|----------------------------|
| xg2g      | http://localhost:8080      | M3U/XMLTV API             |
| Threadfin | http://localhost:34400     | IPTV filtering & mapping  |
| Jellyfin  | http://localhost:8096      | Media server with Live TV |

---

## Step-by-Step Setup Guide

### Part 1: Configure xg2g (Automatic)

xg2g starts automatically and generates:
- **M3U Playlist**: `http://localhost:8080/playlist.m3u`
- **XMLTV EPG**: `http://localhost:8080/xmltv.xml`

Check status:
```bash
curl http://localhost:8080/api/status
```

---

### Part 2: Configure Threadfin

**Access**: http://localhost:34400

#### 2.1 Initial Setup
1. Set admin username/password
2. Click "Save"

#### 2.2 Add xg2g as M3U Source
1. Go to **Playlist** → **M3U**
2. Click **Add new M3U**
3. Fill in:
   - **Name**: `xg2g (Enigma2)`
   - **M3U URL**: `http://xg2g:8080/playlist.m3u`
   - **Update**: `Automatic`
   - **Update interval**: `24 hours`
4. Click **Save**
5. Click **Update** to load channels

#### 2.3 Configure XMLTV
1. Go to **Playlist** → **XMLTV**
2. Click **Add new XMLTV**
3. Fill in:
   - **Name**: `xg2g EPG`
   - **XMLTV URL**: `http://xg2g:8080/xmltv.xml`
   - **Update**: `Automatic`
   - **Update interval**: `12 hours`
4. Click **Save**
5. Click **Update** to load EPG

#### 2.4 Create Filter
1. Go to **Filter**
2. Click **Add Filter**
3. Fill in:
   - **Filter Name**: `threadfin` (lowercase, used in URLs!)
   - **M3U Source**: Select `xg2g (Enigma2)` or similar
   - **XMLTV Source**: Select `xg2g EPG` or similar
   - **Tuner Count**: `8` (or how many concurrent streams you want)
4. Under **Channels**:
   - Click **Select All** to include all channels
   - Click **Auto-Map** button to match M3U with XMLTV automatically
   - Verify that channels show EPG data in the preview
5. Click **Save**

**Note**: "Probing Channel Details" may fail with timeout - this is NORMAL for live streams. Just ignore it and save anyway!

#### 2.5 Get Threadfin URLs for Jellyfin
1. Go to **Settings** → **General**
2. Note down:
   - **M3U URL**: `http://threadfin:34400/m3u/threadfin.m3u` (the filter name from 2.4)
   - **XMLTV URL**: `http://threadfin:34400/xmltv/threadfin.xml`

**Note**: The M3U filename matches your filter name. If you named your filter "Jellyfin Live TV", the URL would be `http://threadfin:34400/m3u/jellyfin_live_tv.m3u`

---

### Part 3: Configure Jellyfin

**Access**: http://localhost:8096

#### 3.1 Initial Setup
1. Follow Jellyfin's initial setup wizard
2. Create admin account
3. Skip media library for now (we'll add Live TV)

#### 3.2 Add Live TV Plugin
1. Go to **Dashboard** → **Plugins** → **Catalog**
2. Install **"Live TV"** plugin
3. Restart Jellyfin

#### 3.3 Configure Live TV
1. Go to **Dashboard** → **Live TV**
2. Click **"+"** to add a new TV source

#### 3.4 Add Threadfin as Tuner
1. Select **M3U Tuner**
2. Fill in:
   - **File or URL**: `http://threadfin:34400/m3u/threadfin.m3u`
   - **User Agent**: Leave empty
   - **Simultaneous Stream Limit**: `8` (optional)
3. Click **Save**

**Important**: Jellyfin does NOT auto-discover Threadfin. You must manually add it as shown above.

#### 3.5 Add EPG Source
1. Still in Live TV settings
2. Go to **"Guide Providers"** tab
3. Click **"+"** → **XMLTV**
4. Fill in:
   - **Name**: `Enigma2 EPG`
   - **File or URL**: `http://threadfin:34400/xmltv/jellyfin.xml`
   - **Update interval**: `12 hours`
5. Click **Save**

#### 3.6 Map Channels to EPG
1. Go to **Dashboard** → **Live TV** → **Channels**
2. For each channel, click **"⋮"** → **Edit**
3. In **"Program Guide"** dropdown, select matching EPG entry
4. Click **Save**

**Tip**: If channel names match exactly, Jellyfin auto-maps most channels!

---

## Verification

### Check Channel Order in Jellyfin
1. Go to **Live TV** in Jellyfin
2. Channels should appear in **bouquet order** (not random)
3. Channel numbers should be 1, 2, 3... matching your Enigma2 bouquet

### Check EPG Data
1. Click on any channel in Jellyfin
2. You should see:
   - Current program
   - Next programs
   - Full 7-day guide

### Test Playback
1. Click any channel to start streaming
2. Stream should start within 2-5 seconds
3. Check video quality and stability

---

## Troubleshooting

### No Channels in Threadfin
**Problem**: M3U update failed

**Solution**:
```bash
# Check xg2g is running
docker logs xg2g

# Test M3U manually
curl http://localhost:8080/playlist.m3u

# Force Threadfin update
# Go to Threadfin → Playlist → M3U → Click "Update"
```

### Channels Missing EPG
**Problem**: XMLTV not loaded or channels not mapped

**Solution**:
1. Check XMLTV URL in Threadfin: `http://xg2g:8080/xmltv.xml`
2. In Threadfin Filter, verify **Auto-Map** matched channels
3. In Jellyfin, manually map unmapped channels

### Streams Won't Play
**Problem**: Wrong stream port or firewall

**Solution**:
1. Check `.env`: `XG2G_STREAM_PORT` should match your receiver (8001 or 17999)
2. Test stream directly:
   ```bash
   curl -I http://YOUR_RECEIVER_IP:17999/1:0:1:...
   ```
3. **Important**: xg2g uses **direct TS streaming** on the stream port (not `/web/stream.m3u`)
   - Format: `http://receiver:17999/<service_ref>`
   - This works better with Threadfin/Jellyfin than nested M3U files

### Threadfin "Probing Channel Details" Fails
**Problem**: Threadfin can't analyze streams

**This is NORMAL and can be ignored!**
- Live streams are infinite, so probing times out
- Streams will still work in Jellyfin
- Just save the filter and continue with Jellyfin configuration

### Channel Order Wrong
**Problem**: Channels not in bouquet order

**Solution**:
1. Check M3U has `tvg-chno` attributes:
   ```bash
   curl http://localhost:8080/playlist.m3u | grep tvg-chno
   ```
2. In Threadfin Filter, ensure "Keep channel order" is enabled
3. Refresh Jellyfin tuner

---

## Maintenance

### Update EPG Daily
xg2g and Threadfin auto-update every 12-24 hours.

Force manual update:
```bash
# Restart xg2g to refresh data
docker restart xg2g

# Or trigger via API
curl -X POST http://localhost:8080/api/refresh
```

### Monitor Logs
```bash
# xg2g logs
docker logs -f xg2g

# Threadfin logs
docker logs -f threadfin

# Jellyfin logs
docker logs -f jellyfin
```

### Backup Configuration
```bash
# Backup all volumes
docker-compose down
docker run --rm -v xg2g-data:/data -v $(pwd)/backup:/backup alpine tar czf /backup/xg2g-data.tar.gz -C /data .
docker run --rm -v threadfin-config:/config -v $(pwd)/backup:/backup alpine tar czf /backup/threadfin-config.tar.gz -C /config .
docker run --rm -v jellyfin-config:/config -v $(pwd)/backup:/backup alpine tar czf /backup/jellyfin-config.tar.gz -C /config .
```

---

## Advanced Configuration

### Multiple Bouquets
In `.env`:
```bash
XG2G_BOUQUET=Premium,Favourites,Sports
```

Channels from all bouquets appear sequentially numbered.

### Custom Channel Groups
In Threadfin Filter, create **Channel Categories** to group channels by type:
- HD Channels
- SD Channels
- Radio
- Premium

---

## Architecture Details

### Data Flow

1. **xg2g** fetches from Enigma2:
   - Bouquet list
   - Channel list
   - EPG data (7 days)
   - Picon URLs

2. **xg2g** generates:
   - `playlist.m3u` with `tvg-chno` (channel numbers)
   - `xmltv.xml` with EPG data

3. **Threadfin** processes:
   - Filters unwanted channels
   - Maps M3U channels to XMLTV EPG
   - Provides virtual tuners for Jellyfin

4. **Jellyfin** displays:
   - Live TV grid with EPG
   - Channels in correct order
   - Program guide

### Network Communication

All services run in Docker network `iptv-stack`:
- xg2g ↔ Enigma2: HTTP (your local network)
- Threadfin → xg2g: `http://xg2g:8080` (Docker network)
- Jellyfin → Threadfin: `http://threadfin:34400` (Docker network)

---

## Performance Tips

### Reduce EPG Load Time
```bash
# In .env, reduce EPG days
XG2G_EPG_DAYS=3  # Default: 7
```

### Increase Concurrent Streams
```bash
# In Threadfin Filter settings
Tuner Count: 16  # Default: 8
```

### Hardware Transcoding in Jellyfin
For better performance, enable hardware acceleration:
1. Dashboard → Playback → Transcoding
2. Select your GPU (Intel QSV, NVIDIA NVENC, etc.)

---

## Support

- **xg2g**: https://github.com/ManuGH/xg2g
- **Threadfin**: https://github.com/Threadfin/Threadfin
- **Jellyfin**: https://jellyfin.org/docs/

---

## License

This setup guide is part of the xg2g project (MIT License).
