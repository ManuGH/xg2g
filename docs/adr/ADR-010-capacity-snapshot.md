# ADR-010: Explicit Capacity Snapshot Endpoint

**Status:** DRAFT  
**Date:** 2026-01-07  
**Author:** Engineering Team  
**Backlog Item:** MUST #2 (Strategic Improvement Backlog)

---

## Context

### Problem Statement

**Current situation:**

- UI cannot explain "why not" (e.g., "Why can't I start a stream?")
- Support engineers parse logs to find capacity info
- No single endpoint for system capacity visibility

**User Impact:**

- Generic errors ("Service unavailable")
- No actionable guidance ("Try later" vs "All tuners in use")
- Support tickets for normal capacity limits

**Example Scenario:**

```
User: "Why can't I watch ZDF?"
Support: [checks logs] "All 4 tuners busy"
Better: UI shows "4/4 tuners in use (2 live, 2 recording) - retry in 10s"
```

---

## Decision

**Add explicit capacity snapshot endpoint for observability.**

### Endpoint Specification

```
GET /api/v3/system/capacity
```

**Auth:** v3:read (any authenticated user)

**Response (200 OK):**

```json
{
  "tuners": {
    "total": 4,
    "used": 4,
    "available": 0,
    "by_reason": {
      "live_sessions": 2,
      "recordings": 2
    }
  },
  "recordings": {
    "active": 2,
    "scheduled": 5
  },
  "sessions": {
    "active": 5
  },
  "timestamp": "2026-01-07T19:40:00Z"
}
```

**Response Headers:**

```
Cache-Control: private, max-age=5
ETag: "abc123"
```

---

## Rationale

### Not a Feature - Observability as API

**This is NOT:**

- Resource reservation system
- Admission control logic
- Decision-making endpoint for frontend

**This IS:**

- Current state snapshot (read-only)
- Observability transparency
- Support tool (not UI heuristic)

### Why This Matters

**1. UI Can Explain (Not Decide):**

```
Before: "Service unavailable"
After:  "All 4 tuners in use (3 live streams, 1 recording)"
```

**2. Support Stops Guessing:**

- No log parsing needed
- Instant capacity view
- User can see same info as support

**3. No Ambiguity:**

- Exact numbers (not percentages)
- Real-time (with caching rules)
- Authoritative (backend SSOT)

---

## API Design

### Fields

**tuners:**

- `total`: Configured tuner slots
- `used`: Currently allocated slots
- `available`: total - used
- `by_reason`: **Breakdown of usage** (prevents "why?" questions)
  - `live_sessions`: Tuners used for live playback
  - `recordings`: Tuners used for DVR recording

**recordings:**

- `active`: Recording state = RECORDING
- `scheduled`: Active timers (future recordings)

**sessions:**

- `active`: Session state = running
- **Note:** No `max` field (semantically unclear - not same as tuner limit)

**timestamp:**

- Server time of snapshot (ISO 8601)

### Caching & Load Behavior

**Problem:** Capacity will be polled frequently → DDoS risk

**Solution:**

```
Cache-Control: private, max-age=5
ETag: "state-hash"
```

**Contract Rules:**

1. **Server:** Response cacheable for 5 seconds
2. **Client:** MUST NOT poll faster than every 5 seconds
3. **Optional:** Support `If-None-Match` → 304 Not Modified

**Why 5 seconds:**

- Fast enough for UI responsiveness
- Slow enough to prevent load spikes
- Aligns with other polling intervals (diagnostics: 30s, streams: 5s)

### No Side Effects

**Read-only operation:**

- No state changes
- No resource allocation
- No locks acquired

**Idempotent:** ✅ YES (GET semantics)

---

## Relationship to /system/diagnostics

**NOT a replacement - complementary.**

**Diagnostics:**

- Subsystem health (ok/degraded/unavailable)
- Error conditions
- Qualitative state

**Capacity:**

- Resource utilization
- Quantitative metrics
- Current load

**Example:**

```
diagnostics: { "receiver": "degraded", "library": "ok" }
capacity:    { "tuners": { "total": 4, "used": 0 } }
```

**Interpretation:** Receiver degraded → no streams possible (capacity irrelevant)

---

## Frontend Integration (Thin-Client Compliant)

### CORRECT Usage (Observability)

**Backend already signals error:**

```typescript
// Backend returns 503 + TUNER_SLOTS_EXHAUSTED + Retry-After: 10
if (response.code === 'TUNER_SLOTS_EXHAUSTED') {
  // OPTIONAL: Fetch capacity for UX enhancement
  const capacity = await fetch('/api/v3/system/capacity');
  showError(`All ${capacity.tuners.total} tuners in use - retry in 10s`);
}
```

