# v3 API Change Log

## [v3.1.1] - 2026-02-27

### Added

- **Operator-grade live stream security boundary**: strict HS256 `playbackDecisionToken` with `iss`/`aud`/`sub`/`jti`/`iat`/`nbf`/`exp`, short TTL policy (≤120s) and deterministic verification hook (`VerifyStrictAt`).
- **DecisionReason SSOT**: machine-readable `decisionReason` codes exposed via `PlaybackInfo` DTO/OpenAPI for deterministic client telemetry and debugging.
- **Player recovery hardening**: deterministic HLS.js recovery policy (`NETWORK_ERROR` exponential backoff with caps; `MEDIA_ERROR` single-shot recovery) + explicit `recovering` state.

### Fixed

- **Fail-closed intents**: unified 401/403/400 semantics for `/api/v3/intents` with RFC7807 `problem+json` responses and stable `/problems/...` type URIs.
- **CORS correctness**: credentialed CORS no longer falls back to `Access-Control-Allow-Origin: *`; `Origin` is reflected with `Vary: Origin`, and requests without `Origin` emit no CORS headers.
- **Live ref validation split**: `ValidateLiveRef` separated from DVR path validation; rejects path traversal and cross-contamination.

### Governance Gates

- **Boundary gates mechanically proven (A–E)**:
  - **A: E2E claim mismatch** → 403 `CLAIM_MISMATCH`
  - **B: JWT parser strictness** → 401 on malformed/forged input; no panics
  - **C: Media path authz** → Bearer-only 401, session cookie 200
  - **D: DecisionReason codes stable via SSOT**
  - **E: CORS header assertions** → never `*` with credentials; `Vary: Origin`
- Legacy intent tests remediated to test admission/preflight/lease logic post-JWT gate.

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
