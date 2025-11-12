# Docker Deployment Guide

This guide explains how to deploy xg2g using Docker and Docker Compose in production.

## Table of Contents

- [Why Docker?](#why-docker)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Production Deployment](#production-deployment)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)

## Why Docker?

Docker provides several advantages for xg2g deployment:

1. **All-in-one package**: Go daemon + Rust remuxer in a single container
2. **Consistent environment**: Works the same everywhere (dev, staging, production)
3. **Easy updates**: Pull new image, restart container
4. **Resource isolation**: CPU/memory limits, security boundaries
5. **No dependency hell**: FFmpeg libraries, Rust runtime all included

## Architecture

The Docker image contains:

```
alpine:3.22
├── xg2g (Go binary, CGO-enabled)
├── libxg2g_transcoder.so (Rust remuxer with ac-ffmpeg)
├── FFmpeg libraries (libavcodec, libavformat, etc.)
└── Runtime dependencies (ca-certificates, tzdata)
```

### Multi-Stage Build

1. **Stage 1**: Build Rust remuxer (ac-ffmpeg)
2. **Stage 2**: Build Go daemon with CGO (links to Rust library)
3. **Stage 3**: Runtime image with both binaries

## Quick Start

### Standard Setup (VLC, Plex, Kodi)

```bash
docker run -d \
  --name xg2g \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_BOUQUET=Favourites \
  ghcr.io/manugh/xg2g:latest
```

Access playlist: `http://localhost:8080/files/playlist.m3u`

### With Audio Transcoding (iPhone/iPad)

```bash
docker run -d \
  --name xg2g \
  -p 8080:8080 \
  -p 18000:18000 \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_ENABLE_STREAM_PROXY=true \
  -e XG2G_PROXY_TARGET=http://192.168.1.100:17999 \
  ghcr.io/manugh/xg2g:latest
```

Access streams: `http://localhost:18000/1:0:19:...`

## Production Deployment

### Using Docker Compose (Recommended)

Create `docker-compose.production.yml`:

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g-production

    # Use host network for simplicity
    network_mode: host

    # Or use bridge mode:
    # ports:
    #   - "18080:18080"   # API
    #   - "18000:18000"   # Stream Proxy

    environment:
      # Receiver
      - XG2G_OWI_BASE=http://192.168.1.100:80
      - XG2G_BOUQUET=Favourites (TV)

      # Server
      - XG2G_LISTEN=:18080

      # Audio Transcoding (enabled by default)
      - XG2G_ENABLE_AUDIO_TRANSCODING=true
      - XG2G_USE_RUST_REMUXER=true

      # Stream Proxy
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_LISTEN=:18000
      - XG2G_PROXY_TARGET=http://192.168.1.100:17999

      # Disable unnecessary features
      - XG2G_EPG_ENABLED=false
      - XG2G_HDHR_ENABLED=false

    volumes:
      - ./data:/data

    restart: unless-stopped

    healthcheck:
      test: wget -q --spider http://localhost:18080/api/status || exit 1
      interval: 30s
      timeout: 10s
      retries: 3
```

Start the service:

```bash
docker compose -f docker-compose.production.yml up -d
```

### Systemd Service (Alternative)

For traditional systemd management:

```ini
[Unit]
Description=xg2g IPTV Gateway
After=docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=10
ExecStartPre=-/usr/bin/docker stop xg2g
ExecStartPre=-/usr/bin/docker rm xg2g
ExecStart=/usr/bin/docker run --rm \
  --name xg2g \
  --network host \
  -e XG2G_OWI_BASE=http://192.168.1.100 \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_ENABLE_STREAM_PROXY=true \
  -e XG2G_PROXY_TARGET=http://192.168.1.100:17999 \
  ghcr.io/manugh/xg2g:latest
ExecStop=/usr/bin/docker stop xg2g

[Install]
WantedBy=multi-user.target
```

Save as `/etc/systemd/system/xg2g.service`:

```bash
sudo systemctl daemon-reload
sudo systemctl enable xg2g
sudo systemctl start xg2g
```

## Configuration

### Environment Variables

#### Required

| Variable | Description | Example |
|----------|-------------|---------|
| `XG2G_OWI_BASE` | Receiver OpenWebif URL | `http://192.168.1.100` |
| `XG2G_BOUQUET` | Bouquet name | `Favourites` |

#### Audio Transcoding (enabled by default)

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_ENABLE_AUDIO_TRANSCODING` | `true` | Enable audio transcoding |
| `XG2G_USE_RUST_REMUXER` | `true` | Use Rust remuxer (0% CPU) |
| `XG2G_AUDIO_CODEC` | `aac` | Output codec (aac) |
| `XG2G_AUDIO_BITRATE` | `192k` | Output bitrate |
| `XG2G_AUDIO_CHANNELS` | `2` | Output channels (stereo) |

#### Stream Proxy

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_ENABLE_STREAM_PROXY` | `false` | Enable stream proxy |
| `XG2G_PROXY_LISTEN` | `:18000` | Proxy listen address |
| `XG2G_PROXY_TARGET` | - | Backend URL (e.g., `http://receiver:17999`) |

#### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `XG2G_LISTEN` | `:8080` | API server listen address |
| `XG2G_EPG_ENABLED` | `true` | Enable EPG collection |
| `XG2G_HDHR_ENABLED` | `true` | Enable HDHomeRun emulation |
| `XG2G_OWI_USER` | - | Receiver username |
| `XG2G_OWI_PASS` | - | Receiver password |

### Port Configuration

| Port | Purpose | Required |
|------|---------|----------|
| 8080 | API & M3U/EPG | Yes |
| 18000 | Stream Proxy | Only if `XG2G_ENABLE_STREAM_PROXY=true` |
| 1900/udp | SSDP Discovery | Only if `XG2G_HDHR_ENABLED=true` |

### Volume Mounts

```bash
-v /path/to/data:/data
```

Stores:
- EPG cache
- Channel logos
- Application state

## Troubleshooting

### Container won't start

Check logs:
```bash
docker logs xg2g
```

Common issues:
- Missing required env vars (`XG2G_OWI_BASE`, `XG2G_BOUQUET`)
- Port already in use (check with `lsof -i :8080`)
- Cannot reach receiver (check network connectivity)

### Audio not working on iPhone

1. Verify stream proxy is enabled:
   ```bash
   docker exec xg2g env | grep PROXY
   ```

2. Check proxy is listening:
   ```bash
   curl -I http://localhost:18000/1:0:19:...
   ```

3. Verify Rust remuxer is loaded:
   ```bash
   docker exec xg2g ls -la /app/lib/libxg2g_transcoder.so
   ```

### High CPU usage

If CPU usage is high during transcoding:

1. Check if Rust remuxer is enabled:
   ```bash
   docker logs xg2g | grep -i remuxer
   ```

2. Verify `XG2G_USE_RUST_REMUXER=true` is set

3. Check FFmpeg is not being used:
   ```bash
   docker exec xg2g ps aux | grep ffmpeg
   ```

### Update to latest version

```bash
docker compose -f docker-compose.production.yml pull
docker compose -f docker-compose.production.yml up -d
```

Or with plain Docker:
```bash
docker pull ghcr.io/manugh/xg2g:latest
docker stop xg2g
docker rm xg2g
# Re-run docker run command
```

## Performance Tuning

### Resource Limits

```yaml
services:
  xg2g:
    # ... other config ...
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 512M
        reservations:
          cpus: '0.5'
          memory: 256M
```

### File Descriptor Limits

```yaml
services:
  xg2g:
    # ... other config ...
    ulimits:
      nofile:
        soft: 8192
        hard: 16384
```

### Security Hardening

```yaml
services:
  xg2g:
    # ... other config ...
    user: "65532:65532"
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE
    read_only: true
    tmpfs:
      - /tmp:noexec,nosuid,nodev,size=200m
```

## Building from Source

To build the Docker image locally:

```bash
docker build -t xg2g:local .
```

With custom CPU optimization:

```bash
docker build \
  --build-arg RUST_TARGET_CPU=x86-64-v3 \
  --build-arg GO_AMD64_LEVEL=v3 \
  -t xg2g:optimized .
```

## Migration from SSH Deployment

If you're currently using SSH-based deployment scripts:

1. **Stop old services**:
   ```bash
   ssh root@server 'systemctl stop xg2g.service xg2g-stream-proxy.service'
   ssh root@server 'pkill -9 xg2g'
   ```

2. **Deploy Docker**:
   ```bash
   scp docker-compose.production.yml root@server:/opt/xg2g/
   ssh root@server 'cd /opt/xg2g && docker compose -f docker-compose.production.yml up -d'
   ```

3. **Remove old files** (optional):
   ```bash
   ssh root@server 'rm -rf /opt/xg2g/old-installation'
   ```

## Support

- **Documentation**: [docs/](../docs/)
- **Issues**: [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
- **Discussions**: [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
