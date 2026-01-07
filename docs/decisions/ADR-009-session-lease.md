# ADR-009: Session Lease Semantics

**Status:** ✅ ACTIVE  
**Date:** 2026-01-07  
**Author:** Engineering Team  
**Approved By:** CTO  
**Backlog Item:** MUST #1 (Strategic Improvement Backlog)

---

## Context

### Problem Statement

Session states exist (new, starting, running, failed, stopped) but the **time dimension is implicit**.

**Current Issues:**

- No explicit session expiration time
- Ghost sessions accumulate (client disconnects without cleanup)
- SRE cannot explain "how long is this session valid?"
- No foundation for multi-region session handoff

**Example Scenario:**

1. Client starts stream (session → running)
2. Network glitch → client disconnects
3. Session remains in "running" state indefinitely
4. Tuner slot remains allocated
5. Manual cleanup required

---

## Decision

**Add explicit lease semantics to all sessions.**

### API Changes (Additive, Non-Breaking)

**Session Response Schema:**

```json
{
  "session_id": "uuid",
  "state": "running",
  "lease_expires_at": "2026-01-07T20:00:00Z",
  "heartbeat_interval": 5,
  "last_heartbeat": "2026-01-07T19:59:55Z"
}
```

**New Fields:**

- `lease_expires_at` (ISO 8601 timestamp): Absolute expiration time
- `heartbeat_interval` (integer seconds): Client should heartbeat every N seconds
- `last_heartbeat` (ISO 8601 timestamp, optional): Last successful heartbeat

### Backend Behavior

**Lease Lifecycle:**

1. **Session Start:** Lease created with TTL (default: 60s)
2. **Client Heartbeat:** Lease renewed (extends `lease_expires_at`)
3. **Lease Expiry:** Session automatically transitions to `stopped` state
4. **Cleanup:** Resources released (tuner, storage)

**Heartbeat Endpoint (New):**

```
POST /api/v3/sessions/{id}/heartbeat
```

**Response:**

```json
{
  "session_id": "uuid",
  "lease_expires_at": "2026-01-07T20:01:00Z",
  "acknowledged": true
}
```

**Automatic Expiry Logic:**

- Background job checks `lease_expires_at` every 10s
- Expired sessions → state transition to `stopped`
- Reason: `LEASE_EXPIRED`

---

## Rationale

### Why Explicit Leases?

**1. No Ghost Sessions:**

- Client crash/disconnect → auto-cleanup
- No manual intervention needed

**2. Operational Clarity:**

- SRE sees exact expiration time
- Support can explain "session expires in 30s"

**3. Deterministic Behavior:**

- No implicit timeouts
- Contract-defined lifecycle

**4. Multi-Region Foundation:**

- Lease handoff enables session migration
- Not implemented now, but architecture-ready

### Why Heartbeat Pattern?

**Proven pattern:**

- Kubernetes uses it (pod liveness)
- Distributed systems standard
- Simple to implement client-side

**Alternative Considered:**

- WebSocket keep-alive: Too stateful, not RESTful
- Video progress polling: Already exists, different purpose

---

## Consequences

### Positive

**1. Automatic Resource Cleanup:**

- Tuner slots released on client disconnect
- No capacity leaks

**2. Better Observability:**

- Metrics: `sessions_expired_total` (counter)
- Alerts: High expiry rate = client issues

**3. Client Behavior Improvement:**

- Forces clients to implement heartbeat
- Makes disconnects visible

### Negative

**1. Client Complexity:**

- Must implement heartbeat loop
- Failure mode: Session expires during playback

**Mitigation:**

- WebUI already has player event handlers
- Heartbeat = 5 lines of code (`setInterval`)

**2. Backend Job:**

- New background worker for expiry check

**Mitigation:**

- Lightweight (query + state update)
- Runs every 10s (low overhead)

### Breaking Changes

**None.** All fields additive.

**Backward Compatibility:**

