# Running xg2g in LXC Containers (Docker-in-LXC)

## TL;DR - Quick Start

**When do I need this?**
- ✅ You're running Docker **inside** a Proxmox LXC container (Docker-in-LXC)
- ❌ NOT needed for: Bare metal Docker, Docker in VMs, or native Proxmox VMs

**How to use:**

```bash
# Standard Docker (Bare Metal, VMs):
docker compose up -d

# Proxmox LXC (Docker-in-LXC) - ALWAYS use the override:
docker compose -f docker-compose.yml -f docker-compose.lxc.yml up -d
```

**Why?**
Docker-in-LXC cannot use port mappings (`-p`) because LXC blocks Docker from modifying kernel sysctl parameters. The override file uses `network_mode: host` to bypass this limitation.

**Security:**
- LXC still provides full isolation between your container and the Proxmox host
- `network_mode: host` only affects Docker's network isolation, not LXC's
- For external access, use a reverse proxy with TLS (see [Security Recommendations](#security-recommendations))

---

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

1. **LXC Security Model**: LXC containers do not allow `sysctl` modifications from inside the container for security reasons
2. **Docker Port Mapping**: When Docker publishes ports using `-p host:container`, it attempts to modify kernel parameters (commonly `net.ipv4.ip_unprivileged_port_start`, though the exact parameter may vary by Docker/kernel version)
3. **Permission Denied**: LXC blocks this sysctl modification, causing container startup to fail with a "permission denied" error

**Important:** This affects **all Docker images** (nginx, postgres, redis, etc.), not just xg2g. The root cause is the same regardless of which sysctl parameter Docker tries to modify - LXC's security model prevents any sysctl changes from within the container.

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

With host networking enabled, xg2g uses these ports **directly on your LXC container's network interfaces**:

| Port  | Service                              | Required For |
|-------|--------------------------------------|--------------|
| 8080  | HTTP API, M3U Playlist, EPG          | All modes    |
| 18000 | Stream Proxy (audio/video transcoding) | MODE 2 & 3   |
| 8085  | GPU Transcoder API                   | MODE 3 only  |
| 1900  | SSDP (Plex/Jellyfin auto-discovery)  | Optional     |
| 9090  | Prometheus metrics                   | Optional     |

**IMPORTANT:** These ports must be available on your LXC container!

#### Checking for Port Conflicts

Before starting xg2g, verify no other service is using these ports:

```bash
# Check if ports are in use
ss -tlnp | grep -E ':(8080|18000|8085|1900|9090)'

# Should return empty output if ports are free
# If you see output, another service is already using these ports
```

#### Resolving Port Conflicts

If ports are already in use, you have three options:

**Option 1: Stop the conflicting service**
```bash
# Find what's using port 8080
ss -tlnp | grep :8080

# Stop the service (example for nginx)
systemctl stop nginx
```

**Option 2: Change xg2g's ports via environment variables**
```yaml
# In docker-compose.yml, add:
environment:
  - XG2G_LISTEN=:8081          # Change API port to 8081
  - XG2G_PROXY_LISTEN=:18001   # Change proxy port to 18001
```

**Option 3: Use a reverse proxy**
Keep xg2g on its default ports and use nginx/Traefik to expose it on different ports externally.

## Security Considerations

### Understanding the Isolation Layers

It's important to understand the two levels of isolation:

#### 1. LXC Container Isolation (Proxmox ← → LXC)

- **LXC provides strong isolation** between the Proxmox host and the LXC container
- The LXC container has its own process tree, filesystem, and network stack
- **This isolation is NOT affected** by `network_mode: host` inside Docker
- The Proxmox host remains fully protected from the Docker container

#### 2. Docker Network Isolation (LXC ← → xg2g Container)

- `network_mode: host` **removes Docker's network namespace isolation**
- xg2g binds directly to the LXC container's network interfaces
- This only affects the boundary between Docker and the LXC container
- **The LXC → Proxmox boundary remains fully isolated**

### In Practice

- xg2g services (8080, 18000, etc.) are exposed on the LXC container's network
- These ports are accessible from your network (same as any LXC service)
- The Proxmox host itself is still protected by LXC's isolation layer

### Recommended Security Practices

#### For Internal Networks (Home Lab, Private Network)

✅ **Safe to use as-is:**
- LXC provides sufficient isolation from the Proxmox host
- Access xg2g directly via the LXC container's IP address
- Perfect for home media servers and internal IPTV streaming

#### For External Access (Internet-Facing)

⚠️ **Do NOT expose LXC container ports directly to the internet!**

Instead, use a **reverse proxy** with proper security:

1. **Reverse Proxy Setup** (nginx, Traefik, or Caddy):
   - Install on the LXC container or Proxmox host
   - Terminate TLS at the proxy
   - Add authentication (HTTP Basic Auth, OAuth, mTLS)
   - Rate limiting and DDoS protection

2. **Firewall Configuration**:
   - Proxmox firewall: Allow only HTTPS (443) from internet
   - Block direct access to ports 8080, 18000, etc. from WAN
   - Allow internal network access only

3. **xg2g Configuration**:
   - Bind to localhost only: `XG2G_LISTEN=127.0.0.1:8080`
   - Only accessible via the reverse proxy
   - Cannot be reached directly from the network

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
