# HDHomeRun Setup in Container Environments

**Status:** Production-Ready
**Version:** 1.5.0+
**Last Updated:** 2025-10-27

## Overview

xg2g provides HDHomeRun emulation for seamless integration with Plex and Jellyfin. However, **SSDP multicast auto-discovery has limitations in containerized environments** (Docker, LXC, Kubernetes).

This guide explains why and provides solutions.

---

## Quick Reference: Access Methods

| Method | URL/Address | Port | Works In |
|--------|-------------|------|----------|
| **M3U Playlist** | `http://YOUR_IP:8080/files/playlist.m3u` | 8080 | All environments |
| **XMLTV EPG** | `http://YOUR_IP:8080/xmltv.xml` | 8080 | All environments |
| **HDHomeRun Auto-Discovery** | SSDP multicast | 1900/udp | Bare-metal, VMs |
| **HDHomeRun Manual** | `YOUR_IP:8080` | 8080 | **Containers (recommended)** |
| **Device Info** | `http://YOUR_IP:8080/discover.json` | 8080 | All environments |
| **Channel Lineup** | `http://YOUR_IP:8080/lineup.json` | 8080 | All environments |

---

## Why Auto-Discovery Doesn't Work in Containers

### Technical Explanation

HDHomeRun uses **SSDP (Simple Service Discovery Protocol)** over **UDP multicast** (239.255.255.250:1900) for automatic device discovery.

**The Problem:**

1. **Multicast Filtering**: Container networking (Docker bridge, LXC veth) uses **multicast snooping** by default
2. **IGMP Membership**: The bridge only forwards multicast to ports that have **joined the multicast group** via IGMP
3. **Passive Listeners**: Plex/Jellyfin listen passively for SSDP announcements but **don't join the multicast group**
4. **Result**: xg2g sends SSDP NOTIFY packets, but the bridge doesn't forward them to Plex/Jellyfin containers

```
┌─────────────┐     SSDP NOTIFY      ┌──────────────┐
│   xg2g      │ ────────────────────>│ Linux Bridge │
│ LXC/Docker  │  239.255.255.250    │  (vmbr0)     │
└─────────────┘                       └──────────────┘
                                            │
                                            X  (filtered - no IGMP join)
                                            │
                                            ▼
                                      ┌──────────────┐
                                      │  Jellyfin    │
                                      │ LXC/Docker   │
                                      └──────────────┘
```

### This is NOT a Bug

This is **expected behavior** in production container environments for:
- **Security**: Prevents multicast flooding
- **Performance**: Reduces unnecessary traffic
- **Isolation**: Containers should not rely on broadcast/multicast

---

## Solutions

### Option 1: Manual Configuration (Recommended)

**Best for**: Production deployments, Docker, LXC, Kubernetes

#### Jellyfin

1. **Settings** → **Live TV**
2. Click **"+ Add"** next to TV Tuner
3. Select **HDHomeRun**
4. Enter **Tuner IP Address**: `YOUR_XG2G_IP:8080` (e.g., `10.10.55.14:8080`)
5. Jellyfin will fetch `/discover.json` and configure automatically

#### Plex

1. **Settings** → **Live TV & DVR**
2. Click **"Set Up Plex DVR"**
3. Select **HDHomeRun**
4. If not auto-detected, click **"Enter IP Address Manually"**
5. Enter: `YOUR_XG2G_IP:8080`

**Advantages:**
- ✅ Works reliably in all container environments
- ✅ No network configuration changes needed
- ✅ More secure (no multicast traffic)
- ✅ Easier to troubleshoot

---

### Option 2: Enable Multicast Support (Advanced)

**Best for**: Bare-metal, homelab with full control, testing

#### For Proxmox LXC

Edit `/etc/network/interfaces` on the Proxmox host:

```bash
auto vmbr0
iface vmbr0 inet static
    address 10.10.55.2/24
    gateway 10.10.55.254
    bridge-ports enp2s0
    bridge-stp off
    bridge-fd 0
    bridge-multicast-snooping 1    # Enable intelligent filtering
    bridge-multicast-querier 1     # Enable IGMP queries
```

Apply changes:
```bash
ifreload -a
# OR reboot
```

#### For Docker

Use `host` network mode (removes container network isolation):

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    network_mode: host  # ⚠️ Removes network isolation
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_OWI_USER=root
      - XG2G_OWI_PASS=yourpassword
