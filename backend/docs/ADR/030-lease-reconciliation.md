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
2. **Persistence Across Restarts & Crash Durability:** Intents are persisted to non-volatile JSON storage (`FileIntentStore`) using atomic file replacement (`.tmp` write, `fsync`, `f.Close()`, `os.Rename`, parent directory `fsync`).
3. **Single-Writer Process Boundary:** `FileIntentStore` guarantees thread-safety within a single process via `sync.RWMutex`. Multi-process concurrency requires explicit single-writer ownership per file path or an underlying relational store.
4. **CAS Revision Safety:** Updates to existing intents enforce strict Compare-and-Swap (CAS) revision increments (`incoming.Revision == existing.Revision + 1`).
5. **Deterministic 5-State Classification Model:** Every inspected resource scope/intent pair is categorized into exactly one of:
   - `confirmed`: Active intent matches active backend lease (`LeaseID`, `Owner`, `Scope`).
   - `released`: Both intent and backend agree the lease is released or expired.
   - `orphaned`: Backend holds an active lease for a scope, but no matching active intent exists in the Intent Store.
   - `missing`: Intent expects an active lease, but the backend does not hold an active lease for the scope.
   - `manual-intervention-required`: Ambiguous conflicts exist (e.g. duplicate active intents for the same scope, duplicate backend leases, owner/ID mismatches).
6. **Stale-Safe Compare-Before-Act Auto-Remediation:** Before releasing an orphaned backend lease, the engine re-verifies backend and intent state immediately prior to executing the release using a detached, bounded context (`RemediationTimeout`) and `LEASE_RECONCILIATION_ORPHAN_CLEANUP` reason code. Release success is confirmed via a post-release backend query before reporting `SUCCEEDED`.
7. **Deterministic Output & Composite Visibility:** Output items and summary reports are sorted deterministically (`Scope` $\to$ `IntentID` $\to$ `BackendID` $\to$ `Status` $\to$ `ReasonCode`). Composite leases are aggregated into composite status summaries.

## Consequences

- Process restarts can safely reconcile persistent intents against backend state without corrupting active resources.
- Resource leaks are reliably surfaced for audit or auto-remediated safely without false positive deletions.
- Manual intervention is explicitly requested when ambiguous conflicts are detected rather than silently overwriting state.