**Key Point:** Backend decides (503 + error code), capacity is supplemental info only.

### INCORRECT Usage (Heuristic - FORBIDDEN)

**Anti-pattern:**

```typescript
// ❌ WRONG: Using capacity to decide retry strategy
const capacity = await fetch('/api/v3/system/capacity');
if (capacity.tuners.available === 0) {
  // Custom retry logic based on capacity
}
```

**Why Wrong:** Violates thin-client (ADR-005). Backend signals retry via `Retry-After`.

---

## Implementation

### Backend

**File:** `internal/api/v3/system_capacity.go` (new)

**Logic:**

```go
func (s *Server) GetSystemCapacity(w http.ResponseWriter, r *http.Request) {
    // 1. Count tuner leases (from state store)
    // 2. Classify by purpose (live_sessions vs recordings)
    // 3. Count active recordings (state = RECORDING)
    // 4. Count active sessions (state = running)
    // 5. Set Cache-Control header (max-age=5)
    // 6. Return snapshot
}
```

**Complexity:** LOW (read-only aggregation)

**Effort:** ~1 day

---

## Consequences

### Positive

**1. User Experience:**

- Transparent capacity limits
- Actionable error messages (with backend-provided context)
- No "magic" unavailability

**2. Support Efficiency:**

- No log diving for capacity
- User can self-diagnose
- Faster ticket resolution

**3. Operational Visibility:**

- Real-time load monitoring
- Capacity planning data
- No guessing at utilization

### Negative

**1. Minor Information Disclosure:**

- Users see total system capacity

**Mitigation:** Capacity is not sensitive (already observable via behavior)

**2. Polling Load:**

- UI might poll too frequently

**Mitigation:** Cache headers + contract rule (max 1 req/5s)

---

## Security Considerations

**Auth Required:** ✅ YES (v3:read)

**Caching:** ✅ YES (Cache-Control: private, max-age=5, optional ETag/304)

**Rate Limiting:** OPTIONAL (Server MAY return 429 on abuse; not required for MVP)

**Information Disclosure:** Minimal (capacity not secret)

---

## Alternatives Considered

### Alternative 1: Embed Capacity in Error Response

**Rejected because:**

- Couples error handling to capacity
- Not available when no error occurs
- Harder to cache/poll independently

### Alternative 2: Add to /system/diagnostics

**Rejected because:**

- Different purpose (health vs capacity)
- Diagnostics already complex
- Separation of concerns

### Alternative 3: Add sessions.max Field

**Rejected because:**

- Semantically unclear (max sessions ≠ max tuners)
- Would create false expectations
- Better to omit than confuse

---

## Non-Goals

**This endpoint does NOT:**

- Reserve resources
- Predict future availability
- Guarantee admission success
- Replace backend error codes (still need 503 + TUNER_SLOTS_EXHAUSTED)

**It only reports current state for observability.**

---

## Acceptance Criteria

**ADR Accepted When:**

- [ ] CTO approves (with 5 corrections applied)
- [ ] No architecture violations
- [ ] Thin-client compliance verified

**Implementation Done When:**

- [ ] Endpoint returns correct data with breakdown
- [ ] Cache headers set correctly
- [ ] Tests verify aggregation logic
- [ ] Contract updated (polling rules documented)
- [ ] Frontend uses capacity for display only (not decisions)

---

## References

**Aligns With:**

- [ADR-001: Backend as SSOT](file:///root/xg2g/docs/decisions/ADR-INDEX.md)
- [ADR-005: Thin-Client](file:///root/xg2g/docs/decisions/ADR-INDEX.md) - Capacity is observability, not decision logic
- [ADR-007: Diagnostics as Control Plane](file:///root/xg2g/docs/decisions/ADR-INDEX.md)

**Related:**

- [STRATEGIC_IMPROVEMENT_BACKLOG.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/STRATEGIC_IMPROVEMENT_BACKLOG.md) - MUST #2

---

## CTO Corrections Applied

1. ✅ **Endpoint name:** `/api/v3/system/capacity` (consistent with v3 pattern)
2. ✅ **Cache/load behavior:** `Cache-Control: max-age=5`, contract rule: max 1 req/5s
3. ✅ **Used breakdown:** `by_reason` field (live_sessions, recordings)
4. ✅ **sessions.max removed:** Semantically unclear, omitted
5. ✅ **Frontend coupling avoided:** Thin-client compliance section added

---

**Status:** DRAFT (awaiting CTO final approval)  
**Effort:** ~1 day (backend only)