```

**Disadvantages:**
- ⚠️ Requires host-level network configuration
- ⚠️ `host` mode removes Docker network isolation
- ⚠️ May still not work with some CNI plugins (Kubernetes)
- ⚠️ More complex troubleshooting

---

## Verification

### Check xg2g is Sending SSDP Announcements

```bash
# On the xg2g container/host
tcpdump -i eth0 -n 'dst host 239.255.255.250 and udp port 1900' -v -c 2
```

Expected output every 30 seconds:
```
IP 10.10.55.14.1900 > 239.255.255.250.1900: UDP, length 290
NOTIFY * HTTP/1.1
HOST: 239.255.255.250:1900
LOCATION: http://10.10.55.14:8080/device.xml
NT: urn:schemas-upnp-org:device:MediaServer:1
NTS: ssdp:alive
```

### Check Multicast Group Membership (Proxmox/Linux)

```bash
# On Proxmox host
bridge mdb show | grep 239.255.255.250
```

Expected: You should see xg2g's veth interface:
```
dev vmbr0 port veth110i0 grp 239.255.255.250 temp
```

### Test Manual Discovery

```bash
curl http://YOUR_XG2G_IP:8080/discover.json
```

Expected output:
```json
{
  "FriendlyName": "xg2g",
  "ModelNumber": "HDHR-xg2g",
  "FirmwareName": "xg2g-1.4.0",
  "DeviceID": "XG2G1234",
  "BaseURL": "http://10.10.55.14:8080",
  "LineupURL": "http://10.10.55.14:8080/lineup.json",
  "TunerCount": 1
}
```

---

## Environment Variables

xg2g v1.4.0+ includes proper SSDP multicast support:

```bash
# HDHomeRun Configuration
XG2G_HDHR_ENABLED=true                           # Enable HDHomeRun emulation (default: true)
XG2G_HDHR_DEVICE_ID=XG2G1234                     # Device ID (default: auto-generated)
XG2G_HDHR_FRIENDLY_NAME=xg2g                     # Display name
XG2G_HDHR_BASE_URL=http://10.10.55.14:8080       # Explicit base URL for SSDP announcements
XG2G_HDHR_TUNER_COUNT=1                          # Number of virtual tuners (default: 4)

# Plex iOS Compatibility (v1.6.0+)
XG2G_PLEX_FORCE_HLS=false                        # Force HLS URLs in lineup.json for Plex iOS compatibility (default: false)
                                                 # When enabled, HDHomeRun lineup returns /hls/ URLs instead of direct MPEG-TS
                                                 # Useful when Plex Server proxies requests, hiding iOS User-Agent
                                                 #
                                                 # RECOMMENDED: Set to 'true' if Plex is your primary client, especially with iOS devices
                                                 # NOTE: Direct clients (VLC, IPTV apps) are unaffected - they don't use HDHomeRun lineup
```

### Best Practices for Plex

**Recommended Configuration for Plex Users:**

```bash
XG2G_HDHR_ENABLED=true
XG2G_PLEX_FORCE_HLS=true   # Enable for iOS compatibility and DVR/Timeshift support
```

**Why enable Force-HLS for Plex?**

1. **iOS Compatibility**: Plex Server proxies iOS client requests with `PlexMediaServer/...` User-Agent, preventing auto-detection
2. **DVR/Timeshift**: HLS provides better compatibility with Plex's DVR and timeshift features
3. **Reliability**: Consistent streaming format across all Plex clients (Desktop, Mobile, TV apps)

**Performance Impact:**
- Adds ~2s initial buffering (HLS segment creation)
- Desktop clients remain fully functional
- Direct IPTV clients (VLC, GSE) unaffected (bypass HDHomeRun lineup)

---

## Troubleshooting

### "No Devices Found" in Plex/Jellyfin

**Solution**: Use **manual configuration** (Option 1 above). This is the recommended approach for containers.

### SSDP Packets Not Arriving

1. **Check xg2g logs** for multicast join:
   ```
   {"message":"joined SSDP multicast group","interface":"eth0"}
   ```

2. **Verify port 1900/udp is not blocked**:
   ```bash
   ss -ulnp | grep 1900
   ```

3. **Check firewall rules** (iptables/nftables)

### Auto-Discovery Works on Host but Not in Container

**Expected behavior**. Containers have isolated network namespaces. Use manual configuration.

---

## Best Practices

### Production Deployments

1. ✅ **Use manual HDHomeRun configuration** in Plex/Jellyfin
2. ✅ **Set explicit `XG2G_HDHR_BASE_URL`** with your server's IP
3. ✅ **Keep multicast snooping enabled** on bridges for security
4. ✅ **Document the IP address** for your team

### Development/Testing

1. ✅ **Use `docker-compose` with bridge networks** (no host mode needed)
2. ✅ **Test manual configuration** before attempting auto-discovery
3. ✅ **Monitor SSDP traffic** with tcpdump if debugging

---

## Summary

| Environment | Auto-Discovery | Manual Config | Recommendation |
|-------------|----------------|---------------|----------------|
| **Bare-metal** | ✅ Works | ✅ Works | Auto-discovery |
| **Virtual Machines** | ✅ Works | ✅ Works | Auto-discovery |
| **Docker (bridge)** | ❌ Limited | ✅ Works | **Manual config** |
| **Docker (host)** | ✅ Works | ✅ Works | Host mode (if acceptable) |
| **LXC (Proxmox)** | ⚠️ Requires config | ✅ Works | **Manual config** |
| **Kubernetes** | ❌ No | ✅ Works | **Manual config** |

**Recommendation for all container deployments**: Use **manual HDHomeRun configuration** with `YOUR_IP:8080`.

---

## See Also

- [Configuration Guide](../CONFIGURATION.md)
- [Production Deployment](../PRODUCTION.md)
- [Docker Compose Examples](../../DOCKER_COMPOSE_GUIDE.md)
