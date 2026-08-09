# Master System Reason Code Matrix Governance

This document serves as the unified systemwide master index for all machine-readable reason code contracts in `xg2g`.

## Master Registry

### 1. Resource Conflict Policy Engine (`POLICY_*`)
Governed by [ADR-031](../ADR/031-resource-conflict-policy-engine.md) and detailed in [POLICY_REASON_CODES.md](POLICY_REASON_CODES.md).

| Reason Code | Category | Description | Decision Outcome |
| :--- | :--- | :--- | :--- |
| `POLICY_GRANTED_RESOURCE_AVAILABLE` | Grant | Target resource has free capacity and a matching compatible candidate. | `GRANT` |
| `POLICY_REJECTED_PROTECTED_ACTIVITY` | Rejection | Resource request is blocked by a sacrosanct allocation or active scheduled recording. | `REJECT` |
| `POLICY_REJECTED_EQUAL_OR_LOWER_PRIORITY` | Rejection | Resource request cannot preempt active allocations of equal or higher priority. | `REJECT` |
| `POLICY_REJECTED_RESOURCE_NOT_REQUIRED` | Rejection | Consumer type does not compete for the requested resource kind. | `REJECT` |
| `POLICY_REJECTED_NO_COMPATIBLE_CANDIDATE` | Rejection | Unallocated capacity exists, but no available compatible candidate exists. | `REJECT` |
| `POLICY_PREEMPTION_REQUIRED` | Preemption | Preemption of a lower-priority allocation is required to fulfill request. | `PREEMPTION_REQUIRED` |
| `POLICY_INVALID_INPUT` | Validation | Evaluation request or snapshot failed structural validation. | `REJECT` |

### 2. Tuner Lease & Reconciliation Engine (`LEASE_*` & `RECONCILIATION_*`)
Governed by [ADR-029](../ADR/029-resource-arbitration-composite-lease-model.md) and [ADR-030](../ADR/030-lease-reconciliation.md).

| Reason Code | Category | Description |
| :--- | :--- | :--- |
| `LEASE_ACQUIRED` | Lifecycle | Tuner lease successfully acquired and bound to session. |
| `LEASE_EXPIRED` | Reconciliation | Lease expired due to TTL timeout and was compensatorily released. |
| `LEASE_RELEASED_EXPLICIT` | Teardown | Lease explicitly released by session owner. |
| `RECONCILIATION_REQUIRED` | Recovery | Ambiguous backend state requires startup or runtime reconciliation. |

### 3. Session & Playback Pipeline (`SESSION_*`)
Governed by [ADR-023](../ADR/023-device-enrollment-session-model.md) and [ADR-025](../ADR/025-playback-confidence-policy.md).

| Reason Code | Category | Description |
| :--- | :--- | :--- |
| `SESSION_ACTIVE` | State | Playback session actively consuming stream. |
| `SESSION_TERMINATED_PREEMPTED` | Eviction | Session terminated due to tuner preemption by higher priority consumer. |

---

## Governance Rules

1. **Uniqueness:** Every machine-readable reason code string in the codebase must be registered in this index exactly once.
2. **Immutability:** String values of reason codes are immutable API contracts. Altering string values is a breaking change.
3. **Automated Verification:** Automated invariant unit tests verify that all reason code constants in Go packages match this master registry.
