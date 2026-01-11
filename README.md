# xg2g - Next Gen to Go

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![License](https://img.shields.io/badge/license-PolyForm%20NC-blue)](LICENSE)

Production-grade HLS streaming gateway for Enigma2 satellite/DVB-T2
receivers. Stream to Safari, iOS, Chrome, and any modern browser.

> [!NOTE]
> **Product Contract**: Universal streaming policy for modern clients.
> No legacy profile switching or client-side transcoding decisions.

## Why xg2g?

| Your Problem | xg2g Solution |
| :--- | :--- |
| Enigma2 MPEG-TS doesn't work in Safari/iOS | ✅ Universal H.264/AAC/HLS |
| Manual transcoding profiles per device | ✅ Server-enforced policy |
| No observability in streaming stack | ✅ Metrics, logs, health probes |
| Unstable DIY setups | ✅ Production-tested builds |

## Features

- 🎯 **Universal Delivery**: H.264/AAC/fMP4 for all devices
- 📊 **Enterprise Observability**: Prometheus, OpenTelemetry, logs
- 🔒 **Security First**: Fail-closed auth, scope enforcement
- ⚡ **Battle-Tested**: CI quality gates, contract tests

## The Universal Policy

xg2g enforces a strict **Universal Delivery Policy**:

| Component | Specification |
| :--- | :--- |
| **Video** | H.264 (AVC) |
| **Audio** | AAC |
| **Container** | fMP4 (Fragmented MP4) |
| **Protocol** | HLS |

Tier-1 compliant with Apple HLS Guidelines.

## Non-Goals

- ❌ **HEVC by default**: Compatibility first.
- ❌ **UI Transcoding Controls**: Fixed server policy.
- ❌ **Browser Workarounds**: Safari is the reference.
- ❌ **Direct Copy**: Always remux to guarantee container.

## FFmpeg Dependencies

xg2g requires **FFmpeg** for media processing (transcoding, remuxing, probing). To ensure reproducibility and avoid distro drift, the project ships a **pinned FFmpeg build** (currently **7.1.3**).

### Docker / Release builds (automatic)

FFmpeg is **bundled into the container image** and fully configured at runtime.  
✅ No manual PATH or LD_LIBRARY_PATH configuration required.

The build uses:

- **Pinned version**: FFmpeg 7.1.3 (tag `n7.1.3`)
- **Checksum verification**: Source tarball verified before build
- **Build flags**: GPL, x264, x265, VAAPI, HLS, native AAC encoder

### Local development (manual)

If building locally (e.g., Homelab/Dev), use `make setup`:

```bash
make setup  # Builds FFmpeg to /opt/xg2g/ffmpeg
```

**Option 1: Use wrappers** (recommended, scoped LD_LIBRARY_PATH):

```bash
# Wrappers handle LD_LIBRARY_PATH automatically
# Use script wrappers for local dev (they default to /opt/ffmpeg)
export XG2G_FFMPEG_PATH=$(pwd)/scripts/ffmpeg-wrapper.sh
export XG2G_FFPROBE_PATH=$(pwd)/scripts/ffprobe-wrapper.sh
# Or override FFMPEG_HOME if you built to a different location
export FFMPEG_HOME=/opt/xg2g/ffmpeg
```

**Option 2: Manual PATH** (global LD_LIBRARY_PATH):

```bash
export PATH=/opt/xg2g/ffmpeg/bin:$PATH
export LD_LIBRARY_PATH=/opt/xg2g/ffmpeg/lib
```

**Note**: Docker uses wrappers automatically - no configuration needed.

**Developer override**: To use system FFmpeg instead (not recommended):

```bash
export XG2G_FFMPEG_PATH=/usr/bin/ffmpeg
```

## Architecture & Decisions

- 📘 **[Architecture Overview](docs/arch/ARCHITECTURE.md)** - Complete system explanation (10/10)
- 📋 **[Architecture Decision Records (ADRs)](docs/adr/)** - Design rationale & trade-offs
- 🔍 **[Repository Audit](docs/arch/AUDIT_REPO_STRUCTURE.md)** - Structure findings & improvements

Key decisions:
- [ADR-001: Universal Delivery Policy](docs/adr/001-universal-policy.md)
- [ADR-002: Session Management](docs/adr/002-session-management.md)

## Quickstart

**Prerequisites:** Docker + Enigma2 receiver on your network

```bash
docker run -d --name xg2g --net=host \
  -e XG2G_UPSTREAM_HOST="192.168.1.10" \
  ghcr.io/manugh/xg2g:latest
```

Open [http://localhost:8080](http://localhost:8080)

## Configuration

See the **[Config Guide](docs/guides/CONFIGURATION.md)**.

## Architecture

See **[Architecture](ARCHITECTURE.md)** and the **[ADR Index](docs/adr/README.md)**.

## Status

| Component | Status | Guarantee |
| :--- | :--- | :--- |
| **API** | Stable (v3) | SemVer |
| **WebUI** | Stable | Thin Client Passed |
| **Streaming** | Production | Universal Policy |

## License

[PolyForm Noncommercial 1.0.0](LICENSE)

- ✅ Free for personal, homelab, and educational use
- ❌ Commercial use requires permission
