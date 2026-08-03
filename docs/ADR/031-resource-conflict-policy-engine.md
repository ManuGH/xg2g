# ADR-031: Resource Conflict Policy Engine

- **Status:** Active (Step E1 Domain Specification)
- **Date:** 2026-08-02
- **Supersedes:** None (formalizes ADR-029 preemption and conflict resolution policy)

## Context

In `xg2g`, hardware tuners, demuxers, hardware transcode sessions (NVENC / VA-API), and storage I/O bandwidth are finite shared resources. When resource capacity is exhausted, concurrent subsystem requests must be evaluated deterministically.

Previously, Phase D established a central persistent lease model (`ADR-029` / `ADR-030`) with atomic startup gates and reconciliation. Phase E introduces the **Policy & Preemption Engine (ADR-031)** to govern resource competition and protect critical workloads (e.g. scheduled recordings).

To prevent ad-hoc priority comparisons and ensure long-term maintainability, the Policy Engine must enforce an explicit, multi-dimensional Conflict Matrix rather than a scalar numerical priority score.

## Decision

1. **Explicit 36-Rule Conflict Matrix over Linear Priority Scores:**
   Resource conflicts are evaluated against an explicit, immutable $6 \times 6$ decision matrix (`ConflictMatrix[existingConsumer][incomingConsumer]`). Rules are defined statically for every pair of consumer types. Numerical weights are prohibited from determining conflict permissibility.

2. **Consumer Classification & Hierarchy:**
   Subsystem consumers are classified into six discrete operational types:
   - `ConsumerScheduledRecording`: Automated DVR timer recordings (Sacrosanct).
   - `ConsumerManualRecording`: User-initiated immediate recordings.
   - `ConsumerLiveTV`: Interactive real-time user viewing sessions.
   - `ConsumerRetroDVR`: Ring-buffer segment commit tasks.
   - `ConsumerChannelScan`: EPG and transponder background scanning.
   - `ConsumerBackgroundTransfer`: Non-realtime media sync or export.

3. **Explicit Resource Kind & Discrete Pool Model:**
   Step E1 supports discrete exclusive resource pools (`ResourceTuner`, `ResourceDemuxer`, `ResourceEncoderSlot`) with single-allocation-per-scope semantics. Shared quantitative resources (e.g. `ResourceStorageIO` bandwidth/IOPS) require a separate capacity model and return `ErrUnsupportedResourceModel` in Step E1.
   
   A consumer resource mapping (`RequiresResource(consumer, resourceKind)`) enforces that non-tuner workloads (e.g. `ConsumerBackgroundTransfer`) do not compete for tuner capacity.

4. **Scope Selection Mode & Exact Target Semantics:**
   Evaluation requests specify `ScopeSelectionMode` (`ScopeSelectionAnyCompatible` or `ScopeSelectionExact`). If `req.TargetScope` is non-empty or mode is `EXACT`, the engine evaluates ONLY the candidate and active allocation matching `req.TargetScope`. Deflection to another tuner slot is strictly forbidden.

5. **Typed Resource Snapshot Contract:**
   The pure domain engine receives a pre-built, typed `ResourceSnapshot` containing capacity, candidate capabilities, and active allocations. The domain engine performs no hardware calls, openwebif requests, or I/O.

6. **Pure Stateless Decision Engine:**
   `PolicyEngine.Evaluate(req EvaluationRequest, snapshot ResourceSnapshot)` is a pure, stateless function.
   - It requires an explicit timestamp (`req.EvaluatedAt`). Calling `time.Now()` internally is strictly forbidden.
   - For identical inputs, it produces 100% byte-for-byte identical results.

7. **Strict Separation of Decision and Execution:**
   The domain package (`internal/domain/policy`) contains only decision outcomes (`DecisionGrant`, `DecisionReject`, `DecisionPreemptionRequired`) and policy reason codes (`POLICY_*` registered in [`REASON_CODES.md`](../guides/REASON_CODES.md) and [`POLICY_REASON_CODES.md`](../guides/POLICY_REASON_CODES.md)). Preemption eviction execution (`PREEMPTION_EVICTION_*`) is isolated in the pipeline orchestration layer (`internal/pipeline/policy`).

8. **Step E2 Audit-Only Integration Sequence:**
   In Step E2, policy evaluation occurs **ONLY** upon an authoritative tuner slot conflict (`ErrScopeConflict`) during session orchestrator tuner lease acquisition.
   - **Preemption Mode:** `XG2G_POLICY_PREEMPTION_MODE` supports `disabled` (default) and `audit-only`. Mode `enforce` or invalid modes trigger startup error.
   - **Bypass in `disabled` Mode:** When mode is `disabled`, zero candidate/allocation reads, policy calls, or audit emissions take place.
   - **Conflict Path Trigger:** On tuner acquire conflict in `audit-only` mode:
     1. Build typed snapshot using separate `CandidateProvider` (hardware scopes) and `AllocationProvider` (active allocations with explicit `policy.ConsumerType` metadata).
     2. Calculate canonical deterministic `SnapshotRevision` hash.
     3. Evaluate `PolicyEngine.Evaluate()`.
     4. Emit `EvaluationAuditEvent` (`EnforcementMode = "audit-only"`).
     5. Log technical errors structurally if snapshot building, evaluation, or audit emission fails (emits `POLICY_EVALUATION_FAILED`).
     6. Return original `ErrScopeConflict` **UNCHANGED**.
   - **ZERO Side Effects:** Step E2 executes **ZERO** preemption, lease release, session revocation, or intent mutation (`Release = 0`, `Revoke = 0`, `Stop = 0`).

8. **Sacrosanct Non-Preemption Invariants:**
   - Active `ConsumerScheduledRecording` allocations are sacrosanct and cannot be preempted by any consumer.
   - Allocations marked `IsSacrosanct == true` or `IsReleasing == true` are protected against preemption.

9. **Deterministic Preemption Candidate Tie-Breaking:**
   If a matrix match permits preemption and multiple active allocations are eligible, candidate selection follows a strict 3-tier tie-breaker:
   1. Lowest `LossClass` (Consumer priority rank)
   2. Oldest `AcquiredAt` timestamp
   3. Lexicographical `AllocationID` order

## Alternatives Considered

- **Scalar Priority Numerical Comparison:** Rejected because simple integer comparisons hide domain invariants and prevent multi-dimensional rule governance.
- **Dynamic Loss Cost Formula in Domain:** Postponed to a future step to ensure Step E1 establishes an auditable, static matrix baseline first.

## Consequences

### Positive
- Prevents scheduled recording interruptions from background activities.
- Provides 100% deterministic, auditable decisions with zero side effects.
- Establishes a clean architectural boundary between decision domain and execution pipeline.

### Negative
- Requires maintaining explicit matrix entries when new consumer types are introduced.
