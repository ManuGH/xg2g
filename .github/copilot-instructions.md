> **Note:** This file helps automated coding agents (and new contributors) become productive quickly in this repository.

# xg2g — Copilot instructions (concise)

Purpose: xg2g translates Enigma2/OpenWebIF bouquets into IPTV artifacts (M3U + XMLTV) and exposes a tiny HTTP API to trigger refreshes and serve generated files.

Read these files first
- `cmd/daemon/main.go` — application entry, ENV → jobs.Config, starts HTTP server.
- `internal/jobs/refresh.go` — central work: fetch bouquets → services → build playlist items → write M3U and optional XMLTV.
- `internal/openwebif/client.go` — OpenWebIF API calls, stream & picon URL helpers, HTTP timeouts and port override (`XG2G_STREAM_PORT`).
- `internal/api/http.go` — API endpoints (`/api/status`, `/api/refresh`) and `/files/*` static serving.
- `internal/playlist/m3u.go` and `internal/epg/*.go` — file format writers.

Quick actionable conventions (project-specific)
- Configuration is ENV-only (prefix `XG2G_`). Key variables: `XG2G_DATA`, `XG2G_OWI_BASE`, `XG2G_BOUQUET`, `XG2G_XMLTV`, `XG2G_PICON_BASE`, `XG2G_FUZZY_MAX`, `XG2G_STREAM_PORT`.
- The single exported operation is `jobs.Refresh(ctx, jobs.Config)` — most changes touch this flow. Prefer extracting small helpers (fetch/builder/writer) to make unit tests easier.
- Deterministic tvg-id creation lives in `makeStableID` inside `internal/jobs/refresh.go`; keep its behaviour when changing channel naming to avoid breaking existing EPG mappings.
- OpenWebIF: code prefers `/api/getallservices?bRef=` and falls back to `/api/getservices?sRef=`; the client filters bouquet-containers (`1:7:` prefix).

Testing & local validation
- Unit tests: run `go test ./... -v` or target packages (e.g. `go test ./internal/epg -v`).
- Quick manual run:
```bash
XG2G_DATA=./data \
XG2G_OWI_BASE=http://receiver.local \
XG2G_BOUQUET=Favourites \
XG2G_LISTEN=:8080 \
go run ./cmd/daemon
```
Trigger a refresh and inspect outputs:
```bash
curl -X POST http://localhost:8080/api/refresh
ls ./data/playlist.m3u ./data/xmltv.xml
```

Build & CI
- Build: `go build ./cmd/daemon`.
- Tests + race: `go test ./... -v -race`.
- The repo contains CI workflows (see `.github/workflows`) and a Makefile with `make build`, `make test`, `make lint` targets.

Known limitations & integration notes (do not assume otherwise)
- XMLTV currently only contains channel definitions (no programme entries). The fuzzy matcher (`internal/epg/fuzzy.go`) exists but is not yet wired to an external EPG ingestion step.
- `jobs.Refresh` directly creates the OpenWebIF client and writes files. For testability, introduce interfaces (e.g. `openwebif.Client` and `Writer` abstractions) when adding unit/integration tests.
- Logging: startup logging in `cmd/daemon/main.go` uses `log.Printf` while other packages use structured `zerolog`. When touching logging, prefer consistent zerolog usage.

Files to inspect for common tasks
- Add API route: `internal/api/http.go`.
- Change refresh behaviour or add outputs: `internal/jobs/refresh.go`.
- Adjust OpenWebIF calls, timeouts or stream URLs: `internal/openwebif/client.go`.
- Modify playlist format: `internal/playlist/m3u.go`.
- Improve XMLTV or fuzzy matching: `internal/epg/*.go`.

If you need a test scaffold, a small patch (interface + mock) for `openwebif.Client` + a unit test for `jobs.Refresh` is a high-value change.

If anything here is unclear or you want a short example (test skeleton, example API call, or a PR template), tell me which area to expand.
> **Note:** This document serves as a best-practice guide for AI agents and developers.

# Repository Custom Instructions for AI Agents

Goal: enable quick productivity. Focus on build/test workflows, architecture, conventions, and integration points of this repo.

## Architecture – Big Picture
- Binary: `cmd/daemon/main.go` starts HTTP API and periodic jobs.
- Core flow: `internal/jobs/refresh.go` calls OpenWebIF, extracts services/bouquets → writes `playlist.m3u` and optionally XMLTV via `internal/epg/*`.
- HTTP layer: `internal/api/http.go` provides:
  - `GET /api/status` – simple health/status.
  - `POST /api/refresh` – triggers end-to-end refresh.
  - `GET /files/*` – serves generated artifacts from `XG2G_DATA` (e.g. `playlist.m3u`, `xmltv.xml`, picons).
- Integrations:
  - OpenWebIF client: `internal/openwebif/*` (`getallservices` fallback to `getservices`, picons, SRef handling).
  - Playlist: `internal/playlist/m3u.go` (write M3U, attributes, grouping).
  - EPG/XMLTV: `internal/epg/*` (generator, fuzzy matching, name↔ID map).

