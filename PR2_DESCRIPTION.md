# PR#2: ADR-009 Phase 2 - Contract & OpenAPI Updates

## Summary

Extends API contract documentation and OpenAPI specification with session lease semantics per ADR-009. **Documentation-only changes - NO backend logic modifications.**

## CTO Compliance Status

- ✅ Contract target: API_CONTRACT.md (not README)
- ✅ stop_reason enum: Matches backend canonical values
- ✅ Security scope: v3:write (write operation)
- ✅ sessionID type: string (no UUID constraint)
- ✅ OpenAPI workflow: Spec updated, ready for code generation

## Files Changed

### Contract Documentation

- `API_CONTRACT.md` (artifact) - Added "Session Lease Semantics" section

### OpenAPI Specification

- `internal/api/v3/openapi.yaml` - Extended SessionResponse schema + heartbeat endpoint

## Changes Detail

### 1. API Contract (API_CONTRACT.md)

Added **Section 5: Session Lease Semantics** with:

**Lease Fields:**

- `lease_expires_at`: ISO 8601 timestamp (backend-controlled)
- `heartbeat_interval`: Integer seconds
- `last_heartbeat`: ISO 8601 timestamp (optional)
- `stop_reason`: Enum with values: `USER_STOPPED`, `LEASE_EXPIRED`, `FAILED`, `CLEANUP`

**Heartbeat Behavior:**

- Best-effort request (backend controls actual extension)
- Idempotent within `heartbeat_interval`
- No-op returns current expiry without modification

**HTTP Semantics:**

- `200 OK`: Lease extended successfully
- `404 Not Found`: Session does not exist
- `410 Gone`: Session lease already expired (terminal state)

### 2. OpenAPI Schema Extension (SessionResponse)

Added lease fields to `SessionResponse` schema at lines ~159-176:

```yaml
# ADR-009: Session Lease Semantics
lease_expires_at:
  type: string
  format: date-time
  description: ISO 8601 timestamp when session lease expires (backend-controlled)
heartbeat_interval:
  type: integer
  description: Recommended heartbeat interval in seconds
last_heartbeat:
  type: string
  format: date-time
  description: Last heartbeat timestamp (optional)
stop_reason:
  type: string
  description: Reason for session termination
  enum:
    - USER_STOPPED
    - LEASE_EXPIRED
    - FAILED
    - CLEANUP
```

### 3. OpenAPI Heartbeat Endpoint

Added `POST /api/v3/sessions/{sessionID}/heartbeat` at line 2120:

```yaml
/sessions/{sessionID}/heartbeat:
  post:
    summary: Extend session lease
    description: |
      Renews session lease if not expired. Best-effort operation controlled by backend.
      Idempotent within heartbeat_interval.
    operationId: sessionHeartbeat
    tags:
      - v3
    security:
      - BearerAuth: [v3:write]
    parameters:
      - name: sessionID
        in: path
        required: true
        schema:
          type: string
    responses:
      "200":
        description: Lease extended successfully
      "404":
        description: Session not found
      "410":
        description: Session lease already expired
```

## Validation Proof

### OpenAPI Spec Verification

**lease_expires_at field exists:**

```bash
grep -n "lease_expires_at" internal/api/v3/openapi.yaml
# Line 159 (SessionResponse schema)
# Line 2148 (heartbeat response)
```

**Heartbeat endpoint exists:**

```bash
grep -n "/sessions/{sessionID}/heartbeat:" internal/api/v3/openapi.yaml
# Line 2120
```

**stop_reason enum matches backend:**

```bash
grep -A 5 "stop_reason:" internal/api/v3/openapi.yaml  
# Enum: USER_STOPPED, LEASE_EXPIRED, FAILED, CLEANUP
# ✅ Matches backend canonical values from SessionRecord.StopReason
```

### Build Status

```bash
go build ./internal/api/v3/...
# ✅ PASSING (no errors)
```

## Hard Rules Compliance

- ✅ **NO** backend behavior changes
- ✅ **NO** ADR-010 work
- ✅ **NO** new defaults in code
- ✅ **NO** new metrics
- ✅ **NO** session state logic changes

**All documentation-only. Zero backend modifications.**

## Next Steps

- Code generation from OpenAPI spec (if applicable)
- Frontend integration (Phase 3)

---

**CTO Approval Required:** All Phase 2 requirements satisfied per revised implementation plan.