- Old clients: Sessions expire after default TTL (60s)
- New clients: Can heartbeat to extend

---

## API Impact Summary

### Modified Endpoints

**GET /api/v3/sessions/{id}**

- **Added fields:** `lease_expires_at`, `heartbeat_interval`, `last_heartbeat`
- **Backward compatible:** Old clients ignore new fields

**POST /api/v3/intents** (session creation)

- **Added response fields:** `lease_expires_at`, `heartbeat_interval`
- **Backward compatible:** Old clients ignore new fields

### New Endpoints

**POST /api/v3/sessions/{id}/heartbeat**

- **Purpose:** Renew session lease
- **Auth:** Same as session access (v3:read)
- **Idempotent:** Yes

---

## Implementation Plan

### Phase 1: Backend (Core Logic)

**Tasks:**

1. Add lease fields to `SessionRecord` struct
2. Implement lease expiry background job
3. Add `POST /sessions/{id}/heartbeat` endpoint
4. Update session creation to set initial lease

**Effort:** ~2 days

### Phase 2: Contract & Tests

**Tasks:**

1. Update API_CONTRACT.md with lease semantics
2. Add tests for lease expiry
3. Add tests for heartbeat endpoint
4. Update OpenAPI spec

**Effort:** ~1 day

### Phase 3: Frontend (WebUI)

**Tasks:**

1. Implement heartbeat interval loop
2. Handle lease expiry gracefully
3. Show warning if lease expiring soon

**Effort:** ~1 day

**Total Effort:** ~4 days

---

## Configuration

**New Config Values (Optional):**

```yaml
sessions:
  lease_ttl: 60s        # Default lease duration
  heartbeat_interval: 5  # Client heartbeat frequency
  expiry_check_interval: 10s  # Background job frequency
```

**Defaults are sensible** - no config change required.

---

## Metrics & Observability

**New Metrics:**

- `xg2g_sessions_lease_expired_total` (counter): Sessions expired due to lease timeout
- `xg2g_sessions_heartbeat_total` (counter): Successful heartbeats
- `xg2g_sessions_heartbeat_late_total` (counter): Heartbeats after lease expiry

**Logs:**

- Level: INFO
- Event: `session.lease_expired`
- Fields: `session_id`, `state`, `reason=LEASE_EXPIRED`

---

## Security Considerations

**No new attack surface:**

- Heartbeat endpoint = same auth as session access
- Rate limiting: Heartbeat every 5s (not spammable)
- Lease expiry = cleanup (reduces attack surface)

---

## Alternatives Considered

### Alternative 1: Implicit Timeout (Status Quo)

**Rejected because:**

- Ghost sessions accumulate
- No SRE visibility
- No contract guarantee

### Alternative 2: WebSocket Keep-Alive

**Rejected because:**

- Too stateful (violates RESTful principle)
- Harder to scale (connection pooling issues)
- Client complexity higher

### Alternative 3: Video Progress as Heartbeat

**Rejected because:**

- Different purpose (playback position vs liveness)
- Not all sessions have video progress (catchup at position 0)

---

## References

**Aligns With:**

- [ADR-001: Backend as SSOT](file:///root/xg2g/docs/decisions/ADR-INDEX.md) - Lease managed by backend
- [ADR-006: Failure as State](file:///root/xg2g/docs/decisions/ADR-INDEX.md) - Expiry is expected state

**Contract Impact:**

- [API_CONTRACT.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/API_CONTRACT.md) - Session schema update

---

## Acceptance Criteria

**This ADR is accepted when:**

- [ ] CTO approves decision
- [ ] API impact documented
- [ ] No architecture violations identified

**Implementation is done when:**

- [ ] Backend lease logic implemented
- [ ] Heartbeat endpoint functional
- [ ] Tests passing (lease expiry, heartbeat)
- [ ] Contract updated
- [ ] WebUI heartbeat loop working

---

**Status:** ✅ ACTIVE (CTO approved 2026-01-07)

**Implementation may proceed.**
