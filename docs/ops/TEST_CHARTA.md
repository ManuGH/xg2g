# Test Charta: Universal Policy

> **Strategic Directive**: Tests must secure Product Guarantees. Tests checking implementation details are noise and should be removed.

## 1. P0 Guarantees (Release Blockers)

| Guarantee | Coverage Strategy | Current Status | Gap / Action |
|-----------|-------------------|----------------|--------------|
| **Universal Policy Only**<br>(No Legacy Profiles) | `Config.Validate`<br>`API Contract Test` | ✅ Backend Config<br>❌ Frontend UI | **Gap**: UI allows profile selection.<br>**Action**: Remove legacy UI code (Done), Add Contract Test. |
| **Fail-Closed Auth**<br>(No Token = No Access) | `Middleware Tests`<br>`Integration Smoke` | ⚠️ Partial<br>(Middleware exists, but sparse negative tests) | **Gap**: No explicit test for "Invalid Token" or "Missing Scope".<br>**Action**: Add 3 core Auth invariant tests. |
| **Config Safety**<br>(Invalid Config = Fail Start) | `Loader Tests`<br>`Validation Tests` | ✅ Strong<br>(Covered by `config_strict_test.go`) | **Gap**: None.<br>**Action**: Keep current suite. |
| **API Contract Stability**<br>(/system/config is Truth) | `Codegen Drift Check`<br>`Schema Validation` | ✅ Strong<br>(CI enforces drift check) | **Gap**: No runtime verification of values.<br>**Action**: Add simple curl-based contract verify. |

## 2. P1 Guarantees (Operational Stability)

| Guarantee | Coverage Strategy | Current Status | Gap / Action |
|-----------|-------------------|----------------|--------------|
| **Engine Lifecycle**<br>(Clean Shutdown/Lease) | `Integration Smoke` | ✅ Covered | Keep. |
| **HLS Persistence**<br>(Segments Retained) | `Recorder Tests` | ⚠️ Implicit | Monitor. |

## 3. Review Decisions: Kill or Keep

### Keep

- **`internal/config`**: Essential protection against misconfiguration.
- **`internal/api/v3`**: Essential for API stability.
- **`internal/validate`**: Low cost, high value.
- **`test/integration`**: The only true "does it boot" test.

### Review / Kill Candidate

- **`internal/epg` (Detail Tests)**: If tests check specific XML parsing details irrelevant to overall function, simplify.
- **`internal/telemetry` & `internal/metrics`**: If these test internal counters excessively, reduce to existence checks.

## 4. Minimal Gap-Closure Plan (Immediate)

To reach "Green" status for Release, we must implement:

1. **Backend Auth Invariants (x3)**:
    - `TestAuth_NoToken_Returns401`
    - `TestAuth_InvalidToken_Returns403`
    - `TestAuth_MissingScope_Returns403`
2. **Frontend Smoke Tests (x3)**:
    - `Render_App_Builds` (covered by build) -> **Upgrade to runtime check**: `test_player_mounts`, `test_settings_load`
    - *Note*: Given 0 current tests, we will start with a basic **Vitest** setup.
3. **Contract Tests (x2)**:
    - `TestContract_SystemConfig_Result` (Go integration test)
    - `TestContract_PreparingError_Shape` (Go integration test)
