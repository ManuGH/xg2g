# Stream Port Configuration

This document explains how xg2g handles stream ports for Enigma2 receivers, including support for OSCam Streamrelay.

## Overview

Enigma2 receivers typically provide streams on multiple ports:

| Port | Service | Description |
|------|---------|-------------|
| `80` | OpenWebIF | Web interface and API (metadata only) |
| `8001` | Enigma2 Stream Server | Direct transport streams (unencrypted or already decrypted) |
| `17999` | OSCam Streamrelay | Decrypted streams for encrypted channels (Sky, HD+, etc.) |

**Important**: xg2g uses port `80` for OpenWebIF API calls (channel lists, EPG data), but the **stream URLs in the M3U playlist** will use either `8001` or `17999` depending on your configuration.

---

## OSCam Streamrelay

### What is OSCam Streamrelay?

OSCam Streamrelay is a feature of OSCam EMU that runs **directly on your receiver** and provides decrypted streams for encrypted channels.

**How it works**:
1. Enigma2 receives encrypted satellite/cable signal
2. OSCam EMU decrypts the signal using softcam keys
3. OSCam Streamrelay serves the decrypted stream on port `17999`
4. xg2g generates M3U URLs pointing to port `17999`

**Benefits**:
- ✅ Encrypted channels (Sky, HD+, etc.) work seamlessly
- ✅ No additional hardware needed
- ✅ Runs entirely on the receiver

### Checking if OSCam Streamrelay is Active

Test if port 17999 is accessible:

```bash
# Test from xg2g host
curl -I http://YOUR_RECEIVER_IP:17999/

# Expected response: HTTP 200 or 404 (both mean the port is listening)
# No response = OSCam Streamrelay is not active
```

If port 17999 is **not accessible**, check OSCam configuration on your receiver:

```bash
# On the receiver (SSH)
cat /etc/tuxbox/config/oscam.conf | grep -A 3 "\[streamrelay\]"
```

Expected configuration:
```ini
[streamrelay]
enabled = 1
port = 17999
```

If `enabled = 0` or the section is missing, enable it and restart OSCam.

---

## Configuration Methods

xg2g provides three ways to configure stream ports, listed from simplest to most advanced:

### Method 1: Automatic Detection (Recommended)

**Enable Smart Stream Detection** to automatically test ports for each channel:

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.100"  # ⚠️ NO PORT HERE!
  username: "root"
  password: "secret"
```

```bash
# Enable via environment variable
XG2G_SMART_STREAM_DETECTION=true
```

**How it works**:
1. For each channel, xg2g tests multiple ports: `8001`, `17999`
2. The first working port is selected and cached
3. FTA channels → usually port `8001`
4. Encrypted channels → usually port `17999` (if OSCam Streamrelay is active)

**Advantages**:
- ✅ Fully automatic
- ✅ Optimal port per channel
- ✅ Works with mixed FTA/encrypted bouquets

**Performance**: Initial refresh is slower (~10-15 seconds) because each channel is tested. Subsequent refreshes use cached results.

---

### Method 2: Manual Port Configuration (Simple)

**Use a fixed port for all streams:**

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.100"  # ⚠️ IMPORTANT: NO :80 PORT!
  username: "root"
  password: "secret"
  streamPort: 17999  # All streams will use this port
```

**⚠️ Critical**: Do **NOT** include a port in `baseUrl`!

❌ **Wrong**:
```yaml
baseUrl: "http://192.168.1.100:80"  # Port in baseUrl
streamPort: 17999  # This will be IGNORED!
```

✅ **Correct**:
```yaml
baseUrl: "http://192.168.1.100"  # No port
streamPort: 17999  # This will be used
```

**Why?** If `baseUrl` contains a port, xg2g preserves it and ignores `streamPort`. This is by design to avoid conflicts.

**Use cases**:
- All channels are encrypted → use port `17999`
- All channels are FTA → use port `8001`
- Mixed setup with OSCam Streamrelay active → use port `17999` (works for both)

---

### Method 3: Stream Base Override (Advanced)

**Override the entire stream base URL:**

```bash
# Environment variable
XG2G_STREAM_BASE=http://192.168.1.100:17999
```

Or via systemd:
```ini
[Service]
Environment="XG2G_STREAM_BASE=http://192.168.1.100:17999"
```

**This completely overrides** `baseUrl` and `streamPort` for stream URLs.

**Use cases**:
- Nginx reverse proxy in front of receiver
- Custom streaming infrastructure
- Testing different ports without changing config file

**Priority**: `XG2G_STREAM_BASE` > `streamPort` > default (`8001`)

---

## Decision Flow

