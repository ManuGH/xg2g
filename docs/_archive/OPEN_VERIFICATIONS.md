# Open Verifications

**Purpose:** Transparent tracking of unverified architectural claims.

These are areas where we have **structural evidence** but not **complete verification**. This prevents self-deception and sets clear expectations.

Last updated: 2026-01-06

---

## 🔍 Pending Verifications

### 1. Engine Implementation Depth

**Claim:** "Event-driven engine with intent/session orchestration"

**Evidence:**

- ✅ Config exists (`XG2G_ENGINE_ENABLED`, `XG2G_ENGINE_MODE`)
- ✅ ARCHITECTURE.md documents intent-based API
- ✅ V3_FSM.md documents session states
- ⚠️ Actual implementation not audited

**What needs verification:**

- [ ] Review `/internal/pipeline/worker/` implementation
- [ ] Verify FSM state transitions match spec
- [ ] Confirm lease-based tuner coordination
- [ ] Check crash recovery behavior

**Assigned to:** TBD
**Target:** PR 5 or dedicated review session

---

### 2. WebUI Architecture (Thin Client Claim)

**Claim:** "WebUI is a thin client with no business logic"

**Evidence:**

- ✅ WebUI exists (`/webui/` with React/TypeScript)
- ✅ API endpoints exist
- ⚠️ Component code not reviewed for business logic

**What needs verification:**

- [ ] Audit WebUI components for policy decisions
- [ ] Check for UI-side defaults or heuristics
- [ ] Verify all state comes from API
- [ ] Confirm no "shadow config" in localStorage

**Assigned to:** TBD
**Target:** Dedicated WebUI review session

---

### 3. Safari Optimization Coverage

**Claim:** "Safari/HLS optimization throughout"

**Evidence:**

- ✅ `RecordingsStableWindow` safeguard exists
- ✅ HLS delivery documented
- ⚠️ Other Safari-specific optimizations not catalogued

**What needs verification:**

- [ ] Document all Safari-specific code paths
- [ ] Verify HLS spec compliance
- [ ] Test seek behavior on Safari/iOS
- [ ] Check endlist handling

**Assigned to:** TBD
**Target:** Safari compatibility audit

---

### 4. Health/Readiness Endpoint Implementation

**Claim:** "Health and readiness checks implemented"

**Evidence:**

- ✅ ARCHITECTURE.md mentions `/healthz` and `/readyz`
- ⚠️ Endpoints not verified to exist

**What needs verification:**

- [ ] Confirm `/healthz` endpoint exists and returns 200
- [ ] Confirm `/readyz` endpoint exists
- [ ] Verify `READY_STRICT` mode behavior
- [ ] Check dependency health checks

**Assigned to:** TBD
**Target:** Quick verification (should be 5 minutes)

---

### 5. OpenAPI Regeneration Process

**Claim:** "OpenAPI spec is up to date"

**Evidence:**

- ✅ OpenAPI spec exists (`api/openapi.yaml`)
- ❌ `server_gen.go` references removed flags (zombie drift detected)

**What needs verification:**

- [ ] Regenerate OpenAPI code
- [ ] Add regeneration to CI/CD
- [ ] Document regeneration process
- [ ] Verify no drift between spec and implementation

**Assigned to:** TBD
**Target:** PR 4.1 or PR 5
**Priority:** HIGH (known drift)

---

## 🎯 Verification Priorities

**P0 (Merge-blocking for future PRs):**

- OpenAPI regeneration (known drift)

**P1 (Important for product claims):**

- WebUI architecture audit
- Health/readiness endpoint verification

**P2 (Nice to have, not urgent):**

- Engine implementation depth
- Safari optimization coverage

---

## How to Close a Verification

1. **Do the work** (code review, testing, documentation)
2. **Document findings** (create issue or PR)
3. **Move claim** to either:
   - `VERIFIED_GUARANTEES.md` (if verified)
   - `ARCHITECTURAL_GUIDELINES.md` (if aspirational)
   - Remove claim (if incorrect)
4. **Update this file** (mark as complete, add date)

---

## Why This Document Exists

**Transparency:** We don't claim things we haven't verified.

**Accountability:** Clear tracking of what needs review.

**Trust:** Users/contributors know what's guaranteed vs. planned.

**Pragmatism:** We ship incrementally, but track our gaps.

---

## Template for New Verifications

```markdown
### N. [Feature Name]

**Claim:** "[What we're claiming]"

**Evidence:**
- ✅ [What we've confirmed]
- ⚠️ [What we haven't verified]

**What needs verification:**
- [ ] [Specific task]
- [ ] [Specific task]

**Assigned to:** TBD
**Target:** [PR number or milestone]
**Priority:** [P0/P1/P2]
```
