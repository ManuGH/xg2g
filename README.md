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
- **Hardware Acceleration**: VAAPI/NVENC detection.
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
