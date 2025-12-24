# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [3.0.0] - 2025-12-24

### Breaking Changes

- **Auth**: Removed query parameter authentication (`?token=...`). Use `Authorization: Bearer <token>` or `xg2g_session`.
- **Config**: Removed legacy environment variable aliases (e.g., `RECEIVER_IP`, `XG2G_API_ADDR`). Canonical env vars only.
- **Config**: Default config schema version is now `3.0.0` with strict validation enabled by default.

### Added

- **RBAC**: Scoped tokens with centralized v3 route registration and deny-by-default enforcement for write/admin endpoints.
- **Config**: `configVersion` and strict-mode validation; `xg2g config migrate` scaffolding for upgrades.
- **Docs**: RBAC guide and endpoint→scope mapping.

### Changed

- **Routing**: v3 routes always registered and return semantic status codes for validation errors.
- **Startup**: v3 store/HLS path validation fails fast when invalid or unwritable.
- **Refactor**: Unified Circuit Breaker implementation into `internal/resilience` (replaces scattered implementations in `api` and `openwebif`).
- **Refactor**: Removed unused error helper functions in `internal/api/errors.go`.
- **Config**: Fixed precedence logic for OpenWebIF credentials to correctly prefer specific env vars (`XG2G_OWI_USER`) over generic ones.
- **Orchestration**: Replaced deprecated `RFFmpegExited` with `RProcessEnded`.

## [2.1.0] - 2025-12-30

### Changed

- **Version alignment**: Bumped binary/OpenAPI/config schema/example to v2.1.0.
- **Docs**: Marked v3 as experimental/preview and normalized references to `/api/v3`.
- **Config**: Legacy environment variable aliases are deprecated, emit warnings, and are scheduled for removal in v2.2.

## [2.0.1] - 2025-12-23

### Security

- **[BREAKING] Query parameter authentication disabled by default**
  - Authentication via `?token=...` in URLs is now disabled by default to prevent token leakage in proxy logs, browser history, and referrer headers.
  - **Migration**: Use `Authorization: Bearer <token>` header or `xg2g_session` cookie instead.
  - **Temporary workaround**: Set `XG2G_ALLOW_QUERY_TOKENS=true` to re-enable (will be removed in v3.0.0).
  - Requests using query tokens will log a deprecation warning.

### Fixed

- **V3 Store**: Fixed lock contention in `MemoryStore.ScanSessions` that could block reads during slow callbacks.
- **API**: JSON encoding errors are now logged instead of silently ignored.
- **V3 API**: Idempotency check failures now return HTTP 503 instead of continuing with undefined behavior.

### Added

- **Config**: `XG2G_ALLOW_QUERY_TOKENS` environment variable to control query parameter authentication (default: false).
- **Tests**: Added concurrency tests for V3 MemoryStore with race detector coverage.

## [2.0.0] - 2025-12-20

### License

- **Changed license from MIT/AGPL to PolyForm Noncommercial License 1.0.0**.
  - **Breaking Change**: Commercial use is now restricted.
  - This change ensures the project remains free for personal and non-profit use while requiring commercial entities to obtain a separate license.
  - See `docs/licensing.md` for migration details.

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
