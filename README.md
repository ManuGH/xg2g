# xg2g

## 🛰️ Turn your Enigma2 receiver into a universal IPTV server

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![Latest Release](https://img.shields.io/github/v/release/ManuGH/xg2g)](https://github.com/ManuGH/xg2g/releases/latest)
[![Docker Pulls](https://img.shields.io/docker/pulls/manugh/xg2g)](https://hub.docker.com/r/manugh/xg2g)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Stream satellite/cable TV to **any device** - Plex, Jellyfin, iPhone, VLC, Kodi - **everything works**.

[Quick Start](#install) • [Features](#features) • [Documentation](docs/) • [Helm Chart](deploy/helm/xg2g/)

---

## Install

```bash
docker run -d \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://RECEIVER_IP \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_INITIAL_REFRESH=true \
  ghcr.io/manugh/xg2g:latest
```

**Done!** Now open: `http://YOUR_IP:8080/files/playlist.m3u`

> **Note:** `XG2G_INITIAL_REFRESH=true` loads channels at startup. Without it, you'll see 0 channels until you manually
> trigger a refresh via the API.

---

## Features

- **Zero Config**: Auto-detects your receiver and configures everything automatically.
- **Universal App**: One container does it all - API, M3U, XMLTV, and Stream Proxy.
- **Plex/Jellyfin Ready**: Built-in HDHomeRun emulation and H.264 stream repair.
- **Smart Transcoding**: Auto-detects if you need audio transcoding (iOS) or video transcoding (Bandwidth).
- **Modern UI**: Beautiful web interface for channel management.
- **Ultra-Fast Audio Transcoding** - Native Rust remuxer: AC3/MP2 → AAC (1.4ms latency, 140x faster, <0.1% CPU)
- **Instant Tune** - Smart caching for <1ms channel switching (New in v3.1)
- **7-Day EPG** - Full electronic program guide in XMLTV format
- **GPU Transcoding** - Hardware-accelerated video transcoding (AMD/Intel/NVIDIA)
- **Enterprise-Grade** - Prometheus metrics, OpenTelemetry tracing, health checks
- **Production-Ready** - SLSA L3 attestation, SBOM, Cosign signing, Helm charts
- **Built-in WebUI** - Manage and monitor your instance directly from the browser.

## WebUI (New in v3.0.0)

xg2g now includes a built-in WebUI for easier management and monitoring.
By default, it is available at: `http://localhost:8080/ui/`

**Features:**

- **Dashboard:** View system health, uptime, and component status.
- **Channels:** Browse available bouquets and channels, check EPG status.
- **Logs:** View recent warnings and errors directly in the browser.
- **Config:** View current configuration (read-only).

**📖 [Read the WebUI Guide](docs/guides/WEBUI.md)** for a full tour of features and API endpoints.

To disable the WebUI, set `XG2G_WEBUI_ENABLED=false` (not yet implemented, currently always on).

**Build Requirements:**

- **Go 1.25+** (stable release) - [Install from go.dev](https://go.dev/dl/)
- **Rust 1.70+** (for native audio transcoder) - [Install via rustup](https://rustup.rs/)
- **FFmpeg libraries** (libavcodec, libavformat) - Required for AC3/AAC codecs
- Docker (optional, for containerized deployment)

---

## Why xg2g?

| Feature | xg2g | Traditional IPTV Proxy |
|---------|------|------------------------|
| iPhone Safari Audio | ✅ Auto-fixed | ❌ Broken AC3/MP2 |
| GPU Acceleration | ✅ Hardware transcode | ❌ CPU only |
| Plex Auto-Discovery | ✅ HDHomeRun emulation | ❌ Manual M3U |
| Security | ✅ SLSA L3, SBOM, signed | ❌ No attestation |
| Observability | ✅ Metrics, tracing, logs | ❌ Basic logging |
| Production Ops | ✅ Helm, K8s, health checks | ❌ DIY deployment |

### Architecture

```text
┌─────────────────────────────────────────────────────────────┐
│                     xg2g Gateway                            │
├─────────────────────────────────────────────────────────────┤
│  HTTP API (:8080)                                           │
│  ├─ M3U Playlist (Enigma2 → IPTV format)                   │
│  ├─ EPG/XMLTV (7-day guide)                                 │
│  ├─ HDHomeRun Emulation (Plex/Jellyfin auto-discovery)     │
│  └─ Health & Metrics (/healthz, /readyz, /metrics)         │
├─────────────────────────────────────────────────────────────┤
│  Stream Proxy (:18000) - Optional MODE 2 & 3               │
│  ├─ Audio Transcoding (AC3/MP2 → AAC) - Rust Remuxer      │
│  ├─ Smart Port Selection (8001 vs 17999)                   │
│  └─ GPU Transcoding (VAAPI) - MODE 3                       │
├─────────────────────────────────────────────────────────────┤
│  Background Workers                                         │
│  ├─ Channel Refresh (periodic sync)                        │
│  ├─ EPG Collection (7-day window)                          │
│  └─ SSDP Announcer (network discovery)                     │
└─────────────────────────────────────────────────────────────┘
         ↓ OpenWebif API (HTTP/HTTPS)
┌─────────────────────────────────────────────────────────────┐
│              Enigma2 Receiver (VU+, Dreambox)              │
│  ├─ Bouquet Management                                      │
│  ├─ Live Streams (8001: clear, 17999: encrypted)          │
│  └─ EPG Data Provider                                       │
└─────────────────────────────────────────────────────────────┘
```

**📖 See [Architecture Documentation](docs/ARCHITECTURE.md) for detailed component design and data flow.**

---

## Use It

### In VLC/Kodi

Open this URL: `http://YOUR_IP:8080/files/playlist.m3u`

### In Plex/Jellyfin

Enable auto-discovery:

```bash
-e XG2G_HDHR_ENABLED=true
```

Plex/Jellyfin will find it automatically.

### On iPhone/iPad

Add stream proxy for working audio:

```bash
docker run -d \
  -p 8080:8080 \
  -p 18000:18000 \
  -e XG2G_OWI_BASE=http://RECEIVER_IP \
  -e XG2G_BOUQUET=Favourites \
  -e XG2G_INITIAL_REFRESH=true \
  -e XG2G_ENABLE_STREAM_PROXY=true \
  -e XG2G_PROXY_TARGET=http://RECEIVER_IP:17999 \
  ghcr.io/manugh/xg2g:latest
```

**Audio works automatically** in Safari. Rust remuxer converts AC3/MP2 → AAC with **0% CPU overhead**.

No extra setup needed.

---

## Settings

### Must Set

```bash
XG2G_OWI_BASE=http://192.168.1.100    # Your receiver IP
XG2G_BOUQUET=Favourites               # Channel list name
```

### Nice to Have

```bash
XG2G_OWI_USER=root          # If receiver has password
XG2G_OWI_PASS=password      # Receiver password
```

### Turn Off (if needed)

```bash
XG2G_EPG_ENABLED=false      # No TV guide
XG2G_HDHR_ENABLED=false     # No Plex/Jellyfin auto-discovery
```

### TLS / HTTPS (Security)

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `XG2G_TLS_CERT`      | Path to Certificate (.pem) | "" (Disabled) |
| `XG2G_TLS_KEY`       | Path to Private Key (.pem) | "" (Disabled) |
| `XG2G_FORCE_HTTPS`   | Redirect HTTP -> HTTPS | `false` |

**Behavior:**

- **Not Set**: Server runs on HTTP (default).
- **Set**: Server runs on HTTPS. HTTP requests are dropped (unless `FORCE_HTTPS` is true).

### Advanced

```bash
XG2G_PROXY_ONLY_MODE=true   # Run as dedicated transcoding proxy only
                            # Disables: API server, metrics, SSDP discovery
                            # Use case: Multi-container deployments with separate
                            # transcoding instances to avoid port conflicts
```

Everything else works automatically.

---

## Troubleshooting

### Problem: 0 channels / Empty playlist

**Cause:** By default, xg2g doesn't load channels at startup.

**Solution:** Add `XG2G_INITIAL_REFRESH=true` to your configuration:

```bash
docker run -d \
  -e XG2G_INITIAL_REFRESH=true \
  ...
```

Or manually trigger a refresh (requires API token):

```bash
# Set token
-e XG2G_API_TOKEN=$(openssl rand -hex 16)

# Trigger refresh
curl -X POST -H "X-API-Token: YOUR_TOKEN" http://localhost:8080/api/refresh
```

### Problem: Connection errors

Check these common issues:

```bash
# 1. Verify receiver is reachable
curl http://RECEIVER_IP

# 2. Check OpenWebIF credentials
curl http://USER:PASS@RECEIVER_IP/api/statusinfo

# 3. View xg2g logs
docker logs xg2g

# 4. Check status endpoint
curl http://localhost:8080/api/status
```

### Problem: Wrong bouquet name

Bouquet names are **case-sensitive**. To find the correct name:

1. Open `http://RECEIVER_IP` in browser
2. Navigate to your bouquets
3. Copy the exact name (including spaces and capitalization)

For more help, see [SUPPORT.md](SUPPORT.md).

---

## Quick Start

**One command to rule them all.**

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"   # API & Web UI
      - "18000:18000" # Stream Proxy (Plex/iOS)
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100  # Your Receiver IP
      - XG2G_BOUQUET=Favourites             # Your Bouquet Name
```

That's it! xg2g will:

1. Connect to your receiver.
2. Generate M3U and XMLTV.
3. Start the Stream Proxy for Plex/iOS compatibility.
4. Enable HDHomeRun emulation for auto-discovery.

**📖 [Read the full Deployment Guide](docs/guides/PRODUCTION_DEPLOYMENT.md)** for advanced configurations (GPU,
multiple instances, etc).

## Health Checks & Observability

xg2g provides production-ready health and readiness endpoints for monitoring:

- **`/healthz`** - Liveness probe (always returns 200 when process is running)
- **`/readyz`** - Readiness probe (returns 503 until service is fully initialized)
- **`/api/v1/status`** - Detailed status with channel counts and last refresh time
- **`/metrics`** - Prometheus metrics endpoint

### Docker Compose

Use `/healthz` for Docker healthchecks (liveness):

```yaml
healthcheck:
  test: wget -q -T 5 -O /dev/null http://localhost:8080/healthz || exit 1
  interval: 30s
  timeout: 10s
  retries: 3
```

### Kubernetes

Use both probes for sophisticated orchestration:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 20

readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

**📖 See [Health Checks Documentation](docs/operations/HEALTH_CHECKS.md) for best practices and troubleshooting.**

---

## Docker Image Tags

We provide optimized images for different hardware:

- **`latest`**: Best for most users (Modern CPUs, Raspberry Pi 4/5)
- **`v3-performance`**: For modern servers (Haswell/Zen+)
- **`v1-compat`**: For old hardware (<2010)

**📖 [See the Docker Images Guide](docs/guides/DOCKER_IMAGES.md)** for deep dive into CPU levels and architecture support.

## Development

Want to build from source or contribute?

**📖 [Read the Contributing Guide](CONTRIBUTING.md)** to get started with:

- Building from source
- Running tests
- CI/CD pipeline details

## KNOWN LIMITATIONS (The "Brutal Truth" Section)

While xg2g aims for a "Zero Config" experience, there are architectural and practical limitations you must be aware of before deploying in production (v2.0).

### 1. Memory Usage (EPG)

The EPG generator is **memory hungry**. It loads the entire program guide (7 days * all channels) into RAM, converts it, and writes it out.

- **Risk**: On low-RAM devices (Raspberry Pi Zero/3), the process may OOM (Out Of Memory) crash during EPG updates.
- **Workaround**: Reduce `XG2G_EPG_DAYS` to 1 or 2 if you experience crashes.

### 2. Stream Start Latency (Receiver WebIF Dependency)

xg2g queries the Enigma2 WebIF *before every stream start* (service info, codec info, scrambling state).

- **Risk**: Sluggish receiver UI → slow stream start.
- **Risk**: HDD spin-up time → 3–6 seconds blocking.
- **Risk**: Flood of concurrent clients → WebIF overload.
- **Risk**: Timeshift/Recording → WebIF might respond incorrectly or slowly.

*xg2g cannot compensate for these hardware effects.*

### 3. iOS, macOS & Plex "False Client" Detection

xg2g decides based on User-Agent whether to deliver HLS or PES.

- **Limitation**: iOS → HLS (works reliably).
- **Limitation**: macOS Safari → PES (technically correct, but users often expect HLS).
- **Limitation**: Plex Mobile → Identifies as "Linux", not iOS.
- **Limitation**: Plex Server → Acts as a "false" client, causing the server to transcode, which often leads to stuttering.

*This is technically not solvable cleanly because User-Agents are incomplete.*

### 4. No Built-in Authentication

The stream proxy (`:18000`) has **no authentication**.

- **Security Warning**: Do NOT expose this port to the internet. Anyone who finds it can tune your receiver and watch TV. Use a VPN or reverse proxy (Nginx/Traefik) with Basic Auth if remote access is needed.

### 5. Rust Transcoder GC Pressure

The experimental Rust audio remuxer allocates new memory buffers for every audio packet.

- **Performance**: While latency is low, this creates high Garbage Collector (GC) pressure in Go. On very weak CPUs, this might cause CPU spikes.

## Help

- **API Documentation:** [API Reference](https://manugh.github.io/xg2g/api.html)
- **Permissions Guide:** [PERMISSIONS.md](docs/security/PERMISSIONS.md) - Docker, Kubernetes, and GitHub Actions permissions
- **Docker Guide:** [DOCKER_COMPOSE_GUIDE.md](docs/guides/DOCKER_COMPOSE_GUIDE.md)
- **Production Guide:** [PRODUCTION_DEPLOYMENT.md](docs/guides/PRODUCTION_DEPLOYMENT.md)
- **Known Issues:** [KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md)
- **How-to guides:** [docs/](docs/)
- **Questions:** [Discussions](https://github.com/ManuGH/xg2g/discussions)
- **Problems:** [Issues](https://github.com/ManuGH/xg2g/issues)

---

**MIT License** - Free to use
