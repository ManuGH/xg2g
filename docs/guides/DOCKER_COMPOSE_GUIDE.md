# Docker Compose Setups

xg2g provides multiple Docker Compose files for different use cases:

## Quick Reference

| File | Use Case | Command |
|------|----------|---------|
| [`docker-compose.yml`](docker-compose.yml) | **Standard Setup** - Start here | `docker compose up` |
| [`docker-compose.minimal.yml`](docker-compose.minimal.yml) | **GPU Transcoding** - AMD/Intel VAAPI | `docker compose -f docker-compose.minimal.yml up` |
| [`docker-compose.jaeger.yml`](docker-compose.jaeger.yml) | **Telemetry/Tracing** - Performance monitoring | `docker compose -f docker-compose.jaeger.yml up` |
| [`docker-compose.test.yml`](docker-compose.test.yml) | **Testing** - CI/Development | `docker compose -f docker-compose.test.yml up` |

---

## 1. Standard Setup (docker-compose.yml)

**For:** New users, production deployments

**Features:**

- ✅ M3U playlist with channel logos
- ✅ 7-day EPG guide (XMLTV)
- ✅ HDHomeRun emulation (Plex/Jellyfin)
- ✅ Smart stream detection (OSCam 8001/17999)
- ✅ Authentication support

**Usage:**

```bash
# Copy and edit environment variables
cp .env.example .env
nano .env  # Set your receiver IP, credentials, bouquet

# Start
docker compose up -d

# Access
# M3U: http://localhost:8080/files/playlist.m3u
# EPG:  http://localhost:8080/xmltv.xml
```

---

## 2. GPU Transcoding Setup (docker-compose.minimal.yml)

**For:** Users with AMD/Intel GPUs who need hardware transcoding

**Adds to standard:**

- 🎬 GPU-accelerated video transcoding (VAAPI)
- 🔊 Audio transcoding (AAC)
- ⚡ HEVC encoding for better compression

**Requirements:**

- AMD Radeon or Intel GPU with `/dev/dri`
- Host must have VAAPI drivers installed

**Usage:**

```bash
# Build GPU transcoder first
docker build -f Dockerfile.gpu-transcoder -t xg2g-gpu-transcoder:production .

# Start with GPU support
docker compose -f docker-compose.minimal.yml up -d
```

**See also:** [GPU Transcoding Guide](docs/GPU_TRANSCODING.md)

---

## 3. Telemetry/Tracing Setup (docker-compose.jaeger.yml)

**For:** Developers, performance monitoring, troubleshooting

**Adds to standard:**

- 📊 Jaeger tracing UI (http://localhost:16686)
- 🔍 OpenTelemetry instrumentation
- ⏱️ Request timing and performance metrics

**Usage:**

```bash
docker compose -f docker-compose.jaeger.yml up -d

# Access Jaeger UI
open http://localhost:16686
```

**See also:** [Telemetry Quickstart](docs/telemetry-quickstart.md)

---

## 4. Testing Setup (docker-compose.test.yml)

**For:** CI/CD pipelines, development testing

**Features:**

- 🧪 Isolated test environment
- 🔄 Mock Enigma2 receiver
- ✅ Health check validation

**Usage:**

```bash
docker compose -f docker-compose.test.yml up --build --abort-on-container-exit
```

---

## Advanced Setups

Additional compose files in [`./deploy/`](deploy/) directory:

| File | Purpose |
|------|---------|
| [`deploy/docker-compose.production.yml`](deploy/docker-compose.production.yml) | Full production stack with monitoring |
| [`deploy/docker-compose.dev.yml`](deploy/docker-compose.dev.yml) | Development with live reload |
| [`deploy/docker-compose.alpine.yml`](deploy/docker-compose.alpine.yml) | Alpine-based (smaller image) |
| [`deploy/docker-compose.distroless.yml`](deploy/docker-compose.distroless.yml) | Distroless (security hardened) |

---

## Combining Setups

You can combine multiple compose files:

```bash
# Standard + Jaeger tracing
docker compose -f docker-compose.yml -f docker-compose.jaeger.yml up -d

# GPU Transcoding + Jaeger tracing
docker compose -f docker-compose.minimal.yml -f docker-compose.jaeger.yml up -d
```

---

## Environment Variables

All setups use the same environment variables from [`.env.example`](.env.example):

**Required:**

```bash
XG2G_OWI_BASE=http://192.168.1.100
XG2G_OWI_USER=root
XG2G_OWI_PASS=yourpassword
XG2G_BOUQUET=Favourites
XG2G_INITIAL_REFRESH=true
```

**Optional:** See [`.env.example`](.env.example) for 30+ configuration options.

---

## Need Help?

- 📖 **Main Documentation**: [README.md](README.md)
- 💬 **Discussions**: [GitHub Discussions](https://github.com/ManuGH/xg2g/discussions)
- 🐛 **Issues**: [GitHub Issues](https://github.com/ManuGH/xg2g/issues)
