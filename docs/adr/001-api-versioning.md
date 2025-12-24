# ADR-001: API Versioning

**Status:** Accepted  
**Date:** 2025-12-19

## Context

`xg2g` serves multiple kinds of clients:

- “real” API consumers (automation, scripts)
- the built-in WebUI
- media servers (Plex/Jellyfin/Emby) that expect stable behaviors

We need an explicit compatibility boundary so changes to handlers don’t silently break clients.

## Decision

- The HTTP API is versioned via a URL prefix: `/api/<version>/*`.
- The OpenAPI-generated server is mounted under this base path, and the WebUI uses the same base.
- Non-versioned "compat" routes (e.g. `/stream/*`) exist only for WebUI playback integration and are considered internal wiring, not public API surface.

## Consequences

- Breaking API changes require a new version prefix and migration docs.
- Within a major API version, backwards-compatible evolution is allowed (additive fields/endpoints), but removing/renaming routes is treated as breaking.
- Legacy version details are captured in the History section.

## History

- Stable API base path at the time of this decision: `/api/v2/*`.
- Streaming control plane introduced under `/api/v3/*`.

## References (Code)

- API router / WebUI: `internal/api/http.go`
- Generated API server wiring: `internal/api/server_gen.go`
