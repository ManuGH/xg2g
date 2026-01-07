# Contracts Directory

**Purpose:** Single source of truth for all API contracts and compliance requirements.

**Last Updated:** 2026-01-07

---

## Active Contracts

### API_CONTRACT.md

**Location:** Artifact storage (reference copy)  
**Canonical Source:** [API_CONTRACT.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/API_CONTRACT.md)

**Scope:** Complete API v3 behavior specification

- HTTP status codes and semantics
- Retry-After header requirements
- Error codes and messages
- Caching rules
- Thin-client principles

**Valid From:** 2026-01-07 (Certification A)

**Change Process:**

1. Propose amendment via ADR
2. CTO approval required
3. Update contract + version bump
4. Re-certification if P0 changes

---

### CERTIFICATION_GATES.md

**Location:** Artifact storage  
**Canonical Source:** [CERTIFICATION_GATES.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CERTIFICATION_GATES.md)

**Scope:** Binary pass/fail criteria for contract compliance

- Gate 1: Frontend compliance (thin-client enforcement)
- Gate 2: QA hard gates (contract violation tests)

**Valid From:** 2026-01-07 (Certification A issued)

---

## Compliance Artifacts

### Current Certification

**Grade:** A  
**Issued:** 2026-01-07  
**Document:** [CERTIFICATION_A_FINAL.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CERTIFICATION_A_FINAL.md)

### Resolved Violations

- [CONTRACT-001-RESOLVED.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CONTRACT-001-RESOLVED.md) - Retry-After normalization
- [CONTRACT-002-VERIFIED.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CONTRACT-002-VERIFIED.md) - RBAC verification
- [CONTRACT-003-RESOLVED.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CONTRACT-003-RESOLVED.md) - Tuner exhaustion Retry-After
- [CONTRACT-004-RESOLVED.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/CONTRACT-004-RESOLVED.md) - UPSTREAM_RESULT_FALSE error code

### QA Evidence

- [QA_TEST1_PASS_EVIDENCE.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/QA_TEST1_PASS_EVIDENCE.md) - 503 without Retry-After test
- [QA_TEST2_STATUS.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/QA_TEST2_STATUS.md) - 503 with Retry-After test

---

## Runbooks (SRE)

Contract-related operational runbooks:

- [RUNBOOK_UPSTREAM_RESULT_FALSE.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/RUNBOOK_UPSTREAM_RESULT_FALSE.md)
- [RUNBOOK_TUNER_SLOTS_EXHAUSTED.md](file:///root/.gemini/antigravity/brain/efe47437-ba93-4c56-ad22-659c39b03381/RUNBOOK_TUNER_SLOTS_EXHAUSTED.md)

---

## Contract Amendment Process

### When Amendment Required

**Mandatory for:**

- New HTTP status code usage
- New error code definition
- Header requirement changes
- State machine modifications
- Security boundary changes

**Not Required for:**

- Implementation bug fixes (behavior stays same)
- Performance optimizations (no external change)
- Internal refactoring

### Amendment Steps

1. **Propose:** Create ADR with contract delta
2. **Review:** CTO reviews impact
3. **Approve:** CTO approves/rejects
4. **Update:** Amend contract document (version bump)
5. **Re-certify:** Execute compliance phase if P0 changes
6. **Deploy:** Merge + announce

### Version Scheme

- **v3.0:** Initial contract
- **v3.1:** Minor amendments (backward compatible)
- **v4.0:** Breaking changes (requires migration)

---

## Relationship to ADRs

**Contracts define WHAT**

- API behavior
- Response formats
- Error semantics

**ADRs define WHY**

- Architectural decisions
- Trade-off justifications
- Design rationale

**Example:**

- Contract: "503 includes Retry-After: 10"
- ADR-006: "Failure is state, not exception" (explains why)

---

## Enforcement

### Pre-Merge

- PR must reference contract section
- 3-question rule enforced
- Tests must validate contract compliance

### Post-Merge

- QA gate tests run on each deploy
- Compliance audits quarterly
- Violations = rollback

### Consequences of Violation

**P0 Violation (Breaking):**

- Immediate rollback
- Root cause analysis
- Process improvement

**P1 Violation (Non-Breaking):**

- Fix within 48h
- Ticket created
- Retrospective

---

## Key Contacts

**Contract Authority:** CTO  
**Compliance Owner:** QA Lead  
**Runbook Owner:** SRE Lead

**Questions:** #engineering-platform Slack

---

**All contract changes require CTO approval. No exceptions.**
