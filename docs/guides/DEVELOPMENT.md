# Development Guide

## Development Workflow

Optional bootstrap helpers:

- `mise install` reads [mise.toml](../../mise.toml) and provisions the pinned
  local Go and Node versions on host machines.
- Reopening the repo in
  [.devcontainer/devcontainer.json](../../.devcontainer/devcontainer.json)
  starts a containerized workstation that runs `make install`,
  `make dev-tools`, and `make doctor` on first create. The devcontainer expects
  access to the host Docker socket for the `make start RUNTIME=...` container
  path.

For a quick map of the repo layout, generated artifacts, and required gates, see
[docs/dev/REPO_MAP.md](../dev/REPO_MAP.md).

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

- `make start` for the default CPU-backed local container path
- `make start RUNTIME=vaapi` for Linux `/dev/dri` hardware acceleration
- `make start RUNTIME=nvidia` for the NVIDIA / NVENC container path
- `make backend-dev` for backend-only work
- `make dev-ui` for frontend-heavy work with Vite HMR
- `make dev` as the short alias for one foreground backend run

Use `make stop RUNTIME=<same-value>` to shut down the matching container path.
The internal `make dev-loop` compatibility target is intentionally absent from
normal help and verification workflows because it restarts crashes indefinitely.

### Port Map

- `make start RUNTIME=base|vaapi|nvidia`: `http://localhost:8088`
- `make backend-dev`: whatever `XG2G_LISTEN` resolves to in `.env` (the default
  local path is typically `:8088`)
- `make dev-ui`: `http://localhost:8080/ui/`

### `run_dev.sh` (Development Loop)

- **Purpose**: Rapid iteration and local debugging.
- **Behavior**: Infinite loop; auto-rebuilds and restarts on crash.
- **Logs**: Captured in `logs/dev.log`.
- **Usage**: Internal compatibility only through `make dev-loop`; not valid for
  audit verification. Prefer `make dev` or `make backend-dev` for a single
  foreground run.

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
- **Deployment**: `deploy/sync.sh --apply --ref <tag|sha>` is the only supported
  installation and upgrade entrypoint.
- **Usage**: Mandatory for releases, sign-offs, and production verification.
- **Boundary**: Make targets never start or stop a production installation.

## 🛠️ Recommended Shutdown Pattern

### Local Development

To stop the application and its dev-loop safely without killing SSH:

```bash
./backend/scripts/safe-shutdown.sh
```

### Production / System (Docker Compose)

Use the installed systemd lifecycle:

```bash
systemctl stop xg2g
```

Direct Compose commands are recovery diagnostics documented in the operator
runbook, not a second supported production lifecycle.

### Local Compose Overrides

For standard local development with Compose:

```bash
make start
make start RUNTIME=vaapi
make start RUNTIME=nvidia
```

Use the same selector when stopping or inspecting a hardware-specific stack:

```bash
make stop RUNTIME=vaapi
make ps RUNTIME=vaapi
make logs RUNTIME=vaapi
```

`backend/scripts/dev-compose.sh` owns the Compose file selection so all local
commands resolve the same base, development, and optional hardware overlays.

### Fast Container Rebuilds

For container-based backend iteration, you can cache FFmpeg in a separate local base image and skip recompiling it on every rebuild:

```bash
make docker-ffmpeg-base
make docker-dev-fast-build
make start
```

`make docker-ffmpeg-base` builds `xg2g-ffmpeg:8.1.2` once from
[Dockerfile.ffmpeg-base](../../Dockerfile.ffmpeg-base).
`make docker-dev-fast-build` then builds the development image with
`XG2G_FFMPEG_BASE_IMAGE=xg2g-ffmpeg:8.1.2`, so the main
[Dockerfile](../../Dockerfile) can reuse the cached FFmpeg runtime layer instead
of rebuilding FFmpeg. Building never starts or restarts a container; lifecycle
changes remain explicit through `make start` and `make stop`.

The tagged release pipeline follows the same pattern via
`ghcr.io/manugh/xg2g-ffmpeg:8.1.2`, so release cuts do not recompile FFmpeg
from source on every tag.

If FFmpeg version or build flags change in `backend/scripts/build-ffmpeg.sh`, rebuild the base image first:

```bash
make docker-ffmpeg-base
```

*Note: This script targets only xg2g development processes.*

### Containerized Testing

Use the development lifecycle so the selected Compose project is scoped
correctly:

```bash
make stop RUNTIME=base
```

## Process Safety

When stopping processes on a shared dev host, avoid broad signals that can
kill your SSH session:

- **Do**: `./backend/scripts/safe-shutdown.sh` (scripted)
- **Don't**: `pkill -u $USER` (kills SSH agent)

For local containers, use `make stop RUNTIME=<same-value>`. For an installed
production service, use `systemctl stop xg2g`.
