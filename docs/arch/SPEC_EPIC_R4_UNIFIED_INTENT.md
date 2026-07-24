# Spec: Epic R4 — Unified Intent Envelope for Live and VOD

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epic R3 (Single Delivery Format + JIT Copy Path) merged to `main`.

---

## 1. Context and Problem Statement

Currently, live playback and VOD playback maintain separate signed intent envelopes:
- Live playback uses `PlannerReceipt` (`internal/domain/playbackplanner`).
- VOD playback uses `BuildIntent` (`internal/domain/playbackprofile/ports`).

Both envelopes serve identical architectural purposes: carrying verified, planner-issued playback claims end-to-end to fail-closed delivery endpoints. Maintaining two separate intent types creates duplicate serialization code, duplicate key rotation logic, and multiple audit sinks.

### Core Objectives of Epic R4:
1. **Unified Intent Envelope**: Consolidate `BuildIntent` and `PlannerReceipt` into a single `PlaybackIntent` type with a `mode` discriminator (`"live"` vs `"vod"`).
2. **Unified Issuance & Verification**: Single cryptographic issuance and verification pipeline owned by `internal/domain/playbackplanner`.
3. **Unified Signing Secret**: Consolidate signing keys under `playback.intent_signing_key` while maintaining key-rotation backwards compatibility.

---

## 2. Invariants

- **I1 — Single signing primitive.** All intent envelopes (Live and VOD) use the same signing and verification mechanism.
- **I2 — Fail-closed verification.** Any tampered or expired intent is rejected with a typed error (`ErrInvalidIntentSignature`).
- **I3 — Backward compatibility during migration.** Existing live receipts and VOD target profiles remain verifiable during the transition window.
- **I4 — No behavior change without a test.** Full test suite parity before and after.

---

## 3. Implementation Slices

### Slice R4.1 — PlaybackIntent Model & Signing Domain
- **Goal**: Define `PlaybackIntent` type and unified sign/verify functions in `internal/domain/playbackplanner`.
- **Files**:
  - `[NEW] internal/domain/playbackplanner/intent_envelope.go`
  - `[NEW] internal/domain/playbackplanner/intent_envelope_test.go`

### Slice R4.2 — VOD & Live Adapter Integration
- **Goal**: Update VOD resolver and Live intent handler to issue and verify `PlaybackIntent`.
- **Files**:
  - `[MODIFY] internal/control/http/v3/recordings/artifacts/resolver.go`
  - `[MODIFY] internal/control/http/v3/intents_adapter.go`
  - `[MODIFY] internal/domain/playbackplanner/planner.go`

---

## 4. Verification Plan

### Automated Tests
- `go test -v ./internal/domain/playbackplanner/...`
- `make ci-pr`

### Manual Verification
- Deploy to Staging (`./scripts/fast_deploy.sh --confirm-staging`) and verify both Live TV and VOD playback work cleanly.
