# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Breaking Changes

- **API v2**: Complete overhaul of the HTTP API.
  - New base path `/api` (routes to v2 by default).
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
  - Replaced by `/api/services/*` in V2.

### Added

- OpenAPI 3.0 specification (`api/openapi.yaml`).
- Generated server stubs using `oapi-codegen`.
- New `oapi-codegen` development dependency.
- Streamlined project structure by removing legacy code.