## Build / Run / Test
- Go: `go build ./cmd/daemon`  |  Tests: `go test ./... -v -race`
- Make (if available): `make build`, `make test`, `make lint`
- Docker: `Dockerfile` for binary image. `docker-compose.yml` runs service + bind-mount for `XG2G_DATA` and published ports.

- Local run example:
  ```bash
  XG2G_DATA=./data \
  XG2G_OWI_BASE=http://receiver.local \
  XG2G_BOUQUET=Favourites \
  XG2G_LISTEN=:8080 \
  go run ./cmd/daemon
  ```

  Manual end-to-end check:
  ```bash
  curl -X POST http://localhost:8080/api/refresh
  ls ./data/playlist.m3u ./data/xmltv.xml
  ```

### Important Environment Variables
- `XG2G_DATA` target directory for artifacts and /files serving.
- `XG2G_LISTEN` HTTP listen address, e.g. `:8080`.
- `XG2G_OWI_BASE` base URL of OpenWebIF (no trailing slash).
- `XG2G_BOUQUET` bouquet name/ID for filtering.
- `XG2G_XMLTV` true|false to generate XMLTV.
- `XG2G_PICON_BASE` external/relative base for picon URLs.
- `XG2G_FUZZY_MAX` max Levenshtein distance for EPG matching.
- `XG2G_STREAM_PORT` port override for stream URLs (if receiver port ≠ 8001).

| Variable         | Type     | Default | Required | Description                                      |
|------------------|----------|---------|----------|------------------------------------------------|
| XG2G_DATA        | Path     | ./data  | yes      | Target folder for artifacts and /files serving |
| XG2G_LISTEN      | Address  | :8080   | no       | HTTP listen address                             |
| XG2G_OWI_BASE    | URL      | —       | yes      | Base URL of OpenWebIF                           |
| XG2G_BOUQUET     | String   | —       | yes      | Bouquet name/ID for filtering                   |
| XG2G_XMLTV       | bool     | false   | no       | Enable XMLTV generation                         |
| XG2G_PICON_BASE  | URL/Path | —       | no       | Base for picon URLs                             |
| XG2G_FUZZY_MAX   | int      | 2       | no       | Max Levenshtein distance for EPG matching      |
| XG2G_STREAM_PORT | int      | 8001    | no       | Port override for stream URLs                   |

Example `.env` for local development:
```env
XG2G_DATA=./data
XG2G_OWI_BASE=http://receiver.local
XG2G_BOUQUET=Favourites
XG2G_LISTEN=:8080
XG2G_XMLTV=true
```

## API Endpoints

| Method | Path          | Purpose                    | Notes                      |
|--------|---------------|----------------------------|----------------------------|
| GET    | /api/status   | Health/status              | no parameters              |
| POST   | /api/refresh  | Trigger refresh (jobs)     | idempotent; 200 on success |
| GET    | /files/*      | Static artifacts           | served from `XG2G_DATA`    |

Example response for `GET /api/status`:
```json
{
  "status": "ok",
  "timestamp": "2025-09-28T09:00:00Z"
}
```

## Project Conventions / Patterns
- Errors: wrap with context (`fmt.Errorf("step: %w", err)`), early returns.
- Stability: service IDs via helper (e.g. `makeStableID` in job) for deterministic #EXTINF IDs.
- OpenWebIF:
  - First `/api/getallservices`, fallback `/api/getservices?sRef=<bouquetRef>`.
  - Stream URL example: `openwebif.StreamURL(base, sRef)` → `http://host:8001/<sRef>`
  - Picon URL: `openwebif.PiconURL(base, sRef)` or filename derived from SRef.
- Playlist:
  - Write M3U with `playlist.WriteM3U(w, []playlist.Item)`; group from bouquet, normalized channel name.
  - `#EXTINF:-1 tvg-id="stableID" tvg-name="Name" group-title="Bouquet", DisplayName`
- EPG/XMLTV:
  - Generate with `epg.WriteXMLTV(channels, path)`; maintain name↔ID map from channel list.
  - Fuzzy matching in `internal/epg/fuzzy.go` (Levenshtein) uses `XG2G_FUZZY_MAX`.

## Test / Debug Hints
- Use temp folder for `XG2G_DATA` in tests, clean artifacts between runs.
- Targeted unit tests for fuzzy cases in `internal/epg/fuzzy.go` with realistic channel names and umlauts.
- Handle receiver network errors: timeouts and 50x wrapped cleanly; limited retries in job.

## Extension Points
- Additional sources: new EPG adapters under `internal/epg/…` with same item signature.
- Additional output channels: implement new writer like `playlist/m3u.go` and register in job.
- API routes: add in `internal/api/http.go`; keep CORS/headers consistent.

## Example Snippets

```go
// OpenWebIF → Services
svc, err := openwebif.Services(ctx, cfg.BaseURL, cfg.Bouquet)
```

```go
// Write playlist
var items []playlist.Item // mapped from services
if err := playlist.WriteM3U(f, items); err != nil {
    // handle error
}
```

```go
// Optional XMLTV
if cfg.XMLTV {
    err = epg.Generate(cfg.DataDir, items)
}
```
