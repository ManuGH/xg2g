# Playback Decision Engine - Normative Spec Index

This document is the **Single Source of Truth** for the Playback Decision Engine
contract. It maps architectural requirements to their technical proofs and
operational procedures.

## 1. Normative Hierarchy

1. **ADR-009 suite**: Foundational contract and semantics.
    - [ADR-009: Decision Engine Core](file:///root/xg2g/docs/ADR/009-playback-decision-engine.md)
    - [ADR-009.1: Container Neutrality Patch](file:///root/xg2g/docs/ADR/009.1-playback-decision-spec-patch.md)
    - [ADR-009.2: Hash & Unicode Normality](file:///root/xg2g/docs/ADR/009.2-hash-semantics.md)
2. **Property Proofs**: Frozen mechanical invariants (`proof_test.go`).
3. **Operation Policy**: [Client Profiles (Range/HLS Policy)](file:///root/xg2g/docs/ops/CLIENT_PROFILES.md).
4. **Incident Playbook**: [Playback Triage](file:///root/xg2g/docs/ops/INCIDENT_PLAYBOOK_DECISION.md).
5. **Storage Strategy**: [ADR-020: SQLite Truth](file:///root/xg2g/docs/ADR/ADR-020_STORAGE_STRATEGY.md)
   and [Storage Invariants](file:///root/xg2g/docs/ops/STORAGE_INVARIANTS.md).
6. **Normative Root Keys**: Shared list defined in `decode.go` (`rootKeys`).

---

## 2. Invariant Traceability Map

| Invariant ID | Description | ADR Clause | Proof (Property Test) |
| :--- | :--- | :--- | :--- |
| **INV-001** | Fail-Closed (Schema) | ADR-009 §1 | `TestProp_FailClosed_InvalidSchema` |
| **INV-002** | Determinism | ADR-009 §4 | `TestProp_Determinism` |
| **INV-003** | Monotonicity | ADR-009 §5 | `TestProp_Monotonicity_*` |
| **INV-004** | Container Neutrality | ADR-009.1 §3 | `TestProp_ContainerMismatch_*` |
| **INV-005** | Unicode Invariance | ADR-009.2 §2 | `TestProp_UnicodeWhitespace_Equivalence` |
| **INV-006** | No Mixed Schema | ADR-009.2 §1 | `TestProp_NoMixedSchemaAmbiguity_Exhaustive` |
| **INV-007** | Dual-Decode Compat | ADR-009.2 §1 | `TestProp_DualDecode_Compatibility` |
| **INV-008** | Schema-less Rejection | ADR-009.2 §1 | `TestProp_SchemaLess_Always400` |
| **INV-009** | Root Objects Are JSON Objects | ADR-009.2 §1 | `TestProp_SourceMustBeObject_Compact` |

---

### 3.1 Closed World Policy

To ensure zero-drift between detection and telemetry, the following keys are
the **only** recognized top-level schema tags. Any unrecognized root keys are
rejected (fail-closed, 400).

| Compact (v3.1+) | Legacy (v3.0) |
| :--- | :--- |
| `source` | `Source` |
| `caps` | `Capabilities` |
| `policy` | `Policy` |
| `api` | `APIVersion` |
| `rid` | `RequestID` |

- **Root Objects**: If a root key is present, its value must be a JSON object.
  Non-object values (e.g., `null`, `[]`, strings) are rejected (400).
- **Shared Keys**: `source.fps` / `Source.fps` are intentionally identical and
  exempt from overlap rejection.

---

### Failure Classification

- **Client Fault (4xx)**:
  - **400**: `capabilities_invalid`.
  - **412**: `capabilities_missing`.
  - **422**: `decision_ambiguous`.
- **Policy Denial (200 + Deny)**: Valid input, but no compatible path exists.
- **Engine Fault (500)**: `invariant_violation`. Stop the Line.

---

### Correlation & Replay

Every decision emits a `Trace.InputHash`. This hash is the **Semantic Identity**
of the request.

- Replay tool: `make replay RID=<id>`
- Search logs for: `xg2g.decision.schema`
