# Phase 5.3 Semantic Deltas & Regression Note

**Date:** 2026-01-21
**Status:** FROZEN
**Grade:** CTO-Final

## 1. Regressions (Accepted)

### 1.1 DirectPlay Defaults to DirectStream

Due to strict "Truth" enforcement in Phase 5.3, many streams that previously qualified for `DirectPlay` (pass-through) now fallback to `DirectStream` (remux) or `Transcode`.

* **Cause:** Strict verification of audio/video codecs against client capabilities. If the container or codec combination is not *explicitly* guaranteed by the `PlaybackCapabilities` (or if truth is ambiguous), the system fails-closed to a safer delivery method.
* **Impact:** Higher CPU/IO usage on the server for scenarios that were previously 0-copy.
* **Mitigation:** `AllowTranscode: true` is now effectively required for robust playback on most clients.

## 2. API Contract Updates (Governance Enforced)

### 2.1 Field Name Reverts (CamelCase)

To maintain backward compatibility (Phase 4 clients), the following field names are **preserved** as `camelCase`. The attempt to move to `snake_case` has been reverted:

* `correlationId` (PRESERVED)
* `sessionId` (PRESERVED)
* `requestId` (PRESERVED)
* Path parameters: `/sessions/{sessionId}` (PRESERVED)

### 2.2 New Response Codes

* **503 Service Unavailable**: Now returned by `GET /stream-info` and playlist endpoints when the system is in `StatePreparing` (e.g., probing or resolving truth).
  * **Header**: `Retry-After: 5`
  * **Behavior**: Clients MUST respect `Retry-After` and retry rather than failing immediately.

## 3. Governance Gates

The following gates are now enforce via `make verify`:

* **Config Surface Sync**: `internal/config/config.go` -> `docs/guides/CONFIGURATION.md`.
* **UI Purity**: Handlers must not access decision logic directly.
* **Contract Freeze**: `testdata/contract/p4_1/golden/*.expected.json` are locked against `GOVERNANCE_BASELINE.json`.
* **OpenAPI Hygiene**: Snake_case enforced (with valid legacy exemptions).