```
┌─────────────────────────────────────┐
│ Do you have OSCam Streamrelay?      │
└──────────┬──────────────────────────┘
           │
     ┌─────┴─────┐
     │   Yes     │
     └─────┬─────┘
           │
     ┌─────▼──────────────────────────┐
     │ Is it working? (curl port 17999)│
     └─────┬──────────────────────────┘
           │
     ┌─────┴─────┐
     │   Yes     │
     └─────┬─────┘
           │
     ┌─────▼──────────────────────────────┐
     │ Use Method 2: streamPort: 17999     │
     │ (works for FTA + encrypted channels)│
     └─────────────────────────────────────┘

     ┌─────┴─────┐
     │   No      │
     └─────┬─────┘
           │
     ┌─────▼──────────────────────────┐
     │ Use Method 1: Smart Detection   │
     │ XG2G_SMART_STREAM_DETECTION=true│
     └─────────────────────────────────┘
```

---

## Examples

### Example 1: Vu+ Receiver with OSCam Streamrelay

**Scenario**: Vu+ Uno 4K with OSCam EMU, mixed FTA and Sky channels.

**Solution**: Use port 17999 for all channels:

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://10.10.55.57"  # NO PORT
  username: "root"
  password: "secret"
  streamPort: 17999  # OSCam Streamrelay port

bouquets:
  - "Premium"  # Contains both FTA and encrypted channels

epg:
  enabled: true
  days: 3
```

**Result**: All stream URLs in M3U will use `http://10.10.55.57:17999/...`

---

### Example 2: Dreambox with FTA Only

**Scenario**: Dreambox 920 UHD, only FTA channels, no OSCam.

**Solution**: Use standard Enigma2 stream port:

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.50"
  streamPort: 8001  # Standard Enigma2 stream server

bouquets:
  - "Favourites"
```

**Result**: All stream URLs use `http://192.168.1.50:8001/...`

---

### Example 3: Smart Detection for Mixed Setup

**Scenario**: Unknown receiver configuration, want automatic optimization.

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.100"

bouquets:
  - "All Channels"
```

```bash
# Enable smart detection
XG2G_SMART_STREAM_DETECTION=true xg2g --config config.yaml
```

**Result**: xg2g tests each channel and selects optimal port.

---

### Example 4: Nginx Reverse Proxy

**Scenario**: Nginx proxy with SSL termination in front of receiver.

```yaml
# config.yaml
openWebIF:
  baseUrl: "http://192.168.1.100:80"  # OpenWebIF API
```

```bash
# Override stream base for proxy
XG2G_STREAM_BASE=https://receiver.example.com:443
```

**Result**:
- API calls go to `http://192.168.1.100:80`
- Stream URLs use `https://receiver.example.com:443/...`

---

## Troubleshooting

### Streams don't play

1. **Test direct stream access**:
   ```bash
   # Get a service reference from the M3U file
   curl -I http://YOUR_RECEIVER_IP:8001/1:0:19:...
   curl -I http://YOUR_RECEIVER_IP:17999/1:0:19:...
   ```

2. **Check which port responds** with HTTP 200

3. **Configure xg2g** to use the working port

### Encrypted channels show black screen

**Problem**: Stream URLs use port `8001` but OSCam Streamrelay is on `17999`.

**Solution**: Configure `streamPort: 17999` (see Method 2 above)

### baseUrl with port breaks streamPort

**Problem**:
```yaml
baseUrl: "http://192.168.1.100:80"
streamPort: 17999  # IGNORED!
```

**Solution**: Remove port from baseUrl:
```yaml
baseUrl: "http://192.168.1.100"
streamPort: 17999  # NOW WORKS!
```

### Smart detection is slow

**Expected behavior**: First refresh with smart detection takes 10-15 seconds as each channel is tested.

**Optimization**: Results are cached. Subsequent refreshes are fast (< 1 second).

**Alternative**: If you know all channels work on `17999`, use Manual Port Configuration instead.

---

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `XG2G_STREAM_PORT` | Default stream port (overrides config file) | `17999` |
| `XG2G_STREAM_BASE` | Complete stream base URL override | `http://192.168.1.100:17999` |
| `XG2G_SMART_STREAM_DETECTION` | Enable automatic port detection | `true` |

---

## Best Practices

1. ✅ **Always omit port** from `openWebIF.baseUrl` if using `streamPort`
2. ✅ **Test OSCam Streamrelay** before configuring port 17999
3. ✅ **Use Smart Detection** if unsure about your setup
4. ✅ **Use fixed port** (Method 2) for better performance if all channels work on same port
5. ⚠️ **Don't mix** methods - choose one and stick with it

---

## See Also

- [Configuration Examples](../config.example.yaml)
- [Production Deployment Guide](PRODUCTION_DEPLOYMENT.md)
- [Troubleshooting](../README.md#troubleshooting)
