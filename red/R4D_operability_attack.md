# RED R4-D: Operability Attack Matrix

**Status:** COMPLETE
**Phase:** R4-D (Operability Attack)
**Goal:** Prove that decision explainability and replayability are "best in class".

---

## Attack 1: The "Silent Deny"

**Attack:** Create an input that results in `ModeDeny` but has no reasons or rule hits explaining why.
**Test:** `TestProp_Explainability_NoEmptyReasonsOnDeny`
**Result:** ✅ PASS
**Finding:** Every `ModeDeny` decision now carries at least one reason and at least one rule hit. An operator will never see a silent denial.

---

## Attack 2: Replay Drift

**Attack:** Take a production-like decision, export it to Canonical JSON, then try to replay it. If the replayed decision differs (even if valid), operability fails.
**Test:** `TestProp_ReplayArtifact_ReproducesDecision`
**Result:** ✅ PASS
**Technical Fix:** Synchronized `DecisionInput` JSON tags with `CanonicalJSON` compact tags (`c`, `v`, `br`, etc.) to ensure 100% roundtrip fidelity.

---

## Attack 3: Hidden State (R2-C Redux)

**Attack:** Run the decision twice on the same pointer.
**Result:** ✅ PASS (Covered by `TestProp_Determinism`)
**Finding:** The engine remains stateless. No hidden counters or context drift.

---

## Attack 4: Semantic Hash Bypass

**Attack:** Send `{"caps": {"rng": null}}` vs `{"caps": {"rng": false}}`.
**Result:** ✅ PASS (Covered by `TestProp_NormalizedEquivalence_Strict`)
**Finding:** Both inputs produce the same `InputHash`. Caching and incident correlation are protected against tri-state boolean drift.

---

## Final Assessment

The engine is now **fully Operable**.

1. **Explainable**: No mode changes without reasons.
2. **Reproducible**: 100% roundtrip fidelity via compact Canonical JSON.
3. **Cacheable**: Semantic InputHash protects against phantom drift.
