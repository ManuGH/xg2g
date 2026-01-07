# ADR-GO-001: Concurrency Manifest

## Status

Accepted (2026-01-06)

## Context

Concurrent programming in Go is highly effective but becomes exponentially harder to debug as the system grows. This manifest establishes the formal rules for concurrency in xg2g to prevent race conditions, deadlocks, and "Heisenbugs".

## Decision

### 1. Besitzpflicht (Ownership)

Every goroutine must have a clear owner and a strictly defined lifecycle.

- **Rule**: "Fire-and-forget" goroutines are prohibited.
- **Requirement**: The owner must be able to cancel the goroutine and verify its termination (via `ctx.Done()` or WaitGroups).

### 2. Mutex-Disziplin (Data Only)

Mutexes protect data, never processes or logic.

- **Prohibited within a Lock**:
  - I/O operations (file, network).
  - `exec.Command` or external process execution.
  - Arbitrary sleeps or waiting on other channels.
- **Rule**: Locks must be short-lived, local, and deterministic. Complexity inside a lock is a design failure.

### 3. Fehler-Symmetrie (Error Handling)

The component starting the work is responsible for its outcome.

- **Rule**: Goroutines must report results and errors back to the owner via thread-safe mechanisms.
- **Requirement**: Use unexported fields Protected by mutexes and exposed via getters (e.g., `Run.Error()`, `Run.Wait()`). Direct access to mutable result fields is prohibited.

### 4. Keine globalen Zustände (No Global State)

Concurrency must be manageable via instances, not package-level variables.

- **Prohibited**: Package-level Maps, global variables used for coordination.
- **Rule**: State must live in clearly named owner types (Manager, Store, etc.) and be injected via interfaces.

### 5. Deterministische Beendigung (Deterministic Exit)

Work that cannot be stopped is a bug.

- **Requirement**: Every goroutine must monitor its context (`ctx.Done()`) and guarantee cleanup on exit.
- **Rule**: "Sometimes hangs" is a catastrophic failure and a release blocker.

## Consequences

- **Positive**: Thread-safety is guaranteed by design. Massive reduction in rare, hard-to-reproduce bugs.
- **Negative**: Requires more boilerplate (Wait functions, getters/setters) and explicit lifecycle management.
