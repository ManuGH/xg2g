# xg2g - Next Gen to Go

**Enterprise-Grade Enigma2 Streaming Gateway**

> [!NOTE]
> **Product Contract (v3.1)**: xg2g provides a **single, universal streaming policy** guaranteed to work on all modern clients (iOS/Safari, Chrome, Android). It does not support legacy profile switching or client-side transcoding decisions.

## What is xg2g?

xg2g is a high-performance streaming gateway that bridges legacy **Enigma2/DVB receivers** to modern **HLS/fMP4 clients**. It handles the complex "last mile" delivery so you don't have to.

**Core Features:**

- **Universal Delivery**: One stream format (H.264/AAC/fMP4/HLS) for ALL devices.
- **Hardware Acceleration**: Automatic VAAPI/NVENC detection.
- **Thin Client Architecture**: Zero-logic WebUI; the server controls the experience.
- **Enterprise Observability**: Prometheus metrics, structured logging, and health probing.

## The Universal Policy

xg2g enforce a strict **Universal Delivery Policy**. There are no "streaming profiles". Every client receives:

| Component | Specification |
|---|---|
| **Video** | H.264 (AVC) |
| **Audio** | AAC |
| **Container** | fMP4 (Fragmented MP4) |
| **Protocol** | HLS (HTTP Live Streaming) |

This policy is **Tier-1 compliant with Apple HLS Authoring Guidelines**, ensuring flawless playback on iOS and Safari without special hacks.

## Non-Goals

To maintain reliability and simplicity, xg2g explicitly **DOES NOT** support:

- ❌ **HEVC/x265 by default**: We prioritize compatibility over bandwidth.
- ❌ **Client-Side Transcoding Controls**: The viewer watches; the operator configures.
- ❌ **Browser-Specific Workarounds**: If it breaks in Safari, it's a server bug, not a client setting.
- ❌ **Direct Stream Copy**: We always transcode/remux to guarantee the container format.

## Quickstart

### Prerequisites

- Linux (x86_64) with Docker
- Intel GPU (VAAPI) or NVIDIA GPU (NVENC) recommended
- An Enigma2 receiver (e.g., Dreambox, VU+)

### Installation

1. **Pull the Image**

   ```bash
   docker pull ghcr.io/manugh/xg2g:latest
   ```

2. **Run the Container**

   ```bash
   docker run -d \
     --name xg2g \
     --net=host \
     --device /dev/dri:/dev/dri \
     -e XG2G_UPSTREAM_HOST="192.168.1.10" \
     -e XG2G_STREAMING_POLICY="universal" \
     ghcr.io/manugh/xg2g:latest
   ```

3. **Access the Player**
   Open `http://localhost:8080` in your browser.

## Configuration

xg2g is configured via Environment Variables or `config.yaml`.
See the **[Configuration Guide](docs/guides/CONFIGURATION.md)** for the complete operator contract.

## Architecture

xg2g follows a strict **backend-driven design**. The WebUI is a dumb terminal for the API.
See **[Architecture](docs/architecture/ARCHITECTURE.md)** for system invariants and design principles.

## Status

| Component | Status | Guarantee |
|---|---|---|
| **API** | Stable (v3) | Semantic Versioning |
| **WebUI** | Stable | Thin Client Passed |
| **Streaming** | Production | Universal Policy |

## License

PolyForm Noncommercial License 1.0.0.
