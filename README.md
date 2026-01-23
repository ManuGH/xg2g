# xg2g - Next Gen to Go

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![License](https://img.shields.io/badge/license-PolyForm%20NC-blue)](LICENSE)

HLS streaming gateway for Enigma2 satellite/DVB-T2 receivers.
Stream to Safari, iOS, Chrome, and any modern browser.

## Why xg2g?

| Your Problem | xg2g Solution |
| :--- | :--- |
| Enigma2 MPEG-TS doesn't work in Safari/iOS | ✅ Universal H.264/AAC/HLS |
| Manual transcoding profiles per device | ✅ Server-enforced policy |
| No observability in streaming stack | ✅ Metrics, logs, health probes |
| Unstable DIY setups | ✅ Production-tested builds |

## Quickstart

**Prerequisites:** Docker + Enigma2 receiver on your network

```bash
docker run -d --name xg2g --net=host \
  -e XG2G_UPSTREAM_HOST="192.168.1.10" \
  ghcr.io/manugh/xg2g:latest
```

Open [http://localhost:8080](http://localhost:8080)

**Next steps:**
[Configuration](docs/guides/CONFIGURATION.md) •
[Architecture](docs/arch/ARCHITECTURE.md) •
[ADRs](docs/ADR/)

## Features

- 🎯 **Universal Delivery**: H.264/AAC/fMP4 for all devices
- 📊 **Observability**: Prometheus, OpenTelemetry, structured logs
- 🔒 **Security**: Fail-closed auth, scope enforcement
- ⚡ **Quality**: CI gates, contract tests, smoke tests

## The Universal Policy

xg2g enforces a strict **Universal Delivery Policy**:

| Component | Specification |
| :--- | :--- |
| **Video** | H.264 (AVC) |
| **Audio** | AAC |
| **Container** | fMP4 (Fragmented MP4) |
| **Protocol** | HLS |

Tier-1 compliant with Apple HLS Guidelines.

**Non-Goals:**

- ❌ HEVC by default (compatibility first)
- ❌ UI transcoding controls (fixed server policy)
- ❌ Browser workarounds (Safari is the reference)
- ❌ Direct copy (always remux to guarantee container)

## Status

| Component | Status | Guarantee |
| :--- | :--- | :--- |
| **API** | Stable (v3) | SemVer |
| **WebUI** | Stable | Thin Client |
| **Streaming** | Production | Universal Policy |

## Documentation

- 📘 [Architecture Overview](docs/arch/ARCHITECTURE.md) - Complete system
  explanation
- 📋 [ADRs](docs/ADR/) - Design decisions and trade-offs
- 🔍 [Repository Audit](docs/arch/AUDIT_REPO_STRUCTURE.md) - Structure
  findings
- ⚙️ [Configuration Guide](docs/guides/CONFIGURATION.md)
- 🏗️ [Development Guide](docs/guides/DEVELOPMENT.md)

## FFmpeg

xg2g requires FFmpeg for media processing. Docker images include a pinned
FFmpeg build (7.1.3) - no manual configuration needed.

For local development: `make setup` builds FFmpeg to `/opt/xg2g/ffmpeg`.
See [FFmpeg Build Guide](docs/ops/FFMPEG_BUILD.md) for details.

## License

[PolyForm Noncommercial 1.0.0](LICENSE)

- ✅ Free for personal, homelab, and educational use
- ❌ Commercial use requires permission
