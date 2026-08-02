# Policy Engine Reason Code Matrix Governance

This document establishes the authoritative, machine-readable reason code catalog for the **Policy & Preemption Engine (ADR-031)**.

## Reason Code Catalog

| Reason Code | Category | Description | Decision Outcome |
| :--- | :--- | :--- | :--- |
| `POLICY_GRANTED_RESOURCE_AVAILABLE` | Grant | Target resource has free capacity and a matching compatible candidate. | `GRANT` |
| `POLICY_REJECTED_PROTECTED_ACTIVITY` | Rejection | Resource request is blocked by a sacrosanct allocation or active scheduled recording. | `REJECT` |
| `POLICY_REJECTED_EQUAL_OR_LOWER_PRIORITY` | Rejection | Resource request cannot preempt active allocations of equal or higher priority. | `REJECT` |
| `POLICY_REJECTED_RESOURCE_NOT_REQUIRED` | Rejection | Consumer type does not compete for the requested resource kind. | `REJECT` |
| `POLICY_REJECTED_NO_COMPATIBLE_CANDIDATE` | Rejection | Unallocated capacity exists, but no available compatible candidate candidate exists. | `REJECT` |
| `POLICY_PREEMPTION_REQUIRED` | Preemption | Preemption of a lower-priority allocation is required to fulfill request. | `PREEMPTION_REQUIRED` |
| `POLICY_INVALID_INPUT` | Validation | Evaluation request or snapshot failed structural validation. | `REJECT` |

## Invariant Governance Rules

1. **Immutability:** Reason codes are stable machine-readable contracts. Renaming or altering string values is strictly forbidden.
2. **Separation of Decision and Execution:** Reason codes in this package represent policy decision evaluations only. Pipeline execution status codes (`PREEMPTION_EVICTION_*`) reside exclusively in the pipeline orchestration layer.
3. **Traceability:** Every `EvaluationResult` emitted by `PolicyEngine.Evaluate()` must contain a non-empty `ReasonCode` registered in this matrix.
