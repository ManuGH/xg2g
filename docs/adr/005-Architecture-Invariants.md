# ADR-ARCH-001: Architecture Invariants (2026 Ready)

## Status

Accepted (2026-01-06)

## Context

As xg2g matures into a standard infrastructure component, the cost of architectural technical debt increases exponentially. To ensure 2026-level reliability and maintainability, we must establish hard invariants that govern all future development.

## Decision

### A. Einzige Quelle der Wahrheit (Single Source of Truth - SSOT)

The Backend is the only authority for state, policy, and logic.

- **Backend Responsibility**: State computation, policy decisions, retry/fallback/degradation heuristics.
- **WebUI Role**: Pure state projector (Thin Client).
- **Prohibited in Client**:
  - Retry heuristics or complex error recovery.
  - Format or profile selection logic.
  - Implicit state transitions or "helpful" auto-fixes.
- **Rule**: If complex logic is required in the client, the API contract is insufficient.

### B. Package Isolation as Contractual Boundary

Internal packages (`vod`, `pipeline`, `auth`, `scheduler`) are policy-agnostic "Black Boxes".

- **Isolation Rules**:
  - No knowledge of HTTP routes, User Sessions, or Feature Flags.
  - Interaction purely via deterministic contracts (Input -> Output/Error).
- **Rule**: A package that knows "where" it is being called from is incorrectly architected.

### C. Single Output Contract

We provide exactly one delivery contract to minimize the test matrix and bug surface.

- **Contract**: H.264 / AAC / fMP4 / HLS.
- **Prohibited**:
  - Browser-specific special paths or heuristics.
  - Legacy shims or multi-profile support.
- **Rule**: Client incompatibility is a "Non-Goal", not a bug. We fix bugs in the contract, not individual clients.

## Consequences

- **Positive**: Reduced complexity, predictable behavior, linear test matrix.
- **Negative**: Some extreme edge-case clients may lose support. Hardware requirements are strictly defined.
