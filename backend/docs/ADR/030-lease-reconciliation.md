# ADR-030: Lease Reconciliation Engine & Persistent Intent Model

- **Status:** Accepted
- **Date:** 2026-08-02
- **Authors:** Antigravity AI / ManuGH
- **Component:** `internal/pipeline/lease`

## Context & Problem Statement

During process restarts, crashes, context cancellations, or network partitions, the intended resource allocation state maintained by orchestrators or sessions (Intents) can diverge from the actual state maintained by underlying resource providers or single-resource lease backends (Reality).

Without a deterministic reconciliation mechanism:
- Backend leases can remain active after process restarts without a tracking owner (resource leaks / orphans).
- Processes can assume a lease is held when it was terminated or missing in the backend (unexpected resource loss).
- Naive auto-remediation can prematurely delete valid leases if intents are lost or not persisted.

## Decision Drivers

1. **Separation of Intent and Reality:** An intent (`LeaseIntent`) represents a requested or active allocation attempt. A lease (`Lease`) represents the confirmed backend state.
2. **Persistence Across Restarts & Durability:** Intents are persisted to non-volatile JSON storage (`FileIntentStore`) using atomic file replacement (`.tmp` write, `f.Sync()`, `f.Close()`, `os.Rename`, parent directory `d.Sync()`). File corruption triggers `ErrIntentStoreCorrupt`; directory sync failures surface `ErrIntentDurabilityUncertain`.
3. **Single-Writer Enforcement:** `FileIntentStore` enforces a process-wide path registry returning `ErrIntentStoreAlreadyOpen` if the same file path is opened concurrently.
4. **CAS Revision Safety:** Updates to existing intents enforce strict Compare-and-Swap (CAS) revision increments (`incoming.Revision == existing.Revision + 1`).
5. **Authoritative Scope Intent Selection:** When classifying a scope, non-terminal intents (`PENDING`, `ACTIVE`, `RELEASING`) take precedence over historical `TERMINAL` intents. Multiple non-terminal intents trigger `MANUAL_INTERVENTION_REQUIRED`.
6. **Explicit Observed vs Final Status:** `ReconciliationItem` tracks both `ObservedStatus` (initial snapshot observation) and `FinalStatus` (post-remediation state).
7. **Stale-Safe Compare-Before-Act Auto-Remediation:** Before releasing an orphaned backend lease, the engine re-verifies backend and intent state immediately prior to executing the release using a detached, bounded context (`RemediationTimeout`) and `LEASE_RECONCILIATION_ORPHAN_CLEANUP` reason code. Release success is confirmed via a post-release backend query before reporting `SUCCEEDED` and setting `FinalStatus = released`.
8. **Strict 3-Phase Execution Model:** Reconciler execution is strictly partitioned into Phase 1 (Snapshot), Phase 2 (Deterministic Analysis), and Phase 3 (Bounded Remediation).
9. **Mandatory Startup Gate Enforcement:** Production lease controllers wrap lease acquisition with persistent `LeaseIntent` lifecycle tracking (`PENDING` $\to$ `ACTIVE` $\to$ `RELEASING` $\to$ `TERMINAL`) and enforce a mandatory `StartupGate` (`GatePending` $\to$ `GateAccepted` / `GateRefused`). No lease may be acquired until startup reconciliation passes cleanly.
10. **Composite Aggregate Recovery:** Multi-resource composite leases with incomplete or missing member leases are evaluated at the aggregate level and marked with status `CompositeStatusBroken` (`"broken"`).
11. **Reason Code Matrix:** All reason codes are formally documented and audited in [lease_reason_matrix.md](file:///Users/manuel/StudioProjects/xg2g/backend/docs/ADR/lease_reason_matrix.md).

## Consequences

- Process restarts can safely reconcile persistent intents against backend state without corrupting active resources.
- Mandatory startup gate guarantees zero lease issuance prior to clean reconciliation.
- Fixpoint idempotence guarantees running reconciliation repeatedly after remediation produces identical reports with zero side-effects.
- Resource leaks are reliably surfaced for audit or auto-remediated safely without false positive deletions.
- Manual intervention is explicitly requested when ambiguous conflicts or broken composites are detected rather than silently overwriting state.
