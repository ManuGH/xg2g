# Development Guide

Quick reference for common development tasks.
For end users, prefer Docker Compose (see README Quick Start).

## 🚀 Quick Start (Development)

### 1. Configure Environment (Required)

Pick one configuration source:

- `.env` file (recommended)
- exported environment variables
- `config.yaml` (YAML config)

Minimum required values for a working start:

- `XG2G_OWI_BASE` (receiver base URL)
- `XG2G_API_TOKEN` (required; UI will prompt)
- `XG2G_API_TOKEN_SCOPES` (include `v3:write` for streaming)

If `XG2G_OWI_BASE` is empty, the UI starts in **Setup Mode** instead of streaming.

Run `make check-env` to validate your `.env` before starting.

### 2. First Time Setup (Using .env)

```bash
# Clone and enter directory
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# Create .env from example
cp .env.example .env

# Edit .env with required settings
nano .env
# Required: XG2G_OWI_BASE=http://YOUR_RECEIVER_IP
# Required: XG2G_API_TOKEN=dev-token
# Required for streaming: XG2G_API_TOKEN_SCOPES=v3:read,v3:write
# Note: XG2G_V3_E2_HOST automatically inherits from XG2G_OWI_BASE if not set
```

### 3. Run Locally (Without Docker)

```bash
# Build and run in one command
make dev

# Convenience wrapper (legacy support)
./run_dev.sh
```

This will:

- Build WebUI (React)
- Embed WebUI in Go binary
- Build Go daemon
- Run with your `.env` configuration

**Access**: <http://localhost:8088>

**Note**: The binary defaults to `:8080`; local dev templates set `XG2G_LISTEN=:8088`.

The UI will prompt for the API token on first load.

`run_dev.sh` is a thin wrapper around `make dev` for convenience.

### 4. Run with Docker Compose

```bash
# Start
make up

# View logs
make logs

# Stop
make down
```

## 🔧 Common Tasks

### Rebuild Everything from Scratch

```bash
# Clean all build artifacts
make clean

# Rebuild
make build
```

### Frontend Development (Separate Dev Server)

For **live frontend development** without rebuilding the backend:

```bash
# Terminal 1: Run backend (serves API only)
go build -o bin/xg2g ./cmd/daemon && ./bin/xg2g

# Terminal 2: Run frontend dev server (hot reload)
cd webui && npm run dev
# Open http://localhost:5173 (Vite dev server with hot reload)
```

**Benefits**: Frontend changes reload instantly, no backend rebuild needed.

### Frontend Only Changes (Production Build)

When you're done and want to **embed** the WebUI into the Go binary:

```bash
# 1. Build WebUI
cd webui && npm run build

# 2. Copy to embed location
cd .. && cp -r webui/dist/* internal/api/dist/

# 3. Rebuild Go binary (embeds new WebUI)
go build -o bin/xg2g ./cmd/daemon
```

### Backend Only Changes

```bash
# Just rebuild Go daemon (WebUI unchanged)
go build -o bin/xg2g ./cmd/daemon

# Or use Make
make build
```

### Restart Running Service

```bash
# If running via docker-compose
make restart

# If running locally (make dev)
# Press Ctrl+C and run `make dev` again
```

## 🐛 Troubleshooting

### "Port already in use"

```bash
# Find and kill process on port 8088
lsof -ti:8088 | xargs kill -9

# Or use Make helper
pkill -x xg2g
```

### "WebUI not loading"

The WebUI is embedded in the Go binary. You must:

1. Build WebUI: `make ui-build`
2. Rebuild daemon: `make build`
3. Restart: `make restart` or re-run `make dev`

### "Changes not appearing"

```bash
# Full clean rebuild
make clean
make build
```

### Docker build cache issues

```bash
# Clean Docker cache
make docker-clean

# Rebuild image
make docker-build
```

## 📁 Project Structure

```
xg2g/
├── cmd/daemon/          # Main entry point
├── internal/            # Go backend code
│   ├── api/            # HTTP API + embedded WebUI
│   ├── v3/             # V3 streaming architecture
│   └── ...
├── webui/              # React frontend (Vite)
│   ├── src/
│   └── dist/           # Build output (embedded in Go)
├── Dockerfile          # Multi-stage build
├── docker-compose.yml  # Deployment config
└── Makefile            # Build automation
```

## 🔨 Build Process Explained

### Full Build Chain

```
1. WebUI (React)   → webui/dist/*
2. Embed WebUI     → internal/api/dist/*
3. Go Binary       → bin/xg2g (includes embedded UI)
```

### Why WebUI Must Be Rebuilt

The WebUI is **embedded** into the Go binary at compile time:

```go
//go:embed dist/*
var distFS embed.FS
```

This means:

- Frontend changes require: `make ui-build` + `make build`
- Backend changes only require: `make build`

## 🎯 Simplified Commands for AI/Automation

### "Start fresh"

```bash
make clean && make build && make dev
```

### "Rebuild everything"

```bash
make clean-full && make build
```

### "Just restart"

```bash
make restart
```

### "View logs"

```bash
make logs
```

## 🚢 Production Deployment

See [V3 Setup Guide](guides/v3-setup.md) for production deployment.

## 📖 Additional Resources

- [Configuration Guide](guides/CONFIGURATION.md)
- [Troubleshooting](TROUBLESHOOTING.md)
- [Architecture](ARCHITECTURE.md)
- [Versioning Guidelines](VERSIONING.md)
