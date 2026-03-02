<!-- GENERATED FILE - DO NOT EDIT. Source: templates/README.md.tmpl -->
# xg2g - Next Gen to Go

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Coverage](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml)
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
  -e XG2G_E2_HOST="http://192.168.1.10" \
  -e XG2G_API_TOKEN="$(openssl rand -hex 32)" \
  -e XG2G_API_TOKEN_SCOPES="v3:admin" \
  ghcr.io/manugh/xg2g:v3.1.8
```

Open [http://localhost:8088/ui/](http://localhost:8088/ui/)

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

- 🚀 [Start Here (10 Minutes)](NEW_HERE.md) - First-commit onboarding path
- 📘 [Architecture Overview](docs/arch/ARCHITECTURE.md) - Complete system
  explanation
- 📋 [ADRs](docs/ADR/) - Design decisions and trade-offs
- 🔍 [Repository Audit](docs/arch/AUDIT_REPO_STRUCTURE.md) - Repository
  structure orientation
- 🌐 [API Reference (GitHub Pages)](https://manugh.github.io/xg2g/) - Generated
  from [backend/api/openapi.yaml](backend/api/openapi.yaml)
- ⚙️ [Configuration Guide](docs/guides/CONFIGURATION.md)
- 🏗️ [Development Guide](docs/guides/DEVELOPMENT.md)
- 🧭 [Workflow Guide](WORKFLOW.md) - Branching, testing, and deployment flow
- 🩺 [CI Failure Playbook](docs/ops/CI_FAILURE_PLAYBOOK.md) - Triage guide for
  failing gates

## FFmpeg

xg2g requires FFmpeg for media processing. Docker images include a pinned
FFmpeg build (7.1.3) - no manual configuration needed.

For local development: `make setup` builds FFmpeg to `/opt/ffmpeg` (Linux)
or your custom prefix. See [FFmpeg Build Guide](docs/ops/FFMPEG_BUILD.md) for
details.

To use your local build:

```bash
export XG2G_FFMPEG_BIN="/opt/ffmpeg/bin/ffmpeg"
export LD_LIBRARY_PATH="/opt/ffmpeg/lib"
```

## Development Tools

Ad-hoc inspection scripts live in `cmd/tools/` and are run directly — no installation needed:

```bash
make verify-bin          # print configured FFmpeg binary path (reads data/config.yaml)
make verify-config-yaml  # parse config.yaml and print Timeouts fields
make verify-app-config   # load data/config.yaml via production loader and print key fields
```

Or invoke directly: `go run ./cmd/tools/verify-bin` (from repo root).

> **Note:** `make verify-config` is a CI gate (configgen diff check) — unrelated to the above.

## Offline Testing

This repository supports deterministic offline testing (air-gap capable).

See: OFFLINE_TEST.md

Quick check:

```bash
export GOTOOLCHAIN=local
export GOPROXY=off GOSUMDB=off GOVCS="*:off"
make quality-gates-offline
```

## License

[PolyForm Noncommercial 1.0.0](LICENSE)

- ✅ Free for personal, homelab, and educational use
- ❌ Commercial use requires permission
