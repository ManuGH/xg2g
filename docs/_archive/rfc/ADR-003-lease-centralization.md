# ADR-003: Lease Responsibility Centralization

**Status**: Accepted
**Date**: 2025-12-30
**Component**: API / Worker (v3)
**Context**: Fixing "Owner Mismatch" & "Hardware Coupling" in Session Management

## Implementation Reference (v3.0.5)

- **Feature Flag**: `XG2G_V3_API_LEASES` (Phase 1=true, Phase 2=false)
- **Secrets**: `XG2G_IDEM_SECRET` (Deprecated/Unused)
- **Verification**: `internal/api/auth_strict_test.go` (`TestRaceSafety_ParallelIntents`)
  - Proves: Atomic Idempotency (1 Event), Phase 2 Behavior (2x 202).

## 1. Context & Problem Statement

Currently, the responsibility for acquiring "Leases" (locks for Tuners and Service Deduplication) is split between two layers:

1. **API Layer (`/api/v3/intents`)**: Acquires a "Dedup Lease" to decide if it should return 202 or 409.
2. **Worker Layer (`Orchestrator.handleStart`)**: Re-acquires "Dedup Lease" (to refresh ownership) and acquires "Tuner Lease" (hardware resource).

### Weaknesses

- **Owner Mismatch**: If the API acquires a lease but the Worker crashes or fails to start, the lease hangs until TTL expiry.
- **Semantic Drift**: Both layers must agree exactly on "what keys to lock". Any drift causes bugs (e.g., API allows, Worker rejects).
- **Coupling**: The API layer has to know about hardware constraints (Tuners), which violates clean separation of concerns.

## 2. Decision

We will **centralize all Lease Acquisition logic** solely within the **Worker (Orchestrator)**.

The API Layer will become an "Intent Enqueue" system, responsible only for Authentication, Validation, and Idempotency.

## 3. Architecture & Contracts

### 3.1 API Layer: Intent Only

**Responsibility**: AuthN -> AuthZ -> Validate -> Idempotency -> Publish.

**Workflow**:

1. **Auth**: Validate Token & Scopes (`v3:write`).
2. **Validate**: Check payload schema (ServiceRef, Profile).
3. **Idempotency**: Compute `IdempotencyKey`.
    - Check Store: If `Key` maps to an existing `SessionID`, return `202 Accepted` + `SessionID`.
4. **Persist**: Create `SessionRecord` (State=`NEW`) or `IntentRecord`.
5. **Publish**: Emit `StartSessionEvent` to Bus.
6. **Return**: Always `202 Accepted` (unless payload invalid).

**Key Change**: The API **NEVER** calls `TryAcquireLease`. It assumes the Worker will handle resource contention.

### 3.2 Worker Layer: Sole Lease Owner

**Responsibility**: Consume Intent -> Acquire Leases -> Execute Pipeline.

**Workflow (`Orchestrator.handleStart`)**:

1. **Dedup Lease**: Attempt to acquire `lease:service:{sRef}`.
    - *If Busy*: Check if I am the owner (re-entrant/idempotent).
        - If YES: Continue (refresh).
        - If NO: Mark Session as `REJECTED_BUSY` (or `ended` with `R_LEASE_BUSY`). Stop.
2. **Tuner Lease**: Attempt to acquire `lease:tuner:{slot}`.
    - *If Busy*: Mark Session as `REJECTED_NO_TUNER`. Stop.
3. **Execute**: Start Transcoder/Pipeline.
4. **Maintain**: Renew leases periodically.
5. **Cleanup**: Release leases on stop/error.

### 3.3 Idempotency Key Design (Server-Side)

To prevent "Thundering Herd" (multiple users starting same channel) without API-side locking, we rely on stable Idempotency Keys using **SHA256 (Canonical Payload)**.

**Policy**: The Server MUST generate the key based on request parameters.
**Payload**: `v1:stream.start:<ServiceRef>:<Profile>:<Bucket>`
**Security**: Keys are deterministic; no secret is required.

### 3. VOD & Idempotency Scope

