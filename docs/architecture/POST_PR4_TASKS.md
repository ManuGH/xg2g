# Post-PR4 Follow-up Tasks

## PR 4.1: OpenAPI Spec Fix

**Owner:** TBD
**Priority:** P0 (blocks clean drift resolution)
**Estimated Time:** 30 min - 1h

### Scope

Fix schema errors in `api/openapi.yaml` to enable clean regeneration.

### Specific Tasks

1. Debug schema error: "map key 'Service' not found"
2. Fix schema (likely missing/incorrect reference)
3. Run `make generate` → verify clean output
4. Verify `git diff --exit-code -- internal/api/server_gen.go` passes
5. Confirm `InstantTune` removed from generated code

### Definition of Done

- [ ] `make generate` runs without errors
- [ ] `internal/api/server_gen.go` has no `InstantTune` references
- [ ] CI drift-check passes
- [ ] No manual edits to generated code
- [ ] OpenAPI spec validates (optional: add linter step)

### Out of Scope

- API functionality changes
- New endpoints
- Breaking changes to contract

---

## PR 5: P1 Verifications

**Owner:** TBD
**Priority:** P1 (product claim risk reduction)
**Estimated Time:** 2-3h total

### Task 5a: Health/Readiness Verification (30 min)

**Objective:** Verify `/healthz` and `/readyz` endpoints work as documented.

**Steps:**

1. Start service: `./xg2g start` or `docker-compose up`
2. Test liveness:

   ```bash
   curl -i http://localhost:8088/healthz
   # Expected: 200 OK (always when process alive)
   ```

3. Test readiness (default):

   ```bash
   curl -i http://localhost:8088/readyz
   # Expected: 200 OK (when dependencies ready)
   ```

4. Test `READY_STRICT` mode:

   ```bash
   export XG2G_READY_STRICT=true
   export XG2G_OWI_BASE=http://invalid-receiver:80
   ./xg2g start
   # Expected: Fail-start OR readyz returns 503
   curl -i http://localhost:8088/readyz
   # Expected: 503 (if service started)
   curl -i http://localhost:8088/healthz
   # Expected: 200 (healthz unaffected)
   ```

**Deliverable:**

- `docs/ops/HEALTH.md` with:
  - Endpoint URLs and semantics
  - Status codes and meanings
  - `READY_STRICT` behavior
  - Example curl commands

**Definition of Done:**

- [ ] All curl tests documented with expected results
- [ ] `READY_STRICT` behavior verified
- [ ] Documentation committed

---

### Task 5b: WebUI Thin Client Audit (1.5-2h)

**Objective:** Prove WebUI is API-client #1 with no hidden policy logic.

**Audit Questions (Hard Criteria):**

1. **API Call Inventory:**
   - List all endpoints WebUI uses (route + purpose)
   - Document data flow: API → UI state

2. **Policy Red-Flags:**
   - ❌ UI sets defaults not from API?
   - ❌ UI "corrects" or transforms backend responses?
   - ❌ localStorage/sessionStorage as "shadow config"?
   - ❌ Retry/backoff logic without server hints (Retry-After, error codes)?
   - ❌ Stream mode selection (WebIF vs Direct) decided by UI?
   - ❌ Recording stability heuristics in UI?
   - ❌ Special endpoints only for UI?

3. **Classification:**
   - ✅ **PASS:** UI is pure presentation + UX (loading states, retry based on server hints)
   - ⚠️ **NEEDS CHANGE:** Minor violations with clear fix path
   - ❌ **FAIL:** Policy decisions in UI → must move to backend

**Deliverable:**

- `docs/architecture/WEBUI_THIN_CLIENT_AUDIT.md` with:
  - API call inventory (table: Route | Purpose | Data Used)
  - Findings (PASS/FAIL with evidence)
  - Violations (if any) with fix plan
  - Verdict: "Thin client confirmed" or "Violations found"

**Definition of Done:**

- [ ] All WebUI API calls documented
- [ ] Policy red-flags checked
- [ ] Findings documented with evidence
- [ ] Fix backlog created (if violations found)
- [ ] Audit committed to repo

---

## Team Challenge Message (Copy/Paste)

**Subject:** PR 4 Merged - New Standards Effective Immediately

Team,

PR 4 is merged. From now on, these are **non-negotiable standards**:

### 1. Architecture Claims Require Evidence

- ✅ **Verified:** Code-backed, tested, documented in `VERIFIED_GUARANTEES.md`
- 🎯 **Guideline:** Aspirational, documented in `ARCHITECTURAL_GUIDELINES.md`
- 🔍 **Unverified:** Tracked in `OPEN_VERIFICATIONS.md` with clear DoD

**Rule:** Don't claim it unless you can prove it or mark it as a guideline.

### 2. OpenAPI Contract Discipline

- **DoD for API changes:** `make generate` + `git diff --exit-code` must pass
- **CI enforces this:** Drift = PR blocked
- **No manual edits** to generated code

### 3. WebUI is API-Client #1

- **Rule:** If it only works via WebUI → it's a bug
- **Anti-pattern:** UI-only features, shadow config, policy in UI
- **Enforcement:** PR 5 audit will catch violations

### 4. Fail-Closed Security

- **Rule:** Security failures = fail-start, never degrade gracefully
- **Examples:** Invalid CIDR → fail-start, partial TLS → fail-start
- **No exceptions**

**Next:** PR 5 is an audit PR (Health/Readiness + WebUI). Findings will be public and tracked.

---

## PR Sequencing

**Immediate:**

- ✅ PR 4: MERGED

**Short-term:**

- 🔧 PR 4.1: OpenAPI spec fix (P0)
- 🔍 PR 5: Verifications (P1)

**Medium-term:**

- 📊 PR 6: Engine reality check (P2)
- 🦁 PR 7: Safari audit (P2)
