# Architectural Guidelines

**Purpose:** Design principles and guardrails for contributors.

These are **aspirational standards**, not current-state guarantees. They guide development decisions and code review.

---

## Core Principles

### 1. Backend is Source of Truth

**Guideline:** All policy decisions, defaults, and validation happen server-side.

**Why:** Prevents drift between clients, ensures consistent behavior, enables non-UI clients.

**Review Questions:**

- Does this logic belong in the backend or is it presentation?
- Can a CLI tool use this without the WebUI?
- Is this a policy or a display preference?

---

### 2. WebUI is API Client #1 (Not Special Case)

**Guideline:** WebUI uses the same API as external clients. No special endpoints, no hidden state.

**Why:** Ensures API completeness, prevents UI-only features, enables integrations.

**Review Questions:**

- Can Home Assistant / CLI / mobile app do this without the WebUI?
- Does the WebUI bypass any API layer?
- Is there "shadow config" in the UI?

**Anti-patterns to avoid:**

- ❌ UI sets implicit defaults
- ❌ UI "corrects" backend responses
- ❌ UI stores state not reflected in API
- ❌ UI makes policy decisions (e.g., "when to retry")

---

### 3. Fail-Closed Security

**Guideline:** Security failures prevent startup or deny access. Never degrade gracefully to insecure state.

**Why:** Home-lab security is critical. Misconfiguration should be loud, not silent.

**Examples:**

- Invalid CIDR → fail-start
- Partial TLS config → fail-start
- No tokens configured → deny all requests
- Invalid auth mode → fail-start

---

### 4. Configuration as Product Surface

**Guideline:** Every config key is a product decision. Defaults matter. Validation is mandatory.

**Why:** Config is how operators interact with the system. Poor config UX = poor product.

**Standards:**

- Every key has a default (or explicit "required")
- Every key has validation (or explicit "any value ok")
- Removed keys are documented
- ENV overrides are explicit (not implicit merge)

---

### 5. Observability is Not Optional

**Guideline:** Metrics, logs, and health checks are first-class features, not afterthoughts.

**Why:** Homelab operators need to debug without vendor support.

**Requirements:**

- Prometheus metrics for critical paths
- Structured logs with correlation IDs
- Health/readiness endpoints
- Clear error messages (no "something went wrong")

---

## Engine Architecture (Evolving)

**Current State:** Intent/Session orchestration is structurally present.

**Guideline:** Engine should manage session lifecycle, not just proxy requests.

**Design Goals:**

- Intent-based API (not imperative commands)
- FSM-driven session states
- Lease-based tuner coordination
- Crash recovery via state store

**Review Questions:**

- Does this add to the engine or work around it?
- Is this session state or transient UI state?
- Can this survive a process restart?

---

## Safari Compatibility (Leitlinie)

**Guideline:** Safari/iOS is a first-class target, not "best effort".

**Why:** Safari has strict HLS requirements. Ignoring them = broken product for Apple users.

**Known Considerations:**

- `RecordingsStableWindow` for endlist timing
- HLS playlist structure
- Seek behavior differences

**Review Questions:**

- Have we tested this on Safari/iOS?
- Does this break HLS spec compliance?

---

## Testing Philosophy

**Guideline:** Backend protects itself. UI tests UX.

**Backend Tests (Mandatory):**

- Unit: Config, validation, security invariants
- Integration: HTTP handlers, engine lifecycle
- Contract: API response shapes

**Frontend Tests (Recommended):**

- Component: Rendering, state management
- E2E: User flows, Safari-specific behavior

**Anti-pattern:**

- ❌ Relying on E2E tests to catch backend logic bugs

---

## When to Violate These Guidelines

Guidelines are not laws. Violate them when:

1. You have a **clear reason** (document it)
2. The violation is **temporary** (create a TODO/issue)
3. The alternative is **worse** (explain the trade-off)

**Example valid violation:**
> "WebUI caches channel list for 5s to reduce API load. This is UI-only state, but improves UX. Backend still owns the data."

**Example invalid violation:**
> "WebUI decides retry logic because backend doesn't expose retry hints."
> → Fix: Backend should expose retry hints in error responses.

---

## How to Use This Document

**For Contributors:**

- Read before starting a PR
- Reference in code review comments
- Propose changes via PR to this doc

**For Reviewers:**

- Use as checklist for architectural decisions
- Ask: "Does this align with our guidelines?"
- Flag violations (or document exceptions)

**For Maintainers:**

- Update as architecture evolves
- Keep aligned with VERIFIED_GUARANTEES.md
- Remove guidelines that become guarantees
