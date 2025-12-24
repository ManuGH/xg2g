# V3 HLS Setup Guide

This guide details how to set up and deploy `xg2g` with the V3 (HLS) streaming backend enabled.

## Overview

The V3 backend introduces a native HLS streaming architecture that replaces the legacy stream proxy. It features:

- **Native HLS Generation**: Direct generation of m3u8 playlists and segments.
- **Resilience**: Improved handling of stream interruptions and receiver restarts.
- **Worker-based Architecture**: A background worker manages stream sessions independently of HTTP requests.

## Deployment

### Docker (Recommended)

To run `xg2g` with V3 enabled using Docker:

1. **Build the image** (or pull from registry):

    ```bash
    docker build -t xg2g:latest .
    ```

2. **Run the container**:
    CRITICAL: You must set `XG2G_V3_WORKER_ENABLED=true`.

    ```bash
    docker run -d \
      --name xg2g \
      --net=host \
      -e XG2G_API_TOKEN="your-secret-token" \
      -e XG2G_OWI_BASE="http://your-receiver-ip" \
      -e XG2G_V3_WORKER_ENABLED=true \
      -v $(pwd)/data:/data \
      xg2g:latest
    ```

### Docker Compose

Add the following environment variable to your `docker-compose.yml`:

```yaml
version: "3"
services:
  xg2g:
    build: .
    network_mode: host
    environment:
      - XG2G_API_TOKEN=your-secret-token
      - XG2G_OWI_BASE=http://your-receiver-ip
      - XG2G_V3_WORKER_ENABLED=true # Enable V3
    volumes:
      - ./data:/data
```

### Bare Metal

1. **Build**:

    ```bash
    go build -o xg2g-daemon ./cmd/daemon
    ```

2. **Run**:

    ```bash
    export XG2G_V3_WORKER_ENABLED=true
    export XG2G_API_TOKEN="your-token"
    export XG2G_OWI_BASE="http://your-receiver-ip"
    ./xg2g-daemon
    ```

## Configuration

The following environment variables control V3 behavior:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `XG2G_V3_WORKER_ENABLED` | `false` | Master switch to enable V3 backend |
| `XG2G_V3_STORE_BACKEND` | `memory` | Session state store (`memory` or `bolt`) |
| `XG2G_V3_STORE_PATH` | `/var/lib/xg2g/v3-store` | Path to BoltDB file (if backend is `bolt`) |

## Verification

To verify V3 is active:

1. **Check Logs**: Look for the startup message:

    ```json
    {"message":"starting v3 worker (Phase 7A)","component":"daemon"}
    ```

2. **Health Check**:

    ```bash
    curl http://localhost:8080/healthz
    ```

    Should return `200 OK`.

3. **Intents**: V3 uses "intents" to start streams. You can manually inspect active sessions at:

    ```bash
    curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v3/sessions
    ```
