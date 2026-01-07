# ADR Index

**Last Updated:** 2026-01-07  
**Maintained By:** Design Authority (CTO)

---

## Active ADRs

### ADR-001: Backend as Source of Truth

**Status:** ✅ ACTIVE  
**Decision:** Backend owns all state and lifecycle decisions. WebUI is strictly an API client.  
**Rationale:** Prevents dual ownership, eliminates frontend state guessing, enables thin-client architecture.  
**Impact:** Frontend never infers state, always queries backend for truth.

### ADR-002: Safari as Reference Browser  

**Status:** ✅ ACTIVE  
**Decision:** Safari compatibility is mandatory for all playback features. Other browsers are best-effort.  
**Rationale:** Safari's strict HLS/codec requirements force good architecture. If it works in Safari, it works everywhere.  
**Impact:** All playback testing must pass in Safari before merge.

### ADR-003: Recording Lifecycle State Machine

**Status:** ✅ ACTIVE  
**File:** [ADR-ENG-003-recording-lifecycle.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/ADR-ENG-003-recording-lifecycle.md)  
**Decision:** Recordings follow strict state transitions: RECORDING → FINISHING → FINISHED. No concurrent ownership.  
**Rationale:** Prevents DVR/Library conflicts, enables clear handoff after stable window.  
**Impact:** DVR releases ownership only after FINISHED state confirmed.

### ADR-004: Contract-First Development

**Status:** ✅ ACTIVE  
**File:** [API_CONTRACT.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/API_CONTRACT.md)  
**Decision:** All API behavior defined by contract before implementation. PRs must answer: contract point, test breakage, runbook.  
**Rationale:** Eliminates ambiguity, enables parallel team work, prevents scope drift.  
**Impact:** No merge without contract reference (3-question rule).

### ADR-005: Thin-Client Architecture

**Status:** ✅ ACTIVE  
**Decision:** Frontend contains zero business logic, zero state inference, zero retry heuristics.  
**Rationale:** Backend complexity belongs on backend. UI complexity ruins everything.  
**Impact:** Frontend compliance enforced via grep checks (no hardcoded delays, no status guessing).

### ADR-006: Failure as State (Not Exception)

**Status:** ✅ ACTIVE  
**Decision:** 503, degraded, unavailable are expected operational states, not errors.  
**Rationale:** Systems degrade gracefully. Retry-After guides recovery. Incidents only for unavailable.  
**Impact:** No logs/alerts for 503 unless sustained. Frontend respects Retry-After.

### ADR-007: Diagnostics as Control Plane

**Status:** ✅ ACTIVE  
**Decision:** `/system/diagnostics` is first query for all UI decisions. No routing without diagnostics.  
**Rationale:** Single source of truth for system health. UI reacts to subsystem status.  
**Impact:** Default tab, error messages, degraded state all driven by diagnostics.

### ADR-008: RBAC Middleware Pattern

**Status:** ✅ ACTIVE  
**Decision:** Authorization enforced via middleware (not handler logic). Handlers assume auth success.  
**Rationale:** Separation of concerns. Reusable scope enforcement. Testable RBAC.  
**Impact:** All admin-only endpoints use `ScopeMiddleware("v3:admin")`.

---

## Superseded ADRs

### ADR-00X: Idempotency Secrets

**Status:** ❌ SUPERSEDED  
**Reason:** Server-side idempotency key generation replaced client secrets.  
**Replaced By:** ADR-004 (Contract-First)

---

## Decision Rules

### When to Create a New ADR

**Mandatory for:**

- Architectural pattern changes (e.g., switching from sync to async)
- API contract amendments (e.g., new error code, new header)
- Security boundary changes (e.g., RBAC policy changes)
- State machine modifications (e.g., new lifecycle states)

**Not Required for:**

- Bug fixes within existing contract
- Performance optimizations without behavior change
- Code refactoring (same external behavior)
- Documentation updates

### ADR Process

1. **Draft:** Author creates ADR with problem statement, options, decision, consequences
2. **Review:** Design Authority (CTO) reviews
3. **Decision:** CTO approves/rejects/requests changes
4. **Active:** ADR becomes binding (all code must comply)
5. **Enforcement:** PRs violating ADR are rejected

### Decision Authority

**Design Authority:** CTO  
**Escalation:** None (CTO decision is final)  
**Consultation:** Engineering leads for technical feasibility, QA for testability

---

## ADR Template

```markdown
# ADR-XXX: [Title]

**Status:** DRAFT | ACTIVE | SUPERSEDED  
**Date:** YYYY-MM-DD  
**Author:** [Name]

## Context
[What problem are we solving?]

## Decision
[What are we deciding?]

## Rationale
[Why this decision?]

## Consequences
[What changes as a result?]

## Alternatives Considered
[What else did we evaluate?]
```

---

## Related Documents

- [API_CONTRACT.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/API_CONTRACT.md) - API behavior contract
- [ARCHITECTURE.md](file:///root/xg2g/docs/architecture/ARCHITECTURE.md) - System architecture
- [CERTIFICATION_GATES.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CERTIFICATION_GATES.md) - Compliance gates
- [team_culture_shift_onepager.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/team_culture_shift_onepager.md) - 3-question merge rule

---

**All architectural decisions require ADR reference. No exceptions.**
