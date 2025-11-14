# Running xg2g in LXC Containers (Docker-in-LXC)

## Overview

When running Docker inside an LXC container (e.g., Proxmox LXC), you may encounter errors when starting xg2g or any other containerized service:

```text
Error response from daemon: failed to create task for container: failed to create shim task:
OCI runtime create failed: runc create failed: unable to start container process:
error during container init: open sysctl net.ipv4.ip_unprivileged_port_start file:
reopen fd 8: permission denied: unknown
```

**This is not an xg2g-specific issue**, but a known limitation of running Docker inside LXC containers.

## Root Cause

1. **LXC Security Model**: LXC containers typically do not allow `sysctl` modifications from inside the container for security reasons
2. **Docker Port Mapping**: When Docker publishes ports using `-p host:container`, it attempts to modify `net.ipv4.ip_unprivileged_port_start`
3. **Permission Denied**: LXC blocks this sysctl modification, causing container startup to fail

This affects **all Docker images** (nginx, postgres, redis, etc.), not just xg2g.

## Verification

You can verify this is an LXC limitation by testing with any official Docker image:

```bash
# This will fail with the same sysctl error in LXC:
docker run --rm -p 8080:80 nginx:alpine

# But this works fine:
docker run --rm --network host nginx:alpine
```

Check your environment:

```bash
# Verify you're running in LXC:
systemd-detect-virt
# Output: lxc

# Check if unprivileged user namespaces are enabled:
cat /proc/sys/kernel/unprivileged_userns_clone
# Output: 1 (enabled)
```

## Solution: Use Host Networking

Instead of Docker's bridge networking with port mappings, use **host networking** mode:

### Option 1: Docker Run Command

```bash
docker run --network host ghcr.io/manugh/xg2g:latest
```

With host networking:
- Container binds directly to host's network interfaces
- No Docker network namespace = no sysctl modification attempts
- Services listen on host ports directly (8080, 18000, etc.)

### Option 2: Docker Compose (Recommended)

Use the provided LXC override file:

```bash
docker compose \
  -f docker-compose.yml \
  -f docker-compose.lxc.yml \
  up -d
```

The override file ([docker-compose.lxc.yml](../docker-compose.lxc.yml)) contains:

```yaml
services:
  xg2g:
    network_mode: host
    ports: []  # Remove port mappings (incompatible with host networking)
```

### Port Availability

With host networking enabled, xg2g uses these ports **directly on your LXC host**:

| Port  | Service                              | Required For |
|-------|--------------------------------------|--------------|
| 8080  | HTTP API, M3U Playlist, EPG          | All modes    |
| 18000 | Stream Proxy (audio/video transcoding) | MODE 2 & 3   |
| 8085  | GPU Transcoder API                   | MODE 3 only  |
| 1900  | SSDP (Plex/Jellyfin auto-discovery)  | Optional     |
| 9090  | Prometheus metrics                   | Optional     |

**Make sure these ports are available on your LXC host** before starting xg2g.

## Security Considerations

### Host Networking Implications

`network_mode: host` means:
- ✅ Container services are directly exposed on the host's network interfaces
- ⚠️ No network isolation between container and host
- ⚠️ All container ports are directly accessible from the network

### Recommended Security Practices

For LXC environments, we recommend:

1. **Internal Networks Only**: Only use in trusted, internal networks (not internet-facing)
2. **Reverse Proxy**: Use nginx/Traefik on the LXC host for external access with TLS/authentication
3. **Firewall Rules**: Configure firewall on the Proxmox host if needed
4. **Container Isolation**: Remember that LXC itself provides container isolation from the Proxmox host

### Example: Secure External Access

If you need external access, use a reverse proxy:

```nginx
# nginx on LXC host
server {
    listen 443 ssl http2;
    server_name iptv.example.com;

    ssl_certificate /etc/letsencrypt/live/iptv.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/iptv.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /stream/ {
        proxy_pass http://127.0.0.1:18000/;
        proxy_buffering off;
    }
}
```

Then configure Proxmox firewall to only allow:
- Port 443 (HTTPS) from external networks
- Ports 8080, 18000 only from localhost

## Typical LXC Setup (Proxmox)

### 1. Create LXC Container

