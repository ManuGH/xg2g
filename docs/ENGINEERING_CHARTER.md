# xg2g Engineering Charter & Governance

**Status:** Active  
**Version:** 1.0.0  
**Applies to:** All source code, tests, and architecture decisions that can affect one or more engineering invariants.  
**Owner:** Architecture Maintainers  
**Last Updated:** 2026-08-02  

---

> **Definition Invariant:** An invariant is a system property that must be preserved permanently, regardless of implementation details or refactorings.

> **System-wide Scope:** Invariants apply system-wide. A component must not achieve local correctness by violating an invariant in another subsystem.

---

### 1. Document Hierarchy & ADR Relationship

This Charter defines the governing engineering principles of the project. Changes to these principles are intentionally rare and must follow the governance process defined in this document.

Architectural decisions that operationalize these principles for specific components are documented as **ADRs (Architecture Decision Records)**.

```
Engineering Charter
    ↓
Architecture Decision Records (ADRs)
    ↓
Implementation
```

---

### 2. Truth Models

* **Persisted Truth (Durable / Survives Restarts):**
  * Recording Intent & scheduled DVR timers
  * RecordingJob state & confirmed progress
  * Transfer targets & storage paths
  * Lease owner, scope, status, and expiration deadline (if reconciled post-restart)
  * Domain terminal errors & recovery metadata
* **Operational Truth (Transient / Re-derived on Startup):**
  * Active Go timers, timeout channels, & heartbeats
  * Receiver status & active demuxer allocations
  * Active OS processes (FFmpeg, PIDs)
  * Open file handles & stream buffers
  * Eviction states in the segment store

---

### 3. Control Flow

```
Intent
  ↓
Planner
  ↓
Policy Matrix
  ↓
Pre-Reservation
  ↓
Execution
  ↓
Commit / Compensation
  ↓
Lease & Reconciliation
```

---

### 4. The 8 Core Invariants

1. **Recording Protection:** Lower-priority, system-controlled activities must not prevent, preempt, or prematurely terminate a scheduled recording.
2. **Lease Safety:** Every lease must have an owner, scope, expiration time, and an idempotent release path. Orphaned leases are detected and cleaned up via reconciliation.
3. **Reader Protection:** An HLS segment must not be deleted as long as an active reader or valid reservation references it.
4. **Playlist Consistency:** Published playlists must not reference permanently removed segments; `Media Sequence` numbers must remain strictly monotonic.
5. **Recovery Safety:** Persisted intents are reconciled with external reality after a process restart before work is resumed or completed.
6. **Decision Explainability:** Every rejection, preemption, and path selection produces a stable domain Reason Code and separate technical diagnostic metadata.
7. **Bounded Failure:** Every external operation has explicit deadlines, cancellation support, bounded retry attempts, and a defined terminal state.
8. **No Partial Resource Leaks:** If a multi-step resource acquisition fails, previously acquired partial resources are released compensatorily or cleaned up via reconciliation.

---

### 5. Conflict Resolution Priority

When invariants conflict during runtime (e.g., *Reader Protection vs. Disk Space Emergency* or *Recovery Speed vs. Fast Startup*), decisions are made by an **explicitly documented Policy (ADR)**, never by ad-hoc code overrides. Any emergency fallback must be logged with a structured Reason Code.

---

### 6. Governance & Versioning

#### Document Versioning
* **Major (X.0.0):** Modification or removal of existing invariants or core governance rules affecting active ADRs.
* **Minor (1.X.0):** Addition of new invariants, governance rules, or mandatory processes.
* **Patch (1.0.X):** Editorial fixes, clarifications, or examples without structural impact.

#### Change Rules
* New invariants may be added.
* Existing invariants may only be modified or removed through an explicit Architectural Decision Record (ADR).
* Changes to this Charter or to the definition of existing invariants require an accompanying ADR.

---

### 7. Review & Merge Gate

Every Pull Request that impacts system behavior or an invariant must provide the following **Required Engineering Evidence**:

| Required Engineering Evidence | Expectation |
| :--- | :--- |
| **1. Invariant Impact** | Which existing invariant(s) does this change rely on, preserve, or affect? |
| **2. Failure Analysis** | What new failure modes, race conditions, or operational risks are introduced? |
| **3. Recovery & Compensation** | How is a failure detected, compensated, or reconciled? |
| **4. Durable Verification** | What automated mechanism (Unit, Integration, Contract, Race Test, CI Gate) permanently guards against regressions? |

---

### 8. Language Policy

To ensure consistency and accessibility for all contributors:
* All source code, identifiers, APIs, database schemas, configuration keys, and commit messages **must be written in English**.
* All ADRs, architecture documents, code comments, user-facing developer documentation, and pull request descriptions **should be written in English**.
* Existing non-English code or documentation should be migrated to English opportunistically when touched by functional changes. Avoid purely cosmetic translation-only refactorings.

---

### 9. Glossary

* **Lease:** A time-bounded, explicitly assigned usage authorization for a limited resource (e.g., Tuner, Encoder slot).
* **Pre-Reservation:** Temporary locking of a resource during a multi-step planning and acquisition workflow.
* **Reconciliation:** The process of aligning operational system state with persisted intent following restarts or network disconnections.
* **Policy Matrix:** Ruleset evaluating resource conflicts based on priority, context, lead time, and loss cost.
* **Reason Code:** A machine-readable, stable identifier explaining the domain cause of a decision. Technical error details are logged separately as diagnostic metadata and are not part of the stable Reason Code contract.

---

### 10. Non-Goals

This Charter intentionally does not prescribe implementation techniques, frameworks, dependency injection approaches, project layout, or design patterns beyond what is necessary to preserve the engineering invariants.

Those decisions belong to ADRs or implementation details as long as they preserve the engineering invariants defined by this Charter.

---

> ### **The Central Architectural Principle**
> *Implementations are replaceable. Invariants are the contract of the system. Every change is measured by whether it preserves this contract or consciously and traceably evolves it.*
