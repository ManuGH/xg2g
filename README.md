# xg2g

<div align="center">

**🛰️ Turn your Enigma2 receiver into a universal IPTV server**

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ManuGH/xg2g/branch/main/graph/badge.svg)](https://codecov.io/gh/ManuGH/xg2g)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/ManuGH/xg2g/badge)](https://scorecard.dev/viewer/?uri=github.com/ManuGH/xg2g)
[![Latest Release](https://img.shields.io/github/v/release/ManuGH/xg2g)](https://github.com/ManuGH/xg2g/releases/latest)
[![Docker Pulls](https://img.shields.io/docker/pulls/manugh/xg2g)](https://hub.docker.com/r/manugh/xg2g)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Stream satellite/cable TV to **any device** - Plex, Jellyfin, iPhone, VLC, Kodi - **everything works**.

[Quick Start](#install) • [Features](#what-it-does) • [Documentation](docs/) • [Helm Chart](deploy/helm/xg2g/)

</div>

---

## Install

```bash
docker run -d \
  -p 8080:8080 \
  -e XG2G_OWI_BASE=http://RECEIVER_IP \
  -e XG2G_BOUQUET=Favourites \
  ghcr.io/manugh/xg2g:latest
```

**Done!** Now open: `http://YOUR_IP:8080/files/playlist.m3u`

---

## What It Does

✅ **Universal Compatibility** - Works with Plex, Jellyfin, VLC, Kodi, iPhone Safari
✅ **Zero-CPU Audio Transcoding** - Native Rust remuxer: AC3/MP2 → AAC with <0.1% CPU
✅ **7-Day EPG** - Full electronic program guide in XMLTV format
✅ **HDHomeRun Emulation** - Auto-discovery in Plex/Jellyfin (no manual setup)
✅ **GPU Transcoding** - Hardware-accelerated video transcoding (AMD/Intel/NVIDIA)
✅ **Enterprise-Grade** - Prometheus metrics, OpenTelemetry tracing, health checks
✅ **Production-Ready** - SLSA L3 attestation, SBOM, Cosign signing, Helm charts

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

### Advanced

```bash
XG2G_PROXY_ONLY_MODE=true   # Run as dedicated transcoding proxy only
                            # Disables: API server, metrics, SSDP discovery
                            # Use case: Multi-container deployments with separate
                            # transcoding instances to avoid port conflicts
```

Everything else works automatically.

---

## 3 Deployment Modes

xg2g has **3 modes** for different use cases:

### MODE 1: Standard (VLC, Kodi, Plex)

**No audio transcoding.** Original AC3/MP2 audio. Desktop players handle this natively.

```bash
docker compose up -d
```

See: [docker-compose.yml](docker-compose.yml)

### MODE 2: Audio Proxy (iPhone/iPad)

**Audio transcoding** for mobile devices. AC3/MP2 → AAC for Safari compatibility.

```bash
docker compose -f docker-compose.audio-proxy.yml up -d
```

Access streams: `http://localhost:18000/1:0:19:...`

See: [docker-compose.audio-proxy.yml](docker-compose.audio-proxy.yml)

### MODE 3: GPU Transcoding

**Hardware-accelerated video + audio transcoding** using VAAPI. For low-power clients or bandwidth optimization.

```bash
docker compose -f docker-compose.gpu.yml up -d
```

**Requirements:**
- Intel Quick Sync (6th gen+) or AMD GPU with VAAPI support
- Host with `/dev/dri/renderD128` device
- Run `vainfo` on host to verify GPU support

Access streams: `http://localhost:18000/1:0:19:...` (routes through GPU transcoder)

See: [docker-compose.gpu.yml](docker-compose.gpu.yml)

---

### Quick Setup Examples

**Standard mode** (desktop players):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
```

**Audio Proxy mode** (iPhone/iPad):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
      - "18000:18000"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_TARGET=http://192.168.1.100:8001
```

**GPU Transcoding mode** (hardware acceleration):
```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
      - "18000:18000"
      - "8085:8085"
    devices:
      - /dev/dri:/dev/dri
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_ENABLE_GPU_TRANSCODING=true
      - XG2G_ENABLE_STREAM_PROXY=true
```

**Proxy-Only mode** (multi-container with dedicated transcoding instances):
```yaml
services:
  # Main xg2g instance (full features)
  xg2g-main:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_BOUQUET=Favourites
      - XG2G_HDHR_ENABLED=true

  # Dedicated audio transcoding proxy
  xg2g-audio-proxy:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "18000:18000"
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_TARGET=http://192.168.1.100:8001
      - XG2G_PROXY_ONLY_MODE=true

  # Dedicated GPU transcoding proxy
  xg2g-gpu-proxy:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "18001:18000"
    devices:
      - /dev/dri:/dev/dri
    environment:
      - XG2G_OWI_BASE=http://192.168.1.100
      - XG2G_ENABLE_GPU_TRANSCODING=true
      - XG2G_ENABLE_STREAM_PROXY=true
      - XG2G_PROXY_ONLY_MODE=true
```

---

## Docker Image Tags

xg2g provides multiple image tags for different use cases and CPU architectures:

### Standard Tags (Multi-Arch: AMD64-v2 + ARM64)

| Tag | Description | Use Case | Updated |
|-----|-------------|----------|---------|
| `latest` | Stable releases | **Production** | On version tags (`v*`) |
| `main` | Latest development | **Staging/Testing** | Every push to main |
| `v1.2.3` | Specific version | **Pinned deployments** | On version tags |

### CPU-Optimized Tags (AMD64 only)

xg2g supports different x86-64 microarchitecture levels for optimal performance on your hardware:

| Tag | CPU Level | Min CPU Year | Target CPUs | Performance | Compatibility |
|-----|-----------|--------------|-------------|-------------|---------------|
| `v1-compat` | x86-64-v1 | 2003+ | Any AMD64 CPU | Baseline | ✅ Maximum |
| `latest` | x86-64-v2 | 2009+ | Nehalem, Bulldozer+ | Good | ✅ **Recommended** |
| `v3-performance` | x86-64-v3 | 2015+ | Haswell, Zen+ (AVX2) | Excellent | ⚠️ Modern only |

**CPU Level Details:**
- **v1** (x86-64): SSE2 only - runs on any 64-bit CPU (Pentium 4+, Athlon 64+)
- **v2** (x86-64-v2): +SSE3, SSE4.1, SSE4.2, POPCNT - **default**, best balance
- **v3** (x86-64-v3): +AVX, AVX2, BMI1/2, FMA - 10-20% faster for audio/video

### Architecture-Specific Tags

| Tag | Architecture | Description | Availability |
|-----|--------------|-------------|--------------|
| `main-arm64` | ARM64 | Latest dev for ARM | ❌ Releases only |
| `v1.2.3-arm64` | ARM64 | Version for ARM | ✅ On releases |
| `sha-abc123-amd64-v2` | AMD64 | Specific commit + CPU level | ✅ Every push |

**⚠️ ARM64 Build Strategy:**
- **main branch**: AMD64 only (fast CI, ~2-3 min)
- **Release tags** (`v*`): AMD64 + ARM64 (slower, ~60-90 min via QEMU)
- **Nightly canary**: ARM64 cross-compile test (no push, validates builds)
- **Reason**: ARM64 emulation via QEMU is 20-30x slower than native AMD64

**Future optimization (prepared, not active):**
- Cross-compilation setup ready in `Dockerfile.cross-arm64`
- Would reduce ARM64 builds from 60-90 min → 5-10 min on releases
- Activation planned when ARM64 usage increases

If you need ARM64 for testing, use the latest release tag or self-compile.

### Choosing the Right Image

**How to check your CPU level:**
```bash
# On Linux
grep -o 'avx2\|avx\|sse4_2' /proc/cpuinfo | sort -u

# Result interpretation:
# - avx2 present → Use :v3-performance
# - sse4_2 present (no avx2) → Use :latest (v2)
# - neither → Use :v1-compat
```

**Recommendation by hardware:**
- 🖥️ **Modern server** (2015+): `v3-performance` - Best performance
- 🏠 **Home server/NAS** (2010+): `latest` - Balanced (default)
- 📦 **Old hardware** (<2010): `v1-compat` - Maximum compatibility
- 🍇 **Raspberry Pi / ARM**: `latest` - Auto-selects ARM64

### Production Deployment

Use `latest` for stable, tested releases (multi-arch, auto-detects AMD64-v2 or ARM64):
```yaml
image: ghcr.io/manugh/xg2g:latest
```

For specific CPU optimization (AMD64 only):
```yaml
# High-performance (Intel Haswell+, AMD Zen+)
image: ghcr.io/manugh/xg2g:v3-performance

# Legacy compatibility (any 64-bit CPU)
image: ghcr.io/manugh/xg2g:v1-compat
```

See: [docker-compose.production.yml](docker-compose.production.yml)

### Staging/Testing Deployment

Use `main` to test latest development changes:
```yaml
image: ghcr.io/manugh/xg2g:main
```

See: [docker-compose.staging.yml](docker-compose.staging.yml)

**⚠️ Note:** The `:main` tag is automatically updated on every push to main. Use for testing only.

### Rollback to Specific Commit

Pin to a specific commit SHA for reproducibility:
```yaml
image: ghcr.io/manugh/xg2g:sha-abc1234
```

Find commit SHAs at: [github.com/ManuGH/xg2g/commits/main](https://github.com/ManuGH/xg2g/commits/main)

---

## Support Policy

### Supported Platforms

| Platform | Architecture | Minimum CPU | Status | Notes |
|----------|-------------|-------------|--------|-------|
| **Linux (Alpine)** | AMD64-v2 | Intel Nehalem (2009+) | ✅ **Recommended** | Default `:latest` tag |
| **Linux (Alpine)** | AMD64-v3 | Intel Haswell (2015+) | ✅ Supported | `:v3-performance` tag |
| **Linux (Alpine)** | AMD64-v1 | Any 64-bit CPU (2003+) | ✅ Supported | `:v1-compat` tag |
| **Linux (Alpine)** | ARM64 | ARMv8-A+ | ✅ Supported | Release tags only |
| **macOS** | AMD64/ARM64 | macOS 11+ | ⚠️ Best-effort | Build from source |
| **Windows** | AMD64 | Windows 10+ | ⚠️ Best-effort | Build from source |

### Image Matrix

| Use Case | Image Tag | CPU Arch | CPU Level | Build Frequency |
|----------|-----------|----------|-----------|-----------------|
| **Production (stable)** | `:latest` | AMD64 + ARM64 | v2 (SSE4.2) | On version tags |
| **Staging/Testing** | `:main` | AMD64 only | v2 (SSE4.2) | Every main push |
| **High Performance** | `:v3-performance` | AMD64 only | v3 (AVX2) | On version tags |
| **Legacy Compatibility** | `:v1-compat` | AMD64 only | v1 (SSE2) | On version tags |
| **Pinned Version** | `:v1.2.3` | AMD64 + ARM64 | v2 (SSE4.2) | Per release |
| **Specific Commit** | `:sha-abc1234` | AMD64 only | v2 (SSE4.2) | Every push |
| **ARM64 Specific** | `:v1.2.3-arm64` | ARM64 only | Generic | On version tags |

### Toolchain Versions

**Current (2025):**
- Go: 1.25
- Rust: 1.84
- Alpine: 3.22.2
- FFmpeg: 7.x (Alpine package)

**Pinning Strategy:**
- Docker base images: Pinned to minor version
- Go/Rust toolchains: Pinned to patch version for reproducibility
- Cross-compilation: cargo-zigbuild 0.19.7

**FFmpeg Linking Strategy:**

| Approach | Advantages | Trade-offs | Status |
|----------|-----------|------------|--------|
| **Dynamic (Alpine packages)** | Smaller images, system updates | ABI drift risk, runtime deps | ✅ **Current** |
| **Static (pre-built)** | Portable, no runtime deps | Larger images, manual updates | ⚠️ Prepared |

**Current implementation:**
- Uses Alpine's `ffmpeg-libs` package (dynamic linking)
- Pinned to Alpine 3.22.2 for ABI stability
- Rust remuxer links against system FFmpeg libraries
- Runtime dependencies: `libavcodec`, `libavformat`, `libavutil`

**Static linking considerations:**
- Would eliminate runtime FFmpeg dependencies
- Requires pre-built static FFmpeg binaries with musl
- Image size increase: ~50-100 MB
- Activation: Set `FFMPEG_STATIC=true` in Dockerfile (prepared, not active)

**Decision rationale:**
- Alpine package updates via `apk upgrade` more convenient than manual static binaries
- ABI stability ensured by pinning Alpine base version
- Static linking reserved for specialized deployments (airgapped, embedded)

### CI/CD Validation

**Main Branch:**
- ✅ AMD64 builds (v1, v2, v3): ~2-3 min
- ✅ Tests + linting: ~5 min
- ❌ ARM64 builds: Disabled (releases only)

**Release Tags:**
- ✅ AMD64 builds (v1, v2, v3): ~2-3 min
- ✅ ARM64 builds via QEMU: 60-90 min
- ✅ Multi-arch manifests
- ✅ SBOM + Provenance attestation
- ✅ Cosign signing

**Nightly (02:17 UTC):**
- ✅ Cache warming (cargo-chef + Go modules)
- ✅ ARM64 cross-compile canary (no push)
- ✅ Validates cargo-zigbuild toolchain
- ✅ Artifact retention: 14 days
- ⚠️ Failure alerting (optional): Set `SLACK_WEBHOOK` repository secret

---

## Help

- **API Documentation:** [API Reference](https://manugh.github.io/xg2g/api.html)
- **Permissions Guide:** [PERMISSIONS.md](PERMISSIONS.md) - Docker, Kubernetes, and GitHub Actions permissions
- **How-to guides:** [docs/](docs/)
- **Questions:** [Discussions](https://github.com/ManuGH/xg2g/discussions)
- **Problems:** [Issues](https://github.com/ManuGH/xg2g/issues)

---

**MIT License** - Free to use
