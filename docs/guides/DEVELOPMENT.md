# Development Guide

## Development Workflow

### `run_dev.sh` (Development Loop)

- **Purpose**: Rapid iteration and local debugging.
- **Behavior**: Infinite loop; auto-rebuilds and restarts on crash.
- **Logs**: Captured in `logs/dev.log`.
- **Usage**: Internal dev only; not valid for audit verification.

### Fast UI Development

For frontend-heavy work, use the dev-tagged backend instead of rebuilding the embedded production UI:

```bash
make backend-dev-ui
make webui-dev
```

- Backend runs on `http://localhost:8080` with `-tags=dev`
- `/ui/` is reverse-proxied to the Vite dev server for HMR
- Production embed behavior stays unchanged for normal builds and containers

Open:

```bash
http://localhost:8080/ui/
```

Single-command helper:

```bash
make dev-ui
```

Optional overrides:

- `XG2G_UI_DEV_PROXY_URL=http://127.0.0.1:5173` points the dev backend at a specific Vite instance
- `XG2G_UI_DEV_DIR=/abs/path/to/frontend/webui/dist` serves a local built bundle instead of Vite

### System / Production (Hardened Container)

- **Standard**: **OCI Image is Source of Truth for Runtime.**
- **Supervisor**: **systemd** (manages Docker/Podman lifecycle).
- **Behavior**: Single execution lifecycle; formal hardening (v3.1.4).
- **Usage**: Mandatory for releases, sign-offs, and verification.

## 🛠️ Recommended Shutdown Pattern

### Local Development

To stop the application and its dev-loop safely without killing SSH:

```bash
./backend/scripts/safe-shutdown.sh
```

### Production / System (Docker Compose)

Use the standard lifecycle commands:

```bash
docker compose down
# OR via systemd if installed
systemctl stop xg2g
```

### Local Compose Overrides

For local development with Compose, apply the dev override:

```bash
docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml up -d
```

Optional (VAAPI / Intel+AMD iGPU):

```bash
docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml -f deploy/docker-compose.gpu.yml up -d
```

### Fast Container Rebuilds

For container-based backend iteration, you can cache FFmpeg in a separate local base image and skip recompiling it on every rebuild:

```bash
make docker-ffmpeg-base
make docker-dev-fast
```

`make docker-ffmpeg-base` builds `xg2g-ffmpeg:8.1` once from [Dockerfile.ffmpeg-base](../../Dockerfile.ffmpeg-base). `make docker-dev-fast` then rebuilds the app container with `XG2G_FFMPEG_BASE_IMAGE=xg2g-ffmpeg:8.1`, so the main [Dockerfile](../../Dockerfile) can reuse the cached FFmpeg runtime layer instead of rebuilding FFmpeg.

The tagged release pipeline follows the same pattern via
`ghcr.io/manugh/xg2g-ffmpeg:8.1`, so release cuts do not recompile FFmpeg
from source on every tag.

If FFmpeg version or build flags change in `backend/scripts/build-ffmpeg.sh`, rebuild the base image first:

```bash
make docker-ffmpeg-base
```

*Note: This script targets only `xg2g` and `run_dev.sh` processes.*

### Containerized Testing

Use `docker stop` to leverage graceful SIGTERM propagation without affecting the host environment:

```bash
docker stop $(docker ps -q --filter name=xg2g)
```

## Process Safety

When stopping processes on a shared dev host, avoid broad signals that can
kill your SSH session:

- **Do**: `pkill -f xg2g` or `pkill -f run_dev.sh` (targeted)
- **Do**: `./backend/scripts/safe-shutdown.sh` (scripted)
- **Don't**: `pkill -u $USER` (kills SSH agent)

For containers, use `docker stop` or `systemctl stop xg2g` instead of
host-wide signals.
