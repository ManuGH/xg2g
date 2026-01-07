# xg2g Architecture

**Version:** 3.x  
**Last Updated:** 2026-01-07  
**Audience:** CTO, Engineering, QA, SRE

---

## 1. System Overview

### Components

**Backend (Go)**

- API Server (v3 endpoints)
- DVR/Recording Manager
- Library Scanner
- Playback Session Manager
- Diagnostics Controller

**Frontend (React + TypeScript)**

- WebUI (thin client, API client #1)
- V3 Player (HLS.js wrapper)
- No business logic, no state inference

**External Dependencies**

- Enigma2 Receiver (OpenWebIF)
- Storage (recordings, HLS segments)
- FFmpeg (transcoding, muxing)

### Data Flows

**Live TV:**

```
User → WebUI → POST /intents → Session Manager → Tuner Allocation → FFmpeg → HLS Segments → WebUI Player
```

**DVR Recording:**

```
Timer → DVR Manager → Tuner Allocation → FFmpeg Record → RECORDING state → FINISHING → FINISHED → Library Ownership
```

**Playback:**

```
User → WebUI → GET /sessions/{id}/hls → Session State Check → HLS Playlist/Segments → WebUI Player
```

**Diagnostics:**

```
WebUI → GET /system/diagnostics → Subsystem Checkers (Receiver/DVR/Library/EPG) → Aggregated Health → UI Routing Decision
```

---

## 2. Source of Truth (SSOT)

### Backend as SSOT (ADR-001)

**Principle:** Backend owns ALL state and lifecycle decisions.

**What Backend Owns:**

- Session state (new, running, failed, stopped)
- Recording lifecycle (recording, finishing, finished)
- Tuner allocation (which slot, lease TTL)
- Error codes and retry guidance (`Retry-After` header)
- Subsystem health (diagnostics aggregation)

**What Frontend NEVER Decides:**

- ❌ Session readiness (guessing "is stream ready?")
- ❌ Retry timing (custom backoff logic)
- ❌ Error interpretation (503 → "probably still building")
- ❌ Lifecycle state (inferring recording is "done")

**WebUI = API Client #1**

- Queries backend for truth
- Displays what backend returns
- Reacts to backend state changes
- No client-side business logic

**Enforcement:**

- Grep checks for hardcoded delays: `setTimeout(.*,(2000|3000|10000))` = 0 results
- Contract compliance audits (frontend violations documented and fixed)
- 3-question merge rule: "What does backend tell me to do?"

---

## 3. State & Lifecycle Models

### Recording Lifecycle (ADR-003)

**States:**

```
RECORDING  → DVR owns, actively recording
FINISHING  → DVR owns, finalizing (stable window wait)
FINISHED   → Library owns, scan-eligible
```

**Ownership Rules:**

- **DVR Phase:** RECORDING + FINISHING (DVR has exclusive ownership)
- **Library Phase:** FINISHED (Library scanner can index)
- **No Dual Ownership:** Recording cannot be in both DVR and Library simultaneously

**Transition Triggers:**

- RECORDING → FINISHING: Timer ends OR user stops
- FINISHING → FINISHED: Stable window elapsed (60s default)
- FINISHED → Library: Scan detects new file

**Stable Window Rationale:**

- Prevents Library scanner from seeing incomplete files
- Allows FFmpeg to finalize muxing
- Ensures duration metadata correctness

### Session Lifecycle

**States:**

```
new       → Intent accepted, not started
starting  → Worker allocating resources
running   → Stream active
failed    → Resource allocation failed OR error during playback
stopped   → User stopped OR worker cleaned up
```

**Retry Behavior:**

- `failed` → Frontend shows error (no auto-retry unless `Retry-After` header present)
- `stopped` → Final state (no retry)

### Scan Gates

**Library Scanner Conditions:**

- File state: FINISHED (not RECORDING or FINISHING)
- File age: >= stable window
- File size: > 0 bytes
- No concurrent DVR ownership

---

## 4. Error & Retry Semantics

### HTTP Status Codes (Contract-Driven)

**503 Service Unavailable:**

- **Meaning:** Temporary unavailability (subsystem busy, not failed)
- **Examples:** Library scan running, tuner slots exhausted, receiver temporarily busy
- **Header:** `Retry-After: 10` (always present per CONTRACT-001)
- **Frontend:** Retry after exactly 10 seconds (no custom backoff)
- **Incident:** ❌ NO (expected operational state)

**502 Bad Gateway:**

- **Meaning:** Upstream (receiver) error
- **Examples:**
  - `UPSTREAM_UNAVAILABLE`: Receiver not reachable
  - `UPSTREAM_RESULT_FALSE`: Receiver returned error (result=false)
- **Frontend:** Show error to user (investigate receiver)
- **Incident:** ✅ YES (if sustained >5min)

**404 Not Found:**

- **Meaning:** Resource doesn't exist
- **Examples:** Recording ID invalid, session not found
- **Frontend:** Show "not found" error
- **Incident:** ❌ NO (user error OR cleanup race)

### Retry-After Normalization (CONTRACT-001)

**Contract Rule:** All 503 responses include `Retry-After: 10` header.

**Before (Drift):**

- Some endpoints: 5s
- Some endpoints: 30s
- Some endpoints: missing

**After (Normalized):**

- **All endpoints:** 10s
- **Frontend:** No guessing, use header value exactly

**Enforcement:**

- Backend: All 503 handlers set `Retry-After: 10`
- Frontend: No `setTimeout` with hardcoded delays (grep enforced)

### Degraded vs Unavailable (ADR-006)

**Degraded:**

- **Meaning:** Partial functionality available
- **Example:** Receiver down, Library still accessible
- **UI:** Show warning banner, disable affected features
- **Incident:** ❌ NO (monitor, don't page)

**Unavailable:**

- **Meaning:** Total service outage
- **Example:** All subsystems down, database unavailable
- **UI:** Show error page
- **Incident:** ✅ YES (page on-call immediately)

**Decision Matrix:**

| `overall_status` | Incident? | Action |
|------------------|-----------|--------|
| `ok` | No | Normal operation |
| `degraded` | No | Monitor (auto-recover expected) |
| `unavailable` | Yes | Page on-call (P1) |

---

## 5. Security & RBAC

### Admin-Only Endpoints (ADR-008)

**Pattern:** Middleware-based authorization (not handler logic)

**Example:**

```go
// Route definition
r.With(s.authMiddleware, s.ScopeMiddleware("v3:admin")).Get("/system/config", s.GetSystemConfig)

// Handler (assumes auth success)
func (s *Server) GetSystemConfig(w http.ResponseWriter, r *http.Request) {
    // No manual RBAC check needed - middleware enforces
    // Return config data
}
```

**Admin-Only Endpoints:**

- `GET /system/config` (sensitive configuration)
- `GET /sessions/debug` (all sessions dump)
- `POST /internal/setup/validate` (setup validation)

**Enforcement:**

- Middleware applies before handler
- Tests verify 403 for non-admin
- Contract documents scope requirements

### Token Handling

**Preferred:** `Authorization: Bearer <token>` header

**Fallback:** `?api_token=<token>` query parameter (for HLS.js compatibility)

**Security:**

- Tokens scoped (`v3:read`, `v3:write`, `v3:admin`)
- RBAC enforced at middleware layer
- Fail-closed (no auth = 401)

---

## 6. Non-Goals (Explicit Exclusions)

### What This Architecture Does NOT Support

**UI Heuristics:**

- ❌ Frontend guessing "stream is probably ready"
- ❌ Custom retry logic (ignoring `Retry-After`)
- ❌ Status inference ("503 means building")

**Dual Ownership:**

- ❌ Recording in both DVR and Library
- ❌ Frontend deciding lifecycle state
- ❌ Scanner processing RECORDING files

**Feature Flags:**

- ❌ Conditional behavior based on flags
- ❌ A/B testing within core paths
- ❌ "Try this, fallback to that" logic

**Magic Behavior:**

- ❌ Undocumented retry strategies
- ❌ Silent error recovery
- ❌ Opportunistic caching

**Rationale:** Complexity kills platforms. Explicit beats implicit. Boring beats clever.

---

## 7. Key Design Principles

### Principle 1: Contract-First (ADR-004)

**Every API behavior defined before implementation.**

**3-Question Merge Rule:**

1. Which contract point do I fulfill?
2. Which test breaks if I build this wrong?
3. Which runbook applies on production failure?

**Cannot answer = PR rejected**

### Principle 2: Thin-Client (ADR-005)

**Frontend = View layer only**

**Backend complexity stays on backend:**

- State machines → Backend
- Retry logic → Backend (via `Retry-After`)
- Error semantics → Backend (via error codes)

**UI complexity indicators (forbidden):**

- `setTimeout` with hardcoded values
- State inference (guessing lifecycle)
- Custom retry algorithms

### Principle 3: Diagnostics-Driven (ADR-007)

**UI always queries `/system/diagnostics` first**

**Use Cases:**

- Default tab selection (DVR if receiver ok, Library if receiver down)
- Error messages (specific subsystem failure)
- Degraded state handling (disable features, show warnings)

**Anti-Pattern:**

- Trying endpoint, assuming failure reason

### Principle 4: Failure as State (ADR-006)

**503 ≠ Error**

**503 = Expected operational condition**

- Library scan in progress
- Tuner slots exhausted
- Receiver temporarily busy

**Frontend Response:**

- Respect `Retry-After`
- Show user-friendly message
- No panic, no alerts

**Incident Policy:**

- Sustained (>30min) degradation during off-peak = investigate
- Total unavailability = page on-call

---

## 8. Decision Authority

**Design Authority:** CTO

**ADR Process:**

- New architectural decision → ADR draft
- CTO reviews and approves
- ADR becomes binding (all code must comply)

**No Exceptions:**

- PRs violating ADR are rejected
- "I didn't know" is not accepted (ADR-INDEX is SSOT)

**Consultation:**

- Engineering leads: Technical feasibility
- QA: Testability
- SRE: Operability

**Escalation:** None (CTO decision is final)

---

## 9. References

**Contracts:**

- [API_CONTRACT.md](../contracts/README.md) - API behavior specification
- [CERTIFICATION_GATES.md](../contracts/README.md) - Compliance verification

**Decisions:**

- [ADR-INDEX.md](../decisions/ADR-INDEX.md) - All architectural decisions

**Culture:**

- [team_culture_shift_onepager.md](../contracts/README.md) - Contract-first development

**Operations:**

- [Runbooks](../contracts/README.md) - Operational procedures

---

## Acceptance Test

**A new Senior Engineer should be able to answer:**

1. **Why does a 503 occur?**
   - Temporary unavailability (scan running, tuner exhausted, receiver busy)

2. **Who decides when to retry?**
   - Backend (via `Retry-After` header). Frontend never invents retry timing.

3. **When is an incident triggered?**
   - `overall_status: unavailable` (total outage)
   - NOT triggered for `degraded` (partial functionality)

4. **Who owns a recording in FINISHING state?**
   - DVR (Library scanner waits for FINISHED)

5. **Where does the UI get subsystem health?**
   - `/system/diagnostics` (not by trying endpoints and guessing)

**If answers unclear → architecture document incomplete.**

---

**This architecture is binding. All code must comply.**
