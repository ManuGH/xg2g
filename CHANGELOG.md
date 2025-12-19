# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Breaking Changes

- **API v2**: Complete overhaul of the HTTP API.
  - New base path `/api/v2`.
  - Authentication now requires `Authorization: Bearer <token>` header (previously `X-API-Token`).
  - Standardized error responses compliant with RFC 7807 (Problem Details).
  - Removed legacy v1 endpoints and WebUI-specific handlers.

### Removed

- **Legacy V1 API** (`/api/v1/*`):
  - `/api/v1/status`, `/api/v1/refresh`, `/api/v1/config/reload`
- **WebUI Helper Endpoints** (`/api/ui/*`):
  - `/api/ui/status`, `/api/ui/urls`, `/api/ui/refresh`
- **M3U/XMLTV Management** (`/api/m3u/*`, `/api/xmltv/*`):
  - `/api/m3u/regenerate`, `/api/m3u/download`
  - `/api/xmltv/download`, `/api/files/status`
- **Channel Management** (`/api/channels/*`):
  - Replaced by `/api/v2/services/*` in V2.

### Added

- OpenAPI 3.0 specification (`api/openapi.yaml`).
- Generated server stubs using `oapi-codegen`.
- New `oapi-codegen` development dependency.
- Streamlined project structure by removing legacy code.

- **Config hot reload**:
  - `SIGHUP` triggers a config reload from disk (non-fatal).
  - `POST /api/v2/system/config/reload` triggers a config reload via API.
- **CLI helpers**:
  - `xg2g config validate` validates a YAML config file.
  - `xg2g config dump --effective` prints the merged config (defaults + file + env, secrets redacted).

### Fixed

- **Upgrade path for older configs**:
  - `config.yaml` accepts common legacy key spellings (e.g. `openwebif`, `bouquet`, `api.addr`) and logs warnings.
  - Refresh supports bouquet selection by **name** or legacy **bouquet ref** string.
- **Config snapshots**: Runtime-ENV is now read only during load/reload (deterministic snapshots, no ENV drift in hot paths). `Snapshot.Epoch` helps debug reload behavior.

### Notes

- The WebUI bundle is embedded in the Go binary via `internal/api/dist/*`, which increases binary size (intentional for single-binary deployments).
