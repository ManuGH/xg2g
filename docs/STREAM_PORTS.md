# Stream Port Configuration

This document explains how xg2g handles stream ports for Enigma2 receivers with OSCam Streamrelay support.

## Quick Reference

| Port | Service | Used For |
|------|---------|----------|
| `80` | OpenWebIF | API calls (channel lists, EPG) |
| `8001` | Enigma2 Stream Server | Standard channels |
| `17999` | OSCam Streamrelay | Channels routed through OSCam (requires OSCam EMU) |

---

## OSCam Streamrelay Support

### Automatic Detection (Recommended)

xg2g automatically detects OSCam Streamrelay and routes encrypted channels through it:

```bash
XG2G_SMART_STREAM_DETECTION=true
```

**How it works:**
1. xg2g fetches `/etc/enigma2/whitelist_streamrelay` from your receiver
2. Channels in the whitelist are routed to port `17999` (OSCam Streamrelay)
3. Other channels are routed to port `8001` (standard Enigma2)
4. No manual configuration needed

**Requirements:**
- OSCam EMU installed and configured on your receiver
- Streamrelay enabled in OSCam config
- `/etc/enigma2/whitelist_streamrelay` file exists

---

## Manual Configuration

### Method 1: Single Port for All Channels

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.100"  # ⚠️ NO PORT HERE!
  streamPort: 8001  # or 17999 for OSCam channels
```

### Method 2: Override Specific Channels

```yaml
openWebIF:
  baseUrl: "http://192.168.1.100"
  streamPort: 8001  # Default for most channels

# Override specific service references
streamPortOverrides:
  "1:0:19:EF10:421:1:C00000:0:0:0:": 17999  # RTL HD
  "1:0:19:EF74:3F9:1:C00000:0:0:0:": 17999  # SAT.1 HD
```

---

## Troubleshooting

### Streams don't play

**Test which port works:**
```bash
# Test standard channel
curl -I http://192.168.1.100:8001/1:0:19:132F:3EF:1:C00000:0:0:0:

# Test OSCam Streamrelay channel
curl -I http://192.168.1.100:17999/1:0:19:EF10:421:1:C00000:0:0:0:
```

### OSCam Streamrelay not working

1. **Check OSCam is running:**
   ```bash
   ps aux | grep oscam
   ```

2. **Check Streamrelay is enabled:**
   ```bash
   cat /etc/tuxbox/config/oscam-emu/oscam.conf | grep -A 5 "\[streamrelay\]"
   ```

3. **Check whitelist exists:**
   ```bash
   cat /etc/enigma2/whitelist_streamrelay
   ```

4. **Test port 17999 manually:**
   ```bash
   curl -I http://192.168.1.100:17999/1:0:19:EF10:421:1:C00000:0:0:0:
   ```

### Mixed playlist not working

If Smart Stream Detection is enabled but all channels use the same port:

1. **Check logs:**
   ```bash
   docker logs xg2g | grep "whitelist"
   ```

2. **Verify whitelist was loaded:**
   ```
   loaded encrypted channels whitelist for smart port selection
   encrypted_channels=88
   ```

3. **Clear cache and refresh:**
   ```bash
   curl -X POST http://localhost:8080/api/refresh
   ```

---

## Common Scenarios

### Scenario 1: FTA Only (No OSCam)
- Use port `8001`
- No Smart Stream Detection needed
- Configuration: `streamPort: 8001`

### Scenario 2: OSCam Streamrelay (Recommended)
- Enable Smart Stream Detection
- xg2g reads `/etc/enigma2/whitelist_streamrelay`
- Automatic mixed playlist (8001 + 17999)
- Channels routed to correct port based on whitelist

### Scenario 3: Manual Port Assignment
- Use `streamPortOverrides` in config
- Specify each encrypted channel manually
- More control, but requires maintenance

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_SMART_STREAM_DETECTION` | `false` | Enable automatic OSCam Streamrelay detection |
| `XG2G_STREAM_PORT` | `8001` | Default stream port (if detection disabled) |

---

## Notes

- **Port 80 vs Port 8001**: Port 80 is for OpenWebIF API only. Stream URLs always use 8001 or 17999.
- **Whitelist Location**: The whitelist file is read from your receiver via HTTP: `http://receiver/file?file=/etc/enigma2/whitelist_streamrelay`
- **No OSCam?**: If OSCam is not installed, Smart Stream Detection gracefully falls back to port 8001 for all channels.
- **Deterministic**: Once the whitelist is loaded, port selection is deterministic and reliable.

---

## See Also

- [Main README](../README.md) - General configuration
- [Audio Transcoding](AUDIO_TRANSCODING.md) - Audio format conversion
- [Production Deployment](../PRODUCTION_DEPLOYMENT.md) - Production setup guide
