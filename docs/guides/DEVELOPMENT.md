# Development Guide

## Development Workflow

Optional bootstrap helpers:

- `mise install` reads [mise.toml](../../mise.toml) and provisions the pinned
  local Go and Node versions on host machines.
- Reopening the repo in
  [.devcontainer/devcontainer.json](../../.devcontainer/devcontainer.json)
  starts a containerized workstation that runs `make install`,
  `make dev-tools`, and `make doctor` on first create. The devcontainer expects
  access to the host Docker socket for the `make start*` container paths.

### First-Time Setup

Before using any of the development paths below:

```bash
make install
make dev-tools
make start
```

- `make install` bootstraps `.env` and WebUI dependencies.
- `make dev-tools` installs the pinned local CLI tools used by repo workflows.
- `make doctor` verifies only the local workspace.
- `make start` is the standard local entrypoint. It runs `make doctor`, checks
  the Docker runtime, and then starts the default local Compose stack.

### Recommended Paths

Pick one path and stay on it for the task at hand:

- `make start` for the default local container path
- `make start-gpu` for Linux `/dev/dri` hardware acceleration in containers
- `make start-nvidia` for the NVIDIA / NVENC container path
- `make backend-dev` for backend-only work
- `make dev-ui` for frontend-heavy work with Vite HMR
- `make dev` only when you explicitly want the crash-restart loop

Use `make stop`, `make stop-gpu`, or `make stop-nvidia` to shut down the
matching container path again.

### Port Map

- `make start`, `make start-gpu`, `make start-nvidia`: `http://localhost:8088`
- `make backend-dev`: whatever `XG2G_LISTEN` resolves to in `.env` (the default
  local path is typically `:8088`)
- `make dev-ui`: `http://localhost:8080/ui/`

### `run_dev.sh` (Development Loop)

- **Purpose**: Rapid iteration and local debugging.
- **Behavior**: Infinite loop; auto-rebuilds and restarts on crash.
- **Logs**: Captured in `logs/dev.log`.
- **Usage**: Internal dev only; not valid for audit verification. Prefer `make backend-dev` for a single foreground run.

### Fast UI Development

For frontend-heavy work, use the dev-tagged backend instead of rebuilding the embedded production UI:

```bash
make dev-ui
```

- Backend runs on `http://localhost:8080` with `-tags=dev`
- `/ui/` is reverse-proxied to the Vite dev server for HMR
- Production embed behavior stays unchanged for normal builds and containers

Open:

```bash
http://localhost:8080/ui/
```

Advanced two-terminal variant:

```bash
make backend-dev-ui
make webui-dev
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

For standard local development with Compose:

```bash
make start
```

Equivalent raw command:

```bash
docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml up -d
```

Advanced or scriptable alias:

```bash
make up
```

Hardware-specific one-command variants:

```bash
make start-gpu
make start-nvidia
```

Manual VAAPI helper equivalent:

```bash
COMPOSE_FILE=docker-compose.yml:../docker-compose.dev.yml:docker-compose.gpu.yml \
  XG2G_COMPOSE_ROOT="$PWD/deploy" \
  XG2G_ENV_FILE="$PWD/.env" \
  backend/scripts/compose-xg2g.sh up -d
```

The checked-in `deploy/docker-compose.gpu.yml` file is a marker overlay. For
`/dev/dri` hosts, use `backend/scripts/compose-xg2g.sh` so visible
`renderD*` nodes are expanded into device entries at runtime.

Manual NVIDIA helper equivalent:

```bash
COMPOSE_FILE=docker-compose.yml:../docker-compose.dev.yml:docker-compose.nvidia.yml \
  XG2G_COMPOSE_ROOT="$PWD/deploy" \
  XG2G_ENV_FILE="$PWD/.env" \
  backend/scripts/compose-xg2g.sh up -d
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
