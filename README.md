# xg2g

<div align="center">
  <img src="docs/images/logo.png" alt="xg2g Logo" width="200"/>
  <h3>The Ultimate Gateway for Your Satellite TV</h3>
  <p>Turn your Enigma2 receiver into a modern, universal IPTV powerhouse.</p>

  [![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
  [![Docker Pulls](https://img.shields.io/docker/pulls/manugh/xg2g?color=blue)](https://hub.docker.com/r/manugh/xg2g)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

  [Quick Start](#-quick-start) • [Features](#-features) • [WebUI](#-modern-webui) • [Docs](docs/)
</div>

---

## 🚀 Why xg2g?

**Stop struggling with M3U playlists and broken audio.**

xg2g is the missing link between your classic Enigma2 receiver (VU+, Dreambox) and the modern streaming world. It wraps your receiver in a powerful API that makes it compatible with **everything**.

| Feature | xg2g | Standard Enigma2 |
| :--- | :---: | :---: |
| **Plex / Jellyfin** | ✅ Auto-Discovery (HDHomeRun) | ❌ Manual Config Hell |
| **Safari / iPhone** | ✅ Native HLS (AAC + compatibility fixes) | ❌ Codec/Playback Issues |
| **iPhone Audio** | ✅ Auto-Transcode (AC3→AAC) | ❌ Silent (Codec Error) |
| **Channel Switching** | ✅ Instant (< 1ms cache) | 🐢 Slow |
| **Management** | ✅ Beautiful Web Dashboard | ❌ Clunky Old WebIF |

---

## ✨ Features

### 🔌 Zero Config

Forget about editing config files. xg2g auto-detects your receiver, scans your bouquets, and configures itself. Just point it at your box and go.

### 📱 Universal Compatibility

- **Plex & Jellyfin**: Appears as a native HDHomeRun tuner. DVR, Live TV, and Guide just work.
- **Safari / iOS / Apple TV**: Native HLS playback with compatible audio/video outputs.
- **VLC & Kodi**: Generates standard M3U playlists and XMLTV guides.

### ⚡ Rust-Powered Performance

Built with a hybrid Go/Rust architecture. The critical audio transcoding path is handled by a custom Rust remuxer that provides **1.4ms latency** with virtually zero CPU overhead.

---

## 🖥️ Modern WebUI

**New in v3.0!** Manage your streams with a sleek, dark-mode dashboard.

<div align="center">
  <img src="docs/images/dashboard_mockup.png" alt="xg2g Dashboard" width="800"/>
</div>

- **Visual Health Checks**: Instantly see if your receiver, EPG, or streams are having issues.
- **Stream Inspector**: Monitor active transcode sessions and bandwidth usage.
- **Log Viewer**: Debug issues without digging into the command line.

---

## 🛡️ Streaming Hardening Flags

Optional Flags für mehr Stabilität unter Last:

- `XG2G_MAX_CONCURRENT_STREAMS` — Limitiert gleichzeitige Medien-Sessions (Proxy/HLS/Repair/Transcode). Default `0` (unbegrenzt). Gilt nicht für Control-Plane (`/api`, `/healthz`, `/readyz`, `/metrics`, `/lineup`, `/discover`, `/files`). Bei Überschreitung: HTTP 429.
  - Beispiel: `export XG2G_MAX_CONCURRENT_STREAMS=3`
- `XG2G_TRANSCODE_FAIL_OPEN` — Verhalten bei ffmpeg-Fehlern in Repair/Transcode: Default `false` (502 zum Client). `true` → Warn-Log und Fallback auf Direkt-Proxy (kein Format-Mapping, best effort).
  - Beispiel: `export XG2G_TRANSCODE_FAIL_OPEN=true`
- `XG2G_BIND_INTERFACE` — Überschreibt den Host für Listen-Adressen vom Typ `:PORT`. Explizite `host:port` bleiben unverändert. Akzeptiert IP/Host oder `if:<interface>` (erste nicht-loopback IPv4). Keine IPv6-Auflösung; Start bricht ab, wenn keine IPv4 gefunden wird. In Docker bindest du damit an die Container-IP.
  - Beispiel: `export XG2G_BIND_INTERFACE=192.168.1.10` oder `export XG2G_BIND_INTERFACE=if:eth0`
- `XG2G_FFMPEG_LOGLEVEL` — Setzt `-loglevel` für alle ffmpeg-basierten Pipelines (Repair/Transcode/HLS-Profile). Default unset (bestehendes Verhalten). Zulässig: `quiet, panic, fatal, error, warning, info, verbose, debug, trace`.
  - Beispiel: `export XG2G_FFMPEG_LOGLEVEL=warning`
- `XG2G_PROXY_IDLE_TIMEOUT` — Bricht Medien-Sessions ab, wenn für die Dauer kein Byte an den Client ging. Default `0` (aus). Nur Medienpfade, nicht Control-Plane.
  - Beispiel: `export XG2G_PROXY_IDLE_TIMEOUT=60s`

---

## 🚀 Quick Start (2min)

```bash
git clone https://github.com/ManuGH/xg2g
cd xg2g

# 1. Configure
cp .env.example .env
nano .env  # or vim/code - Edit XG2G_OWI_BASE, XG2G_BOUQUET, etc.

# 2. Start
docker compose up -d

# 3. Access
# WebUI: http://localhost:8080
# Playlist: http://localhost:8080/files/playlist.m3u
```

## 🛠️ Local Development (Go 1.25 required)

**Docker (Recommended):**

```bash
docker compose up -d
```

**Local:**

```bash
go install golang.org/dl/go1.25.5@latest
go1.25.5 download
export PATH=$HOME/sdk/go1.25.5/bin:$PATH
make dev
```

**That's it.** You can configure via ENV (recommended for Docker) or `--config config.yaml` (YAML).

Config helpers:

- Validate: `xg2g config validate --file config.yaml`
- Dump merged config (defaults + file + env): `xg2g config dump --effective --file config.yaml`
- Reload config at runtime: `SIGHUP` or `POST /api/v2/system/config/reload`

### 🧪 Running Tests Locally

Run the Go unit test suite (recommended for iterating):

```bash
make test
```

Or run everything including race detection, coverage, and security checks (used by CI):

```bash
make codex
```

> **Note**: `make test` requires no special setup. `make codex` requires `golangci-lint` and `govulncheck` (installable via `make dev-tools`).

---

## ✅ Quality Checks (Codex-ready)

- One command to run the review bundle: `make codex` (golangci-lint + race/coverage tests + govulncheck)
- Prereqs: Go 1.25.5, `make`, optional `make dev-tools` to install linters/scanners locally
- Optional extras: `make schema-validate` (config schema), `make security` (SBOM + dependency scanning), `make ui-build` if you changed `webui/`

---

## 🛠️ Advanced Usage

Everything is configured via `.env`. See `.env.example` for all available options, including:

- **Security**: API Tokens, Rate Limiting, HTTPS/TLS
- **Performance**: Audio/Video Bitrates, Buffers
- **Hardware**: GPU Transcoding (Mode 3), Device Mappings

### 🔒 HTTPS/TLS Support

xg2g supports HTTPS out of the box to fix Mixed Content issues with Plex Web (which runs on HTTPS).

#### Option 1: Auto-Generated Self-Signed Certificates (Recommended for local use)

Enable auto-generation on startup:

```bash
# In .env or environment
XG2G_TLS_ENABLED=true
```

xg2g will automatically generate self-signed certificates in `certs/` on first start.

**Automatic Network Detection:** The certificate includes all your server's network IPs (e.g., `10.10.55.14`) in addition to `localhost`, so `https://your-server-ip:8080` works without additional configuration - perfect for Plex accessing xg2g over the network!

#### Option 2: Manual Certificate Generation

```bash
make certs
```

Then configure the paths:

```bash
export XG2G_TLS_CERT=certs/xg2g.crt
export XG2G_TLS_KEY=certs/xg2g.key
```

#### Option 3: Use Your Own Certificates (Production)

```bash
export XG2G_TLS_CERT=/path/to/your/cert.pem
export XG2G_TLS_KEY=/path/to/your/key.pem
```

#### Accepting Self-Signed Certificates

When using self-signed certificates, you'll see a browser warning on first access. This is expected and safe for local use:

1. Navigate to `https://your-server-ip:8080` (e.g., `https://10.10.55.14:8080`)
2. Click "Advanced" → "Proceed to [host] (unsafe)"
3. The certificate will be accepted for your browser session

**For Plex Logo Fix:** Once you've accepted the certificate in your browser, Plex Web (which runs in the same browser) will be able to fetch logos from `https://your-server-ip:8080` without Mixed Content errors.

See [Configuration Guide](docs/guides/CONFIGURATION.md) for more details.

### API Reference (v2)

xg2g now provides a standard OpenAPI v3 REST API.

- **Spec**: [api/openapi.yaml](api/openapi.yaml)
- **Authentication**: `Authorization: Bearer <XG2G_API_TOKEN>`
- **Base URL**: `/api`

See the spec file for full endpoint documentation.

### Hardware Acceleration (Mode 3)

xg2g supports hardware-accelerated video transcoding via VAAPI/NVENC using `ffmpeg` and Rust FFI. This is **disabled by default** to keep the footprint small.

To enable it:

1. Set `MODE_3_ENABLED=true` in `.env`.
2. Ensure your host has GPU drivers installed (e.g. `intel-media-driver`).
3. Uncomment the `devices` section in `docker-compose.yml` if needed.

### Kubernetes Ready

Production-grade from day one. Includes specific endpoints for liveness/readiness probes, Prometheus metrics, and OpenTelemetry tracing.

[Read the Architecture Guide →](docs/ARCHITECTURE.md)

---

## 🤝 Join the Community

We are building the best open-source TV gateway.

- **[Discussions](https://github.com/ManuGH/xg2g/discussions)**: Ask questions and share setups.
- **[Issues](https://github.com/ManuGH/xg2g/issues)**: Report bugs or request features.

---

<div align="center">
  <sub>MIT License • Built with ❤️ by the Open Source Community</sub>
</div>
