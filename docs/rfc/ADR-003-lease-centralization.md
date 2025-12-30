# ADR-003: Lease Responsibility Centralization

**Status**: Proposed
**Date**: 2025-12-30
**Component**: API / Worker (v3)
**Context**: Fixing "Owner Mismatch" & "Hardware Coupling" in Session Management

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
3. **Dedup Lease**: Attempt to acquire `lease:service:{sRef}`.
    - *If Busy*: Check if I am the owner (re-entrant/idempotent).
        - If YES: Continue (refresh).
        - If NO: Mark Session as `REJECTED_BUSY` (or `ended` with `R_LEASE_BUSY`). Stop.
4. **Tuner Lease**: Attempt to acquire `lease:tuner:{slot}`.
    - *If Busy*: Mark Session as `REJECTED_NO_TUNER`. Stop.
5. **Execute**: Start Transcoder/Pipeline.
6. **Maintain**: Renew leases periodically.
7. **Cleanup**: Release leases on stop/error.

### 3.3 Idempotency Key Design

To prevent "Thundering Herd" (multiple users starting same channel) without API-side locking, we rely on stable Idempotency Keys using **HMAC-SHA256**.

**Configuration**:

- `XG2G_IDEM_SECRET`: Required. If missing, generate random on startup (but this breaks idempotency across restarts).
- **Rotation**: To support rotation, the system should try verifying with `CurrentSecret`, then `PreviousSecret` (if configured). For *generation*, always use `CurrentSecret`.

**Definition**:

```go
key = HMAC_SHA256(
    secret, 
    "v3_intent" + "|" + type + "|" + target_id + "|" + profileID + "|" + bucket
)
```

**Bucketing & Target**:

- **Live Streams**:
  - `target_id = serviceRef`
  - `bucket = "0"` (Global deduplication for Live).
- **VOD**:
  - `target_id = recordingID` (CRITICAL: Must distinguish assets).
  - `bucket = floor(start_ms / 1000)` (1-second bucket).
  - *Rationale*: A seek to T=10s vs T=10.5s should probably share a session, but T=60s is a new intent.

### 3.4 Busy Contract (Conflict Resolution)

Since the API no longer checks locks, it **always defaults to 202 Accepted** (unless Payload Invalid 400).

- **Scenario**: User A starts Channel X. User B starts Channel X (different profile/params -> different Idempotency Key).
- **API**: Both return `202 Accepted`.
- **Worker**:
  - User A's Intent: Acquires Lock -> Starts.
  - User B's Intent: Sees Lock Busy.
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
- **HMAC Secret**: Must be secured in env vars. Rotation support ensures long-term security.
- **LAN Guard**: Relies on robust `X-Forwarded-For` parsing (Fixed in `lan_guard.go`).
