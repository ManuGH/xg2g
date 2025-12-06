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
| **iPhone Audio** | ✅ Auto-Transcode (AC3→AAC) | ❌ Silent (Codec Error) |
| **Channel Switching** | ✅ Instant (< 1ms cache) | 🐢 Slow |
| **Management** | ✅ Beautiful Web Dashboard | ❌ Clunky Old WebIF |

---

## ✨ Features

### 🔌 Zero Config

Forget about editing config files. xg2g auto-detects your receiver, scans your bouquets, and configures itself. Just point it at your box and go.

### 📱 Universal Compatibility

- **Plex & Jellyfin**: Appears as a native HDHomeRun tuner. DVR, Live TV, and Guide just work.
- **iOS & Apple TV**: Real-time audio caching creates fully compliant HLS streams from satellite feeds.
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

## 🚀 Quick Start (2min)

```bash
git clone https://github.com/ManuGH/xg2g
cd xg2g

# 1. Configure
cp .env.example .env
nano .env  # or vim/code - Edit OWI_HOST, Bouquets, Modes

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

**That's it.** Configuration is now handled entirely in `.env`.

---

## 🛠️ Advanced Usage

Everything is configured via `.env`. See `.env.example` for all available options, including:

- **Security**: API Tokens, Rate Limiting
- **Performance**: Audio/Video Bitrates, Buffers
- **Hardware**: GPU Transcoding (Mode 3), Device Mappings

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