- **Scope**: The `/api/v3/intents` endpoint and its idempotency guarantees cover **Live Streams** and **Live-Catchup/Timeshift** only.
- **Recordings**: Recording playback uses a separate flow (`/api/v3/recordings/...`) and does not participate in this intent/lease system.
- **Key Generation**: Idempotency keys are derived from `SHA256("v1:stream.start:<ServiceRef>:<Profile>:<Bucket>")`.
  - **Bucket**: "0" for Live, `StartMs` derivative for Catchup.
  - **Secrets Deprecated**: `XG2G_IDEM_SECRET` is no longer used; keys are strictly server-side deterministic.

### 4. Observability Gate (Phase 2 Check)

Before removing the feature flag, the following metrics must indicate stability:

- `v3_intents_total{outcome="conflict", mode="phase2"}` should be near zero (handled by Worker).
- `v3_idempotent_replay_total` should accurately track retry volume.
- `v3_worker_start_total{outcome="rejected_busy"}` should correlate with theoretical conflicts.

To guarantee single-event publishing in Phase 2, `PutSessionWithIdempotency` MUST be atomic.

**Signature**:

```go
func (s *Store) PutSessionWithIdempotency(ctx, session, key, ttl) (existingID string, exists bool, err error)
```

**Workflow**:

1. Compute `IdempotencyKey`.
2. Call `PutSessionWithIdempotency`.
3. **If Exists**: Return `202 Accepted` with `sessionId=existingID`. **DO NOT Publish Event**.
4. **If New**: Publish `StartSessionEvent`. Return `202 Accepted`.

### 3.5 Busy Contract (Conflict Resolution)

Since the API no longer checks locks, it **always defaults to 202 Accepted** (unless Payload Invalid 400).

- **Scenario**: User A starts Channel X. User B starts Channel X (different params -> different Key).
- **API**: Both return `202 Accepted`.
- **Worker**:
  - Session A: Running.
  - Session B: Terminal (`R_LEASE_BUSY`).
  - **Resolution**: Worker marks User B's Session as `ended` with reason `R_LEASE_BUSY` (or `REJECTED_BUSY`).
- **UX**: Client B polls session state, sees "Ended/Busy" immediately.

## 4. Migration Plan (Zero Downtime)

We will use a feature flag `XG2G_V3_API_LEASES` to control the transition.

1. **Phase 1 (Flag ON)**: Default. API calls `TryAcquireLease`. If busy, returns `409 Conflict`.
2. **Phase 2 (Flag OFF)**: API skips lease. Returns `202 Accepted`. Worker handles busy logic.
3. **Phase 3**: Verify stability. Remove flag.

## 5. Acceptance Criteria

### 5.1 Parallel Start (Thundering Herd)

**Test**: 2 concurrent `POST /api/v3/intents` for the **same** Channel/Profile.
**Expectation**:

- **Phase 1**: 1x `202`, 1x `409` (API Dedup).
- **Phase 2**: 2x `202`. Both return **SAME** `session_id` (Idempotency Key Match).
- **Worker**: Only **1** Tuner Lease acquired.

### 5.2 Lease Busy (Conflict)

**Test**: Start Channel A. Then Start Channel A again (different params -> different Key).
**Expectation**:

- **Phase 2**: Both return `202` (different session_ids).
- **Worker**:
  - Session A: Running.
  - Session B: Terminal (`R_LEASE_BUSY`).

### 5.3 Hard Kill Recovery

**Test A: Kill without Restart (Crash)**

- Action: `kill -9` Worker. API remains up.
- Result: Lease persists until TTL expires (e.g., 30s). New attempts fail (Busy) until TTL.

**Test B: Kill with Restart (Deployment)**

- Action: `kill -9` Worker. Worker restarts immediately.
- Result: `OnStartup` -> `Store.DeleteAllLeases()` (Single-Writer Hack).
- Outcome: **Immediate availability** for new intents (Flush clears zombie locks).

## 6. Security Implications

- **Auth Strictness**: Reject paths (401/403) must strictly assume "No Side Effects" (checked by `auth_strict_test.go`).
- **LAN Guard**: Relies on robust `X-Forwarded-For` parsing (Fixed in `lan_guard.go`).
