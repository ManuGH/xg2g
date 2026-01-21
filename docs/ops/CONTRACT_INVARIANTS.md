# Operational Invariants (Phase 5.3)

## 1. Single-Worker Lease Constraint

**Invariant**: Single-writer access is ENFORCED via `system:orchestrator:guard_lock`.

### Guard Lease Specification (FROZEN)

- **Key**: `system:orchestrator:guard_lock` (Exact string match required).
- **Owner**: Must be unique per running instance (e.g., `hostname:pid` or UUID).
- **TTL**: Must be significantly greater than heartbeat interval (e.g., TTL 15s / Heartbeat 5s) to prevent flapping.

### Enforcement

1. **Startup Gate**: `Orchestrator.Run()` MUST acquire or verify ownership of the guard lock before any logic execution.
2. **Ambiguity Fatal**: If guard acquisition returns `false` and ownership cannot be proven (e.g., store error or ambiguous state), the service MUST exit (FATAL).
3. **Wipe Protection**: `DeleteAllLeases` (Wipe) is ONLY permitted after verifying guard ownership.
4. **Fail-Closed Maintenance**: A background loop renews the guard lease. If renewal fails (store error) or lease is lost (stolen), the service MUST terminate immediately (FAIL-CLOSED) to prevent split-brain.
5. **Shutdown**: Best-effort `ReleaseLease` on exit to facilitate fast failover.

**Consequence**: Restarting the service flushes all lease locks (`DeleteAllLeases`) only after proving guard ownership.
**Risk**: Split-brain is prevented by the strict startup gate and runtime heartbeat.

## 2. Tuner Metric Truth

**Invariant**: `xg2g_tuners_in_use` reflects the count of unique tuner slots held by valid leases owned by non-terminal sessions.
**Enforcement**: `Orchestrator.reconcileTunerMetrics` validates:

- Session claims slot X (ContextData).
- Lease X exists and is owned by SessionID.
- Slot X is counted at most once (deduplicated).
**Violation**: Any duplicate claim or lease mismatch triggers `xg2g_invariant_violation_total` and an operational warning log.

## 3. Terminal Session 410 Gone

**Invariant**: Accessing a session in `STOPPED`, `FAILED`, or `CANCELLED` state via API v3 returns `410 Gone`.
**Enforcement**: `handleV3SessionState` checks `session.State.IsTerminal()`.
**Body**: Returns `application/problem+json` with `requestId` for debugging.