In Proxmox web UI:
- **Template**: Debian 12 (bookworm) or Ubuntu 22.04
- **Disk**: 20 GB minimum
- **CPU**: 2 cores minimum (4+ for MODE 2/3)
- **Memory**: 2 GB minimum (4+ for MODE 2/3)
- **Network**: Bridge to your local network

### 2. Enable Nesting and Features

Edit LXC config (`/etc/pve/lxc/<CTID>.conf`):

```bash
# Required for Docker
features: nesting=1

# Optional for better Docker compatibility
lxc.apparmor.profile: unconfined
lxc.cgroup.devices.allow: a
lxc.cap.drop:
```

**Restart the LXC container** after making config changes.

### 3. Install Docker in LXC

Inside the LXC container:

```bash
# Update system
apt update && apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sh

# Verify Docker works
docker run --rm --network host hello-world
```

### 4. Deploy xg2g

```bash
# Create project directory
mkdir -p /opt/xg2g
cd /opt/xg2g

# Download compose files
wget https://raw.githubusercontent.com/ManuGH/xg2g/main/docker-compose.yml
wget https://raw.githubusercontent.com/ManuGH/xg2g/main/docker-compose.lxc.yml

# Edit configuration
nano docker-compose.yml
# Set XG2G_OWI_BASE, XG2G_OWI_USER, XG2G_OWI_PASS, XG2G_BOUQUET

# Start with LXC override
docker compose -f docker-compose.yml -f docker-compose.lxc.yml up -d

# Check logs
docker compose logs -f
```

### 5. Verify Deployment

```bash
# Check container is running
docker ps

# Test API endpoint
curl http://localhost:8080/api/v1/status

# Test M3U playlist
curl http://localhost:8080/files/playlist.m3u
```

## Troubleshooting

### Container Still Won't Start

1. **Check LXC nesting is enabled**:
   ```bash
   grep nesting /etc/pve/lxc/<CTID>.conf
   # Should show: features: nesting=1
   ```

2. **Verify Docker is using host networking**:
   ```bash
   docker inspect xg2g | grep NetworkMode
   # Should show: "NetworkMode": "host"
   ```

3. **Check for port conflicts**:
   ```bash
   ss -tlnp | grep -E ':(8080|18000|8085|1900|9090)'
   # Should not show any existing listeners
   ```

### Performance Issues

If you experience performance issues with MODE 2 (Rust remuxer):

1. **Increase CPU allocation** in Proxmox (4+ cores recommended)
2. **Enable CPU passthrough** for better performance:
   ```bash
   # In /etc/pve/lxc/<CTID>.conf
   lxc.cgroup2.cpu.max: max
   ```
3. **Increase memory** to 4 GB or more

### Network Connectivity Issues

If xg2g can't reach your Enigma2 receiver:

1. **Check LXC network configuration**:
   ```bash
   ip addr show
   # Verify container has an IP on your local network
   ```

2. **Test connectivity from LXC**:
   ```bash
   curl http://192.168.1.100/web/about  # Replace with your receiver IP
   ```

3. **Check Proxmox firewall** is not blocking outbound connections

## Alternative: Native Docker (Not LXC)

If host networking is not acceptable for your use case, consider:

1. **Run Docker in a Proxmox VM** (not LXC) - full kernel isolation, no sysctl restrictions
2. **Run xg2g directly on Proxmox host** - no nested virtualization
3. **Use Proxmox VM with Docker** - supports standard Docker networking with `-p` mappings

## Why Not Fix This in the Image?

This is a **runtime environment limitation**, not an image configuration issue:

- ✅ xg2g image runs as root (no USER directive)
- ✅ Image is correctly built and verified
- ✅ Same image works fine in VMs, bare-metal Docker, Kubernetes
- ❌ Docker + LXC + port mappings = sysctl restriction (applies to ALL images)

The only workaround is to use host networking when running Docker inside LXC.

## References

- [Docker in LXC: Known Issues](https://discuss.linuxcontainers.org/t/docker-inside-lxc/498)
- [Proxmox LXC: Docker Support](https://pve.proxmox.com/wiki/Linux_Container#pct_container_features)
- [Docker Host Networking](https://docs.docker.com/network/host/)
- [LXC Security](https://linuxcontainers.org/lxc/security/)
