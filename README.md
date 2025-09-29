# xg2g - OpenWebIF to M3U/XMLTV Proxy

![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg)
![Docker](https://github.com/ManuGH/xg2g/actions/workflows/docker.yml/badge.svg)
![License](https://img.shields.io/github/license/ManuGH/xg2g)

## Quick Start

Run locally (example):

```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_BOUQUET=Favourites \
XG2G_LISTEN=:8080 \
go run ./cmd/daemon
```

Or via Docker (docker-compose snippet):

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "34400:34400"
    environment:
      - XG2G_DATA=/data
      - XG2G_OWI_BASE=http://receiver.local
      - XG2G_BOUQUET=Favourites

```

xg2g is a small Go microservice that converts OpenWebIF bouquets (for example from VU+ receivers) into IPTV-friendly artifacts. It exposes a tiny HTTP API and writes generated files into the directory specified by `XG2G_DATA`.

> **Note**
> xg2g is a converter that produces the M3U/XMLTV basis for downstream middleware (xTeVe, Threadfin, Plex, Jellyfin, …). It does not replace those middleware components; instead it feeds them with preprocessed data from OpenWebIF.

Generated artifacts

- `playlist.m3u` — M3U playlist with `#EXTINF` attributes: `tvg-id`, `tvg-name`, `group-title`, `tvg-logo` and stable tvg ids.
- `xmltv.xml` — XMLTV channel definitions (programmes are currently not populated by default).
- `/files/*` — served by the HTTP API from the `XG2G_DATA` folder (e.g. `/files/playlist.m3u`).

Configuration (ENV)

Key environment variables:

| Variable           | Type     | Default  | Required | Description                                                                                 |
| ------------------ | -------- | -------- | -------- | ------------------------------------------------------------------------------------------- |
| `XG2G_DATA`        | path     | `./data` | yes      | Target folder for generated artifacts and `/files` serving                                   |
| `XG2G_OWI_BASE`    | url      | -        | yes      | Base URL of the OpenWebIF instance (receiver)                                               |
| `XG2G_BOUQUET`     | string   | -        | yes      | Bouquet name or ID to fetch (e.g. `Favourites`)                                             |
| `XG2G_XMLTV`       | string   | ``       | no       | If set, path to write `xmltv.xml` inside `XG2G_DATA` (or absolute path)                     |
| `XG2G_PICON_BASE`  | url/path | ``       | no       | Base URL or path for picon images; if empty, OpenWebIF picon derivation is used             |
| `XG2G_FUZZY_MAX`   | int      | `2`      | no       | Max Levenshtein distance for fuzzy matching EPG names                                       |
| `XG2G_STREAM_PORT` | int      | `8001`   | no       | Override for the OpenWebIF stream port (defaults to 8001)                                   |

API endpoints

- `GET /api/status` — Returns simple status JSON.
- `GET, POST /api/refresh` — Trigger a refresh (fetch bouquets/services → write playlist ± xmltv). The operation is idempotent; repeated calls have the same effect.
- `GET /files/*` — Static serving of generated artifacts from `XG2G_DATA`.

Example refresh calls:

```bash
curl -X POST http://127.0.0.1:34400/api/refresh
curl      http://127.0.0.1:34400/api/refresh
```

Testing & development

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

Notes

- XMLTV currently only contains channel definitions by default; the fuzzy matcher exists for later EPG integration.
- Configuration is ENV-only (no config files). See `cmd/daemon/main.go` and `internal/jobs/refresh.go` for how ENV variables are read.
