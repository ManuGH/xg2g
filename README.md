# xg2g - Next Gen to Go

**Enigma2 Streaming Gateway**

> [!NOTE]
> **Product Contract**: Universal streaming policy for modern clients.
> No legacy profile switching or client-side transcoding decisions.

## What is xg2g?

xg2g bridges legacy **Enigma2 receivers** to modern **HLS clients**.
It handles the complex delivery so you don't have to.

**Core Features:**

- **Universal Delivery**: H.264/AAC/fMP4 for all devices.
- **Processing**: Pure Go mit FFmpeg-CLI für Audio-Repair (CPU-only).
- **Thin Client**: Zero-logic WebUI.
- **Enterprise Observability**: Metrics, logging, health probes.

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

### Prerequisites: Linux with Docker, GPU, Enigma2 Receiver

1. **Pull Image**: `docker pull ghcr.io/manugh/xg2g:latest`
2. **Run**:

   ```bash
   docker run -d --name xg2g --net=host -e XG2G_UPSTREAM_HOST="192.168.1.10" ghcr.io/manugh/xg2g:latest
   ```

3. **Open**: `http://localhost:8080`

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

## License: PolyForm Noncommercial 1.0.0
