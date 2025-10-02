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

With HTTP Basic Auth and multiple bouquets:

```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_OWI_USER=root \
XG2G_OWI_PASS=secret \
XG2G_BOUQUET="Premium,Favourites,Sports" \
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
- `xmltv.xml` — XMLTV file with channel definitions and optionally full EPG programme data (enable via `XG2G_EPG_ENABLED=true`).
- `/files/*` — served by the HTTP API from the `XG2G_DATA` folder (e.g. `/files/playlist.m3u`).

## Configuration (ENV)

Key environment variables:

| Variable              | Type     | Default   | Required | Description                                                                                 |
| --------------------- | -------- | --------- | -------- | ------------------------------------------------------------------------------------------- |
| `XG2G_DATA`           | path     | `./data`  | yes      | Target folder for generated artifacts and `/files` serving                                  |
| `XG2G_OWI_BASE`       | url      | `-`       | yes      | Base URL of the OpenWebIF instance (receiver)                                               |
| `XG2G_OWI_USER`       | string   | `(empty)` | no       | HTTP Basic Auth username for OpenWebIF (optional)                                           |
| `XG2G_OWI_PASS`       | string   | `(empty)` | no       | HTTP Basic Auth password for OpenWebIF (optional)                                           |
| `XG2G_BOUQUET`        | string   | `-`       | yes      | Bouquet name(s) to fetch (e.g. `Favourites` or `Premium,Sports` for multiple)              |
| `XG2G_XMLTV`          | string   | `(empty)` | no       | Optional output path for XMLTV. If set, writes to this path. Relative paths resolved against `XG2G_DATA`. |
| `XG2G_PICON_BASE`     | url/path | `(empty)` | no       | Base URL or path for picon images; defaults to OpenWebIF derivation                         |
| `XG2G_FUZZY_MAX`      | int      | `2`       | no       | Max Levenshtein distance for fuzzy matching EPG names                                       |
| `XG2G_STREAM_PORT`    | int      | `8001`    | no       | Override for the OpenWebIF stream port (defaults to 8001)                                   |
| `XG2G_METRICS_LISTEN` | address  | `(empty)` | no       | Prometheus metrics server listen address (e.g. `:9090`). Empty disables metrics. IPv6 needs brackets. |
| `XG2G_PLAYLIST_FILENAME` | string | `playlist.m3u` | no | Filename for the generated playlist (used by readiness check)                               |

EPG (Electronic Program Guide) configuration:

| Variable                  | Type    | Default | Required | Description                                                                                 |
| ------------------------- | ------- | ------- | -------- | ------------------------------------------------------------------------------------------- |
| `XG2G_EPG_ENABLED`        | bool    | `false` | no       | Enable EPG programme data collection (adds `<programme>` entries to XMLTV)                  |
| `XG2G_EPG_DAYS`           | int     | `3`     | no       | Number of days to fetch EPG data (1-14 days)                                                |
| `XG2G_EPG_MAX_CONCURRENCY`| int     | `3`     | no       | Maximum parallel EPG requests (1-10, tune based on receiver capacity)                       |
| `XG2G_EPG_TIMEOUT_MS`     | int     | `15000` | no       | Timeout per EPG request in milliseconds (5000-60000)                                        |
| `XG2G_EPG_RETRIES`        | int     | `2`     | no       | Retry attempts for failed EPG requests (0-5)                                                |

Server timeouts and limits (HTTP listener):

- `XG2G_SERVER_READ_TIMEOUT` (default: `15s`)
- `XG2G_SERVER_WRITE_TIMEOUT` (default: `15s`)
- `XG2G_SERVER_IDLE_TIMEOUT` (default: `60s`)
- `XG2G_SERVER_MAX_HEADER_BYTES` (default: `1048576`) — in bytes
- `XG2G_SERVER_SHUTDOWN_TIMEOUT` (default: `10s`) — graceful shutdown timeout

Rate limiting (per-process, token bucket):

- `XG2G_RATELIMIT_ENABLED` (default: `false`)
- `XG2G_RATELIMIT_RPS` (default: `5`) — steady-state requests per second
- `XG2G_RATELIMIT_BURST` (default: `10`) — burst capacity
- `XG2G_RATELIMIT_WHITELIST` (default: empty) — comma-separated IPs/CIDRs bypassing limits

OpenWebIF HTTP client pooling (transport tuning):

- `XG2G_HTTP_MAX_IDLE_CONNS` (default: `100`)
- `XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST` (default: `10`)
- `XG2G_HTTP_MAX_CONNS_PER_HOST` (default: `0` = unlimited)
- `XG2G_HTTP_IDLE_TIMEOUT` (default: `90s`)
- `XG2G_HTTP_ENABLE_HTTP2` (default: `true`)

## API Endpoints

- `GET /api/status` — Returns simple status JSON.
- `POST /api/refresh` — Trigger a refresh (fetch bouquets/services → write playlist ± xmltv). The operation is idempotent; repeated calls have the same effect.
- `GET /healthz` — Liveness/health endpoint.
- `GET /readyz` — Readiness endpoint (becomes 200 once the first successful refresh has occurred).
  - Details: readiness flips to 200 after a successful refresh and when the playlist file exists in `XG2G_DATA` under `XG2G_PLAYLIST_FILENAME` (default `playlist.m3u`).
- `GET /metrics` — Prometheus metrics endpoint (only when `XG2G_METRICS_LISTEN` is configured).
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

## Full EPG smoke test

To verify end-to-end Service-EPG (per-channel, multi-day) and generate a sizable XMLTV with many `<programme>` entries, use the helper script:

```bash
# Optional: set your environment
export XG2G_DATA=./data
export XG2G_OWI_BASE=http://receiver.local
export XG2G_BOUQUET=Premium
export XG2G_XMLTV=xmltv.xml

export XG2G_EPG_ENABLED=true
export XG2G_EPG_DAYS=7
export XG2G_EPG_MAX_CONCURRENCY=6
export XG2G_EPG_TIMEOUT_MS=20000
export XG2G_METRICS_LISTEN=:9090

# Thresholds (fail build if smaller)
export EPG_MIN_BYTES=$((5 * 1024 * 1024))
export EPG_MIN_PROGRAMMES=5000

./scripts/epg-full-refresh.sh
```

The script will:
- Start the daemon, trigger a refresh, and wait for the XMLTV to appear
- Print the XMLTV size and the number of `<programme>` entries (+ a small sample)
- Optionally show filtered Prometheus metrics (if `XG2G_METRICS_LISTEN` is set)
- Exit non-zero (2) when thresholds are not met, which is useful for CI smoke checks

Tip: if the file is still small, increase `XG2G_EPG_DAYS` (e.g. 10–14) and temporarily raise `XG2G_EPG_MAX_CONCURRENCY` (8–10), then tune back down to protect the receiver.

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

Prometheus scrapes the in-cluster service via annotations on port `9090`. For Kubernetes clusters with a Prometheus Operator, you can switch to a `ServiceMonitor` if preferred. Core alerts and runbooks:

- HTTP 5xx rate / API failures – [docs/runbooks/http-5xx.md](docs/runbooks/http-5xx.md)
- Refresh latency (p95/p99) – [docs/runbooks/latency.md](docs/runbooks/latency.md)
- Refresh endpoint errors / stalled refresh – [docs/runbooks/refresh-failures.md](docs/runbooks/refresh-failures.md)
- Metrics scrape or pod outage – [docs/runbooks/metrics-scrape.md](docs/runbooks/metrics-scrape.md)
- Low `/data` free space – [docs/runbooks/data-capacity.md](docs/runbooks/data-capacity.md)

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

- XMLTV generation supports both channel definitions and full EPG programme data (see `XG2G_EPG_ENABLED` and related ENV variables).
- Configuration is ENV-only (no config files). See `cmd/daemon/main.go` and `internal/jobs/refresh.go` for how ENV variables are read.

Please refer to [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

### Pull Request checks (required)

All PRs must pass the full CI gate before merge:

- Build
- Tests (incl. -race)
- Lint (golangci-lint)
- Security: CodeQL + govulncheck (govulncheck fails on High/Critical; Low/Medium reported as warnings)

Keep your branch up to date with `main` to satisfy required checks and avoid stale base failures.

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
