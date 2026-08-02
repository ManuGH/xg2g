# ADR-029: Resource Arbitration and Composite Lease Model

- **Status:** Active
- **Date:** 2026-08-02
- **Supersedes:** None (formalizes resource allocation rules)

## Context

In `xg2g`, hardware tuners, demuxers, hardware transcoder sessions (NVENC / VA-API), storage bandwidth, and network slots are finite, shared resources. Multiple concurrent subsystem workloads compete for these resources:
- Scheduled DVR Recordings (high reliability requirement)
- Live TV Playback Sessions (low latency requirement)
- Retro-DVR Capture / Transfers (background fill requirement)
- EPG & Channel Scans (periodic maintenance requirement)

Previously, resource allocation was performed implicitly across individual components (e.g. tuner checks in `enigma2`, transcode checks in `decision`, session creation in `control/http/v3`). This led to potential resource contention, orphaned tuner holds if transcoder setup failed, and lack of deterministic preemption rules.

Because external hardware (Enigma2 receivers, GPU encoders, filesystems) does not support global 2PC (Two-Phase Commit) transactions, resource acquisition across multiple heterogeneous sub-resources cannot be guaranteed in a single atomic database lock.

## Decision

We establish a centralized **Resource Arbitration Engine** based on **Composite Leases** with compensation and reconciliation:

1. **Policy Matrix over Linear Priority:**
   Resource conflicts are evaluated against a multi-dimensional Policy Matrix taking into account workload type, lead time, user context, and loss cost:
   - *Scheduled DVR vs. Live TV:* Seek alternative tuner/demuxer capacity first; if unavailable, apply preemption policy protecting the recording.
   - *Live TV vs. EPG Scan:* Pause or abort EPG scan immediately.
   - *Scheduled DVR vs. Background Retro-DVR:* Instantly evict Retro-DVR resources to guarantee DVR recording.

2. **Composite Lease Contract:**
   A single logical reservation (`CompositeLease`) encompasses all required sub-resources for a session (Tuner + Demuxer + Transcode Slot + Storage Budget).
   - Leases have an explicit `Owner`, `Scope`, `Expiration Deadline`, and an idempotent `Release()` method.

3. **Staged Acquisition with Compensation:**
   Acquisition follows a staged workflow:
   `Plan → Pre-Reservation → External Setup → Commit Lease`
   If setup of any sub-resource fails mid-flight (e.g. tuner acquired, but HW encoder session fails), all previously pre-reserved sub-resources are compensatorily released immediately.

4. **Reconciliation:**
   A background reconciliation loop continuously verifies active operational resources (running FFmpeg PIDs, open stream handles) against persisted lease deadlines to clean up orphaned leases caused by hard process crashes.

## Alternatives Considered

- **Global Distributed 2PC Lock:** Rejected because external Enigma2 boxes and GPU driver sessions do not support transactional rollback.
- **Linear Priority Counter:** Rejected because priority alone ignores lead times, user interaction context, and preemption gracefully-degrading capabilities.
- **Uncoordinated Local Allocation:** Rejected because local allocation leads to partial resource leaks and race conditions between concurrent playback and recording startup.

## Consequences

### Positive
- Prevents orphaned tuner locks and hardware encoder session leaks.
- Guarantees scheduled DVR recordings are protected against lower-priority background tasks.
- Produces clear, machine-readable Reason Codes on resource rejection.

### Negative
- Increases upfront planning overhead before stream startup.
- Requires components to implement explicit compensation callbacks.

## Charter Invariants Affected

- **Invariant 1 (Recording Protection):** Lower-priority activities cannot preempt or block scheduled DVR recordings.
- **Invariant 2 (Lease Safety):** All leases have explicit deadlines, owners, and idempotent release paths.
- **Invariant 8 (No Partial Resource Leaks):** Failed multi-step acquisitions trigger compensatory cleanup and background reconciliation.

## Verification

- **Unit & Contract Tests:** Verify Policy Matrix resolution across all workload combination matrix pairs.
- **Compensation Tests:** Simulate mid-flight encoder failure and verify tuner pre-reservation is released within timeout bounds.
- **Reconciliation & Race Tests:** Run `go test -race` simulating process restart during active lease execution.

## References

- [xg2g Engineering Charter](../ENGINEERING_CHARTER.md)
- [ADR-005: Architecture Invariants](005-Architecture-Invariants.md)
- [ADR-025: Playback Confidence and Runtime Policy Layer](025-playback-confidence-policy.md)
