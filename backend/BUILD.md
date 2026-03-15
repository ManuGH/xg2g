# xg2g Build & Deployment Facts

This document captures the technical requirements and targets for building and deploying xg2g.

## 1. Toolchain Requirements

| Component | Policy / Requirement |
| :--- | :--- |
| **Go** | Source of Truth: `backend/go.mod`. Required language version: `1.25`; pinned toolchain: `go1.25.7`. |
| **FFmpeg** | Required for HLS/Transcoding. Version: `6.x` or `7.x`. |
| **Docker** | Required for containerized build/deploy. Supports `buildx`. |
| **Make** | Used as the orchestration layer for all dev tasks. |

## 2. Make Targets

Base command: `make [target]`

| Target | Description |
| :--- | :--- |
| `build` | Compiles the offline-safe backend binary to `./bin/xg2g`. |
| `ui-build` | Builds `frontend/webui/` and copies the bundle into `backend/internal/control/http/dist/`. |
| `docker-build` | Builds the Docker image (AMD64) with `--load` |
| `test` | Runs unit and fast integration tests |
| `lint` | Executes `golangci-lint`, WebUI linting, and design contract checks |

## 3. Deployment Posture

- **Deployment Mode**: Containerized (Docker / Docker Compose).
- **Configuration**: Primarily via [Environment Variables](docs/guides/REFERENCE.md).
- **Persistence**: Requires a volume mount for `XG2G_DATA` to persist EPG and cache.
- **Network**: Exposes HTTP service (default `:8088`).

> [!IMPORTANT]
> Always verify `backend/go.mod` and the root `mk/variables.mk` toolchain pin before local compilation to stay aligned with CI.
