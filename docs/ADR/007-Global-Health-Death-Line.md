# ADR-SRE-001: Global Health & The Death Line

## Status

Accepted (2026-01-06)

## Context

xg2g executes long-running, resource-intensive jobs (VOD, Remux, DVR). Without strict termination rules, "hopeless" or pathological jobs lead to node instability, cascading timeouts, and unpredictable behavior for all users. We must define the threshold where user work is intentionally sacrificed to protect system integrity.

## Decision

### A. Latenz-Death-Line (Time Budget Exhausted)

Every job has a finite time budget.

- **Rule**: Jobs that exceed their allocated time budget (e.g., 3x expected duration or a fixed maximum cap) are terminated immediately.
- **Result**: No retries for expired deadlines.
- **Error Code**: `JOB_TIMEOUT_DEADLINE_EXCEEDED`.
- **Reasoning**: A job that hangs or stalls is likely symptomatic of an underlying I/O or media pathology. Letting it continue only increases resource pressure.

### B. Ressourcen-Death-Line (Node First, Job Second)

The stability of the computing node always takes precedence over individual jobs.

- **Rule**: If a node hits critical resource watermarks (e.g., RAM OOM signal, Disk > 90%), non-critical background jobs are killed immediately.
- **Grace**: No grace period, no attempt to "save" partially written output.
- **Dogma**: Jobs are replaceable; nodes are not.

### C. Circuit-Breaker-Death-Line (Queue Protection)

We protect the system from queue explosions by failing fast.

- **Rule**: When circuit breakers switch to "Open" (Red), new requests are failed-closed immediately.
- **Result**: HTTP 503 with a structured error payload.
- **Reasoning**: Waiting without a guarantee is worse UX than a clear error. This prevents service-level cascading failures.

### D. Zombie-Detection (Unkillable Work)

Work that refuses to be stopped is considered a defect.

- **Rule**: Every job must respond to `ctx.Done()`.
- **Consequence**: If a process exceeds its cleanup timeout after cancellation, it is flagged as a zombie, and the node/container is marked for recycle.
- **Alignment**: This enforces ADR-GO-001 (Deterministic Exit).

## Consequences

### Positive

- Predictable behavior under high load.
- Minimal blast radius for pathological inputs.
- Faster Recovery (MTTR) due to fail-fast semantics.
- Authoritative and honest error reporting to clients.

### Negative (Consciously Accepted)

- Users may lose progress on long-running jobs if the system is stressed.
- No "Best Effort" mode for corrupted inputs that cause stalls.
- Reduced "magic recovery" logic in favor of deterministic failure.

## Dogma

A stable system with clear errors is superior to an unstable system with hope.
