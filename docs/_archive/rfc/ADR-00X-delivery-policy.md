# ADR-00X: Replace Streaming Profiles with Delivery Policy (Universal Default)

**Status:** Proposed → Accepted (with PR 5.2)  
**Date:** 2026-01-06  
**Deciders:** Engineering Team  
**Context:** Post-Thin Client Audit, PR 5.1 completion

---

## Context

Historically, "streaming profiles" (`auto`, `safari`, `safari_hevc_hw`) existed as workarounds for client-side differences. This created:

- **UI Policy Violations:** Frontend made policy decisions (auto-switch on error)
- **Config Drift:** More options than actual value
- **Test Explosion:** Matrix of Clients × Profiles
- **Unclear Product Truth:** "What is actually the default?"

**Evidence from Thin Client Audit:**

- Profile selection logic in UI (localStorage)
- Auto-profile-switch on error (policy in frontend)
- Hardcoded defaults (`'auto'`)

---

## Decision

We **eliminate** the concept of "profiles" from product and API perspective.

We introduce `streaming.delivery_policy` with a single value: **`universal`**.

### Policy Semantics

`streaming.delivery_policy=universal` guarantees:

- **Video:** H.264 (AVC)
- **Audio:** AAC
- **Container:** fMP4
- **Delivery:** HLS
- **Safari-compliant:** Segment/GOP constraints enforced server-side
- **No client-side fallbacks:** No UI automatisms, no browser detection

### Non-Goals

- ❌ No HEVC/x265 as default
- ❌ No automatic policy selection based on User-Agent
- ❌ No "auto-switch" mechanics in frontend
- ❌ No per-client profile variations

---

## Rationale

### Why "Delivery Policy" instead of "Profile"?

**"Profile"** implies:

- Client-specific variants
- User choice
- Runtime switching

**"Delivery Policy"** implies:

- Server-defined contract
- Single source of truth
- Operational guarantee

### Why only "universal"?

**Simplicity:**

- One code path to test
- One pipeline to maintain
- Clear product contract

**Extensibility:**

- Future policies (e.g., `hevc_experimental`) require:
  - New ADR
  - Audit
  - Test plan
  - Explicit opt-in
- No accidental proliferation

---

## Consequences

### Positive

✅ **API Simplification:** No profile enum, no allowed_profiles list  
✅ **WebUI Simplification:** No dropdown, no localStorage, no auto-switch  
✅ **Test Focus:** One delivery path, clear guarantees  
✅ **Product Clarity:** "Universal works everywhere" is the promise  

### Negative

⚠️ **Migration Cost:** Rename config keys, regenerate types, update UI  
⚠️ **Future Flexibility:** Adding a second policy requires formal process  

### Neutral

- Existing `auto` behavior becomes `universal` (no functional change if already Safari-compliant)
- Users who set `XG2G_STREAM_PROFILE=safari` will need to update to `XG2G_STREAMING_POLICY=universal`

---

## Implementation

See PR 5.2 implementation plan for detailed file-by-file changes.

**Key Changes:**

1. Config: `default_profile` → `delivery_policy`
2. Env: `XG2G_STREAM_PROFILE` → `XG2G_STREAMING_POLICY`
3. API: `streaming.default_profile` → `streaming.delivery_policy`
4. WebUI: Remove profile dropdown, localStorage, auto-switch
5. Backend: Consolidate pipeline to single "universal" path

---

## Alternatives Considered

### Alternative 1: Keep "auto" as default

**Rejected:** "auto" implies automatic selection, which violates Thin Client principle.

### Alternative 2: Multiple policies (safari, chrome, firefox)

**Rejected:** Creates test matrix explosion, unclear which is "correct".

### Alternative 3: User-Agent detection

**Rejected:** Server-side browser detection is fragile and violates API-only truth.

---

## Verification

### Acceptance Criteria

- [ ] Zero-config deployment uses `universal` policy
- [ ] No profile selection UI exists
- [ ] No auto-switch code paths exist
- [ ] `/api/v3/system/config` returns `delivery_policy: "universal"`
- [ ] All tests pass with single policy
- [ ] Documentation reflects "universal is the only supported policy"

### Rollback Plan

If `universal` doesn't work for a specific client:

1. **First:** Fix the pipeline (it's a bug, not a feature request)
2. **Last Resort:** Revert to profiles (requires new ADR justifying why universal failed)

---

## References

- [Thin Client Audit](../architecture/WEBUI_THIN_CLIENT_AUDIT.md)
- [PR 5.1: Thin Client Violations Fix](../../../.gemini/antigravity/brain/7f9ab0ef-0899-4e0d-97d6-bf77884f5109/walkthrough.md)
- [Architectural Guidelines](../architecture/ARCHITECTURAL_GUIDELINES.md)

---

## Team Communication

**Subject:** PR 5.2 – "Profile" is eliminated: Only Delivery Policy (universal) remains

Team,

We are ending the concept of "Streaming Profiles" completely. Starting with PR 5.2, there are no browser-specific profiles/fallbacks. Instead, we introduce `streaming.delivery_policy=universal` (server-side default, zero-config).

**New Guardrails (non-negotiable):**

1. **Backend is Source of Truth.** No UI policy. No auto-switch.
2. **Universal is the only supported delivery path.** If Safari/Chrome/Firefox differ: it's a bug, not a new profile.
3. **Any additional policy** (e.g., HEVC) requires: ADR, test plan, audit, opt-in – otherwise not allowed.

**Review Criterion:**

If the PR contains "switch profile", "fallback to safari", "user-agent", "localStorage profile" → **REJECTED**.

**Next Goal:** PR 5.2 delivers a unified pipeline, reduces options, makes the product more maintainable.

---

**Status:** ✅ **ACCEPTED** (with PR 5.2)
