# xg2g - OpenWebIF to M3U/XMLTV Proxy

![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)
![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg)
![License](https://img.shields.io/github/license/ManuGH/xg2g)

## Quick Start

Run locally:

```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_BOUQUET=Favourites \
XG2G_LISTEN=:8080 \
go run ./cmd/daemon
```

## Usage Notes

xg2g converts OpenWebIF bouquets into M3U and XMLTV artifacts and exposes a small HTTP API. It acts as a preprocessing fetcher/generator and does not replace middleware such as xTeVe or Threadfin. Those tools still handle channel mapping, EPG merging, proxy streaming, and transcoding.

## Docker Deployment

Recommended structure on Linux hosts (example under `/opt`):

- `/opt/xg2g/config` - store `docker-compose.yml`, `.env`, and other configuration files.
- `/opt/xg2g/data` - bind mount for generated artifacts (`playlist.m3u`, `xmltv.xml`, picons ...).

Example setup:

```bash
sudo mkdir -p /opt/xg2g/{config,data}
cp docker-compose.yml /opt/xg2g/config/
# edit docker-compose.yml: port mapping, XG2G_OWI_BASE, XG2G_BOUQUET, XG2G_LISTEN, etc.
cd /opt/xg2g/config
docker compose config    # validate file
docker compose up -d
```

Check status:

```bash
curl http://<host>:8080/api/status
```

Optional: Dockge or other orchestration front ends can point to the same compose stack — keep `/opt/xg2g/data` mounted so the generated files persist.

### Example docker-compose.yml

```yaml
# Using the Compose Specification; no version field required
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g
    ports:
      - "8080:8080"          # host:container (adjust if needed)
    environment:
      - XG2G_DATA=/data
      - XG2G_OWI_BASE=http://receiver.local
      - XG2G_BOUQUET=Favourites
      - XG2G_LISTEN=:8080    # optional, defaults to :8080
    volumes:
      - /opt/xg2g/data:/data
    restart: unless-stopped
```

## Reminder

xg2g produces the M3U/XMLTV basis for downstream middleware. It does not perform channel mapping, EPG merging, proxy streaming, or transcoding by itself.

## Generated Artifacts

- `playlist.m3u` — M3U playlist with `#EXTINF` attributes (tvg-id, tvg-name, group-title, tvg-logo) and stable tvg-id values.
- `xmltv.xml` — XMLTV channel definitions (programs currently not populated by default).
- `/files/*` — served by the HTTP API from the `XG2G_DATA` folder (e.g. `/files/playlist.m3u`).

## Configuration (ENV)

Key environment variables:

| Variable           | Type     | Default  | Required | Description                                                                                 |
| ------------------ | -------- | -------- | -------- | ------------------------------------------------------------------------------------------- |
| `XG2G_DATA`        | path     | `./data` | yes      | Target folder for generated artifacts and `/files` serving                                   |
| `XG2G_OWI_BASE`    | url      | `-`      | yes      | Base URL of the OpenWebIF instance (receiver)                                               |
| `XG2G_BOUQUET`     | string   | `-`      | yes      | Bouquet name or ID to fetch (e.g. `Favourites`)                                             |
| `XG2G_XMLTV`       | string   | `(empty)` | no       | If set, path to write `xmltv.xml` inside `XG2G_DATA` (or absolute path)                     |
| `XG2G_PICON_BASE`  | url/path | `(empty)` | no       | Base URL or path for picon images; defaults to OpenWebIF derivation                         |
| `XG2G_FUZZY_MAX`   | int      | `2`      | no       | Max Levenshtein distance for fuzzy matching EPG names                                       |
| `XG2G_STREAM_PORT` | int      | `8001`   | no       | Override for the OpenWebIF stream port (defaults to 8001)                                   |
| `XG2G_METRICS_LISTEN` | address | `:9090` | no      | Prometheus metrics server listen address (empty = disabled, IPv6 needs brackets)            |

## API Endpoints

- `GET /api/status` — Returns simple status JSON.
- `GET, POST /api/refresh` — Trigger a refresh (fetch bouquets/services → write playlist ± xmltv). The operation is idempotent; repeated calls have the same effect.
- `GET /files/*` — Static serving of generated artifacts from `XG2G_DATA`.

Example refresh calls:

```bash
curl -X POST http://127.0.0.1:8080/api/refresh
curl      http://127.0.0.1:8080/api/refresh
```

## Testing & Development

- Build: `go build ./cmd/daemon`
- Tests: `go test ./... -v`
- Quick manual run:

```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_BOUQUET=Favourites \
XG2G_LISTEN=:8080 \
go run ./cmd/daemon
```

## Stability & Tuning

xg2g includes comprehensive retry and timeout mechanisms for reliable operation with potentially unreliable OpenWebIF receivers.

### Default Configuration

| Parameter | Default | Range | Description |
|-----------|---------|--------|-------------|
| `XG2G_OWI_TIMEOUT_MS` | 10000 | 1000-60000 | Request timeout per attempt (ms) |
| `XG2G_OWI_RETRIES` | 3 | 0-10 | Maximum retry attempts |
| `XG2G_OWI_BACKOFF_MS` | 500 | 100-5000 | Base backoff delay (ms) |
| `XG2G_OWI_MAX_BACKOFF_MS` | 2000 | 500-30000 | Maximum backoff delay (ms) |

### When to Adjust Settings

**Slow or Overloaded Receiver:**

- Increase timeout: `XG2G_OWI_TIMEOUT_MS=20000`
- Increase backoff: `XG2G_OWI_BACKOFF_MS=1000`

**Fast Local Network:**

- Decrease timeout: `XG2G_OWI_TIMEOUT_MS=5000`
- Decrease backoff: `XG2G_OWI_BACKOFF_MS=200`

**Unreliable Network:**

- Increase retries: `XG2G_OWI_RETRIES=5`
- Increase max backoff: `XG2G_OWI_MAX_BACKOFF_MS=5000`

### Monitoring & Security

The application exposes comprehensive Prometheus metrics for observability and security:

**Performance Metrics:**

- `xg2g_openwebif_request_duration_seconds` - Request latencies (p50/p95/p99)
- `xg2g_openwebif_request_retries_total` - Retry attempts by operation  
- `xg2g_openwebif_request_failures_total` - Failures by error class
- `xg2g_openwebif_request_success_total` - Successful requests

**Security Metrics (v1.2.0+):**

- `xg2g_file_requests_denied_total{reason}` - Blocked requests by attack type
- `xg2g_file_requests_allowed_total` - Successful file serves
- `xg2g_http_requests_total{status,endpoint}` - HTTP responses by status code

**Production Monitoring:**

```bash
# Deploy full monitoring stack (Prometheus + Grafana + AlertManager)
docker-compose -f docker-compose.monitoring.yml up -d

# Access monitoring dashboards
# Grafana: http://localhost:3000 (admin/admin)  
# Prometheus: http://localhost:9091
# AlertManager: http://localhost:9093
```

**Security Testing:**

```bash
# Comprehensive penetration testing
./scripts/security-test.sh

# Quick CI/CD security check
./scripts/quick-security-check.sh
```

Use structured logs with consistent fields (`attempt`, `duration_ms`, `error_class`) for debugging.

## Contributing

> **English-only Policy**: All communication in this repository (issues, pull requests, documentation, code comments) must be in English to ensure accessibility for the global community.

- XMLTV currently only contains channel definitions by default; the fuzzy matcher exists for later EPG integration.
- Configuration is ENV-only (no config files). See `cmd/daemon/main.go` and `internal/jobs/refresh.go` for how ENV variables are read.

Please refer to [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Docker images

The default Dockerfile uses Alpine (multi-stage) and includes an HTTP healthcheck via wget.

For a lean runtime image, a distroless variant is available as `Dockerfile.distroless` (no shell/tools, no built-in HEALTHCHECK; rely on orchestrator probes):

```bash
# Build default (Alpine)
docker build -t xg2g:latest -f Dockerfile .

# Build distroless
docker build -t xg2g:distroless -f Dockerfile.distroless .
```

Hardened deployment templates are available in `deploy/`:

- `deploy/docker-compose.alpine.yml` — Alpine with built-in healthcheck, non-root, read-only FS, caps dropped.
- `deploy/docker-compose.distroless.yml` — Distroless, non-root, read-only FS; probes via orchestrator.
- `deploy/k8s-alpine.yaml` and `deploy/k8s-distroless.yaml` — Kubernetes manifests with digest pins, securityContext and probes (`/healthz`, `/readyz`).

