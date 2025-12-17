# xg2g

<div align="center">
  <img src="docs/images/logo.png" alt="xg2g Logo" width="200"/>
  <h3>The Modern Web Player for Enigma2</h3>
  <p>Watch your satellite TV in the browser with Timeshift, EPG Search, and perfect reliability.</p>

  [![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
  [![Docker Pulls](https://img.shields.io/docker/pulls/manugh/xg2g?color=blue)](https://hub.docker.com/r/manugh/xg2g)
  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

  [Quick Start](#-quick-start) • [Features](#-features) • [Docs](docs/)
</div>

---

## 💖 Support the Project

If you like **xg2g** and want to support its development (or get access to the "Founder Edition"), please consider donating!

<a href="https://github.com/sponsors/ManuGH">
  <img src="https://img.shields.io/badge/Sponsor-GitHub-ea4aaa?style=for-the-badge&logo=github" alt="Sponsor on GitHub" />
</a>
<a href="https://paypal.me/ManuGH">
  <img src="https://img.shields.io/badge/Donate-PayPal-00457C?style=for-the-badge&logo=paypal" alt="Donate with PayPal" />
</a>

Your support keeps the updates coming! 🚀

## 🚀 Why xg2g?

**Your Enigma2 receiver is great at reception, but its web interface is stuck in the past.**

xg2g transforms your receiver into a modern streaming platform. No more VLC plugins, no more buffering, and no more clunky interfaces. Just open your browser and watch.

---

## ✨ Features

### Modern Web Interface (WebUI)

A beautiful, dark-mode accessible dashboard built with React.

- **Live TV**: Instant channel switching (<2ms).
- **EPG & Search**: Browse the full program guide or search specifically for your favorite shows.
- **System Dashboard**: Monitor backend health, uptime, and real-time logs to troubleshoot issues instantly.

### ⏪ Timeshift (Replay)

Missed a scene? No problem.

- **45-Minute Buffer**: Rewind up to 45 minutes of live TV instantly in your browser.
- **Safari Note**: On Safari (iOS/macOS), playback is **Live-Only** (Pause is supported, but rewinding is currently not available due to native HLS limitations).

### 📱 Perfect Mobile Streaming

- **Native HLS**: Streams are remuxed on-the-fly to be compatible with all modern browsers and mobile devices.
- **Rust Audio Engine**: Real-time AC3/DTS to AAC transcoding ensures you get sound on every device, without burning your CPU.

### � HDHomeRun Emulation (Beta)

xg2g also emulates an HDHomeRun tuner, allowing you to connect it to **Plex** or **Jellyfin**.
*(Note: Full Plex/Jellyfin documentation and optimization is coming in a future update.)*

---

## 🚀 Quick Start

### Docker Compose (Recommended)

1. **Create a folder** and download `docker-compose.yml`:

    ```bash
    mkdir xg2g && cd xg2g
    curl -o docker-compose.yml https://raw.githubusercontent.com/ManuGH/xg2g/main/docker-compose.yml
    ```

2. **Configure environment** (create `.env`):

    ```bash
    # IP of your Enigma2 Receiver (VU+, Dreambox, etc.)
    XG2G_OWI_BASE=http://192.168.1.100

    # Bouquet to stream (comma separated)
    XG2G_BOUQUET=Favourites
    ```

3. **Start it up:**

    ```bash
    docker compose up -d
    ```

**That's it.**

- **Open your browser:** [http://localhost:8080](http://localhost:8080)

---

## 🛠️ Configuration

xg2g is configured primarily via **Environment Variables**.

| Variable | Description | Default |
| :--- | :--- | :--- |
| `XG2G_OWI_BASE` | URL of your Enigma2 receiver (Required) | - |
| `XG2G_BOUQUET` | Bouquet names to load (comma separated) | `Favourites` |
| `XG2G_API_TOKEN` | (Optional) Secures the API with Bearer auth | - |
| `XG2G_EPG_DAYS` | Number of days to fetch EPG | `7` |

👉 **[Read the Full Configuration Guide](docs/guides/CONFIGURATION.md)** for advanced settings, YAML config, and metrics.

---

## 🤝 Community & Support

- **[Discussions](https://github.com/ManuGH/xg2g/discussions)**: Share your setup, ask questions.
- **[Issues](https://github.com/ManuGH/xg2g/issues)**: Report bugs.

License: **MIT**
