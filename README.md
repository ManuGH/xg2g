# xg2g

<div align="center">
  <img src="docs/images/logo.png" alt="xg2g Logo" width="200"/>
  <h3>Production-Ready Streaming Middleware for Enigma2</h3>
  <p>
    Modern event-driven bridge between Enigma2 receivers and
    Plex/Jellyfin/Browsers with HLS transcoding, EPG, and 45-minute timeshift.
  </p>

  [![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
  [![Docker Pulls](https://img.shields.io/docker/pulls/manugh/xg2g?color=blue)](https://hub.docker.com/r/manugh/xg2g)
  [![License: PolyForm Noncommercial](https://img.shields.io/badge/License-PolyForm_Noncommercial-blue.svg)](https://polyformproject.org/licenses/noncommercial/1.0.0)
  [![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)

  [Quick Start](#-quick-start) • [Features](#-features) •
  [v3.0.0 Release](docs/RELEASE-v3.0.0.md) • [Docs](docs/) •
  [Architecture](ARCHITECTURE.md)
</div>

> [!NOTE]
> **Production-Ready for Home-Lab**: xg2g v3 is a single-tenant control plane
> optimized for stability & operational simplicity. Read
> [ARCHITECTURE.md](ARCHITECTURE.md) for scope, threat model, and design
> decisions.

> [!IMPORTANT]
> **License**: xg2g is licensed under the **PolyForm Noncommercial License
> 1.0.0**. This application is free for personal, educational, and non-profit
> use. **Commercial use (e.g., ISPs, Resellers) requires a separate license.**
> See [Licensing](docs/licensing.md) for details.

---

## 💖 Support the Project

If you like **xg2g** and want to support its development (or get access to the "Founder Edition"), please consider donating!

<a href="https://github.com/sponsors/ManuGH">
  <img src="https://img.shields.io/badge/Sponsor-GitHub-ea4aaa?style=for-the-badge&logo=github" alt="Sponsor on GitHub" />
</a>
<a href="https://www.paypal.me/manuelherma">
  <img src="https://img.shields.io/badge/Donate-PayPal-00457C?style=for-the-badge&logo=paypal" alt="Donate with PayPal" />
</a>

Your support keeps the updates coming! 🚀

## 🚀 Why xg2g?

**Your Enigma2 receiver is great at reception, but its web interface is stuck in the past.**

xg2g transforms your receiver into a modern streaming platform. No more VLC plugins, no more buffering, and no more clunky interfaces. Just open your browser and watch.

---

## ✨ Features

### 🎯 v3.0.0 Production Ready

**Feature Complete** and stable for production deployment:

- ✅ **Event-Driven V3 Architecture** with FSM and persistent sessions
- ✅ **100% TypeScript Frontend** (4,132 LOC, strict mode)
- ✅ **RBAC Security** with scoped Bearer tokens
- ✅ **OpenTelemetry** distributed tracing (Jaeger/Tempo)
- ✅ **Zero Legacy Code** - all compatibility layers removed

[Read the full v3.0.0 Release Notes →](docs/RELEASE-v3.0.0.md)

---

### Modern Web Interface (WebUI)

Beautiful, dark-mode accessible dashboard built with React 19 + TypeScript:

- **Live TV**: Instant channel switching (<2ms)
- **EPG & Search**: Browse program guide or search for shows
- **System Dashboard**: Monitor health, uptime, real-time logs

### ⏪ Timeshift (Replay)

Missed a scene? No problem.

- **45-Minute Buffer**: Rewind up to 45 minutes of live TV instantly in your browser.
- **Safari Note**: On Safari (iOS/macOS), playback is **Live-Only** (Pause is supported, but rewinding is currently not available due to native HLS limitations).

### 📱 Perfect Mobile Streaming

- **Native HLS**: On-the-fly remuxing for all modern browsers/devices
- **Audio Transcoding**: Real-time AC3/DTS→AAC via FFmpeg 7.x

### 📺 HDHomeRun Emulation

Emulates HDHomeRun tuner for **Plex** and **Jellyfin** integration:

- SSDP discovery for automatic client detection
- M3U playlist export with bouquet filtering
- XMLTV EPG export (7-day default)

### 🏗️ V3 Streaming Architecture

Event-driven design for reliability and performance:

- **Intent-Based API**: Request Start/Stop intents, receive SessionID
- **Event Bus**: Decouples API from Worker (pub/sub)
- **Finite State Machine**: New→Tuning→Transcoding→Ready→Stopped
- **Persistent Sessions**: BadgerDB/BoltDB with crash recovery
- **HLS Delivery**: 45-minute timeshift buffer, browser-native playback

---

## 🚀 Quick Start

### Docker Compose (Recommended)

1. **Create a folder** and download `docker-compose.yml`:

    ```bash
    mkdir xg2g && cd xg2g
    curl -o docker-compose.yml https://raw.githubusercontent.com/ManuGH/xg2g/main/docker-compose.yml
    ```

2. **Configure environment**:

    ```bash
    # Download template and customize
    curl -o .env https://raw.githubusercontent.com/ManuGH/xg2g/main/.env.example
    nano .env
    ```

    **Set these values in `.env`:**
    - `XG2G_OWI_BASE` - Your Enigma2 receiver IP (e.g., `http://192.168.1.100`)
    - `XG2G_BOUQUET` - Optional: comma-separated bouquets (empty = all)

    **Note**: `XG2G_V3_E2_HOST` automatically inherits from `XG2G_OWI_BASE` if not set.

3. **Start it up:**

    ```bash
    docker compose up -d
    ```

**That's it.**

- **Open your browser:** [http://localhost:8088](http://localhost:8088)

---

## 📦 Deployment Options

xg2g supports multiple deployment methods. Choose what fits your use case:

| Method | Use Case | Auto-Restart | Isolation |
|--------|----------|--------------|-----------|
| **Docker Compose** | Quick setup, portable environments | ✅ Yes | ✅ Full |
| **systemd Service** | Production servers, low overhead | ✅ Yes | ⚠️ Shared host |
| **Manual Binary** | Development, testing | ❌ No | ❌ None |

**Current Production Recommendation:** systemd service provides optimal performance for dedicated hosts, while Docker Compose offers better portability and isolation for multi-service deployments.

> **Note:** The repository includes both deployment methods. Docker support is maintained for containerized environments, while systemd is the primary production path for bare-metal/VM deployments.

---

## 🛠️ Configuration

xg2g is configured primarily via **Environment Variables**.

| Variable | Description | Default |
| :--- | :--- | :--- |
| `XG2G_OWI_BASE` | URL of your Enigma2 receiver (required for streaming) | - |
| `XG2G_BOUQUET` | Bouquet names to load (comma separated). Empty = all. | empty |
| `XG2G_API_TOKEN` | (Optional) Secures the API with Bearer auth | - |
| `XG2G_EPG_DAYS` | Number of days to fetch EPG | `7` |

👉 **[Read the Full Configuration Guide](docs/guides/CONFIGURATION.md)** for advanced settings, YAML config, and metrics.

---

## 📚 Documentation

### Release Documentation

- **[v3.0.0 Release Notes](docs/RELEASE-v3.0.0.md)**: Migration guide,
  breaking changes, upgrade instructions

### Setup & Configuration

- **[Quick Start Guide](docs/guides/v3-setup.md)**: Production deployment
- **[Configuration Reference](docs/guides/CONFIGURATION.md)**: ENV vars
  and YAML options
- **[NAS Installation](docs/guides/INSTALL_NAS.md)**: Unraid/Synology setup
- **[RBAC Guide](docs/guides/rbac.md)**: Token scopes and endpoint mapping

### Development & Architecture

- **[Development Guide](docs/DEVELOPMENT.md)**: Build, run, debug locally
- **[Architecture](ARCHITECTURE.md)**: System design and V3 internals
- **[V3 FSM Specification](docs/V3_FSM.md)**: Session state machine contract
- **[Stream Resolution](docs/STREAMING.md)**: Operational standards
- **[Architecture Decision Records](docs/adr/)**: ADR-001 to ADR-004
- **[Troubleshooting](docs/TROUBLESHOOTING.md)**: Common issues

---

## 🤝 Community & Support

- **[Discussions](https://github.com/ManuGH/xg2g/discussions)**: Share your setup, ask questions.
- **[Issues](https://github.com/ManuGH/xg2g/issues)**: Report bugs.

License: **PolyForm Noncommercial License 1.0.0**
*(See [Licensing](docs/licensing.md) for commercial inquiries)*
