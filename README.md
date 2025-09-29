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

## Notes

- XMLTV currently only contains channel definitions by default; the fuzzy matcher exists for later EPG integration.
- Configuration is ENV-only (no config files). See `cmd/daemon/main.go` and `internal/jobs/refresh.go` for how ENV variables are read.
