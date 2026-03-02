# v3 API Change Log

## [Unreleased]

### Changed

- Recordings: `PREPARING` for impossible probe stabilized (`blocked/probe_disabled` instead of false upstream escalation).
- Recordings: probe orchestration is anchored in the resolver; truth classification remains side-effect free.
- PlaybackInfo Contract: `Retry-After` now distinguishes `preparing` (`5s`) from `blocked` (`30s`).
- Error paths: terminal errors retain `MediaTruth` context for handlers/observability.
- Regression protection: added tests for Option-A semantics, orchestration boundary, and terminal truth payload propagation.

## [v3.1.0] - 2026-01-22

### ⚠️ BREAKING CHANGES

- **Standardized Casing**: The v3 API is now **camelCase-only**.
  - Renamed `begin_unix_seconds` -> `beginUnixSeconds`
  - Renamed `dvr_window_seconds` -> `dvrWindowSeconds`
  - Renamed `live_edge_unix` -> `liveEdgeUnix`
- **ProblemDetails Requirement**: `requestId` is now a **required** field in all `ProblemDetails` responses to ensure traceability.

### Added

- **Governance Gates**:
  - Mechanical check for underscore-free JSON tags in Go DTOs.
  - $ref-aware OpenAPI hygiene check focusing on v3 scope.
  - Strict duplicate key detection for OpenAPI spec files.
- **Runtime Validation**:
  - JSON golden tests for response type stability (e.g., `posSeconds` as integer).
  - Runtime schema compliance asserting `additionalProperties: false`.

### 🔄 Rollback & Compatibility Note

- **No Backward Compatibility**: There is **zero tolerance** for `snake_case` in v3 responses. Clients using old v3 consumers MUST regenerate their DTOs.
- **Rollback Strategy**: If a rollback to a pre-camelCase version is required, ensure the client-side DTOs are also rolled back to `snake_case` versions simultaneously to avoid field name mismatches.
