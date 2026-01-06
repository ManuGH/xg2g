# xg2g Build & Deployment Guide

## Go Toolchain Policy

Source of truth: `go.mod` `go` directive (minor-pinned).

- Policy: pin to a minor version (e.g., `1.25`) and allow patch updates.
- Dockerfiles and CI must match the minor version in `go.mod`.
- CI enforces this via `scripts/check-go-toolchain.sh`.

## Quick Start: Build and Deploy with Docker

### 1. Build Docker Image

```bash
make docker-build
```

This will:
- Build the Docker image for `linux/amd64`
- Load it into your local Docker daemon
- Tag it as `xg2g:trixie-local`, `xg2g:latest`, etc.

**Important:** The `--load` flag is included to ensure the image is available for `docker compose`.

### 2. Restart the Application

```bash
docker compose down && docker compose up -d
```

### 3. View Logs

```bash
docker logs -f xg2g
```

## Development Workflow

### After Code Changes

```bash
# 1. Build new Docker image
make docker-build

# 2. Restart container with new image
docker compose down
docker compose up -d

# 3. Tail logs to verify startup
docker logs -f xg2g
```

### Quick Rebuild (without cache)

```bash
docker buildx build --no-cache --load -t xg2g:trixie-local .
docker compose down && docker compose up -d
```

## Testing

### Run Unit Tests

```bash
go test ./...
```

### Run Specific Test Package

```bash
go test -v ./internal/pipeline/worker
```

### Build Application Binary (non-Docker)

```bash
make build
./bin/xg2g
```

## Common Issues

### Image not found when running `docker compose up`

**Problem:** Running `make docker-build` but image not available for `docker compose`.

**Solution:** Ensure `--load` flag is present in the Makefile `docker-build-cpu` target:

```makefile
docker buildx build \
    --load \
    --platform $(PLATFORMS) \
    ...
```

### Container name conflict

**Problem:** `Error: container name "/xg2g" is already in use`

**Solution:**
```bash
docker rm -f xg2g
docker compose up -d
```

## Build Targets

See `make help` for all available targets:

```bash
make help
```

Key targets:
- `make build` - Build Go binary
- `make docker-build` - Build Docker image (with --load)
- `make test` - Run unit tests
- `make lint` - Run linters
- `make clean` - Clean build artifacts

## Configuration

### V3 Worker Environment Variables

All V3 worker environment variables are documented in:
- **[V3 Environment Variables Reference](docs/V3_ENVIRONMENT_VARIABLES.md)**

Quick reference for common V3 settings:
```bash
XG2G_V3_WORKER_ENABLED=true        # Enable V3 worker
XG2G_V3_WORKER_MODE=standard       # standard or virtual
XG2G_V3_TUNER_SLOTS=0,1,2          # Available tuner slots
XG2G_V3_HLS_ROOT=/data/stream/encoded  # HLS output directory
```

See the full documentation for all 13 available V3 environment variables.
