# Admission control: Try-acquire lease in control plane and return 409 (no session) on R_LEASE_BUSY

## Problem statement

Current behavior creates a terminal session on contention. This is correct but inflates the failure surface and creates synthetic artifacts that look like faults.

## Goal

If the lease cannot be atomically acquired at intent time, return HTTP 409 with `R_LEASE_BUSY` and do not create a session.

## Non-goals

- No worker refactor in this issue.
- No queueing.
- No new session states.

## Design constraint

Acquisition must be an atomic try-acquire, not a probe, to avoid TOCTOU.

## Acceptance criteria

- Contention returns HTTP 409 with a deterministic error body (`requestId`, `reason`, optional `retryAfterMs`).
- No session is created for lease contention.
- Existing behavior remains available behind a feature flag until proven stable (optional).

## Why deferred

This requires shared lease-manager participation in the control plane or a safe handoff across the control/worker boundary, which is higher refactor risk than PR3.
