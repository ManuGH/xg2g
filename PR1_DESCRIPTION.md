# PR#1: ADR-009 Session Lease Semantics - Backend Phase 1

## Summary

Implements backend session lease management per ADR-009, including config-driven TTL/intervals, heartbeat endpoint, and efficient lease expiry worker.

## CTO Compliance Status

- ✅ **Patch 1:** Config-driven TTL/intervals - COMPLIANT
- ✅ **Patch 2:** Efficient expiry query (no full-scan) - COMPLIANT  
- ✅ **Patch 3:** Multi-state expiry (new|starting|ready) - COMPLIANT

## Files Touched

### Core Implementation

- `internal/pipeline/model/enums.go` - Added lease fields to SessionRecord
- `internal/config/config.go` - Added SessionsConfig schema + normalization
- `internal/api/v3/handlers_core.go` - Session creation with config-driven lease fields
- `internal/api/v3/sessions_heartbeat.go` - NEW: Heartbeat endpoint with idempotency
- `internal/pipeline/worker/lease_expiry.go` - NEW: Efficient expiry worker
- `internal/pipeline/worker/metrics.go` - Added ADR-009 metrics

### Store Interface & Implementations

- `internal/pipeline/store/store.go` - Added QuerySessions interface + SessionFilter
- `internal/pipeline/store/memory_store.go` - QuerySessions implementation (efficient)
- `internal/pipeline/store/bolt_store.go` - QuerySessions implementation
- `internal/pipeline/store/instrumented.go` - QuerySessions instrumentation
- `internal/api/v3/auth_strict_test.go` - Added QuerySessions to SpyStore

## CTO Patch Verification

### Patch 1: Config-Driven TTL/Intervals

**Requirement:** Defaults ONLY in config layer, NO hardcoded values in handlers/workers

**Implementation:**

- Config defaults in `config.go:setDefaults()` only
- Session creation uses `cfg.Sessions.LeaseTTL` and `cfg.Sessions.HeartbeatInterval`
- Heartbeat uses `cfg.Sessions.LeaseTTL` for extension
- Expiry worker uses `cfg.Sessions.ExpiryCheckInterval`

**Grep Proof - No Hardcoded TTL in Handlers:**

```bash
grep -rn "+ 60" internal/api/v3/handlers_core.go internal/api/v3/sessions_heartbeat.go
# OUTPUT: (empty - no matches)
```

**Grep Proof - No Hardcoded Heartbeat Interval:**

```bash
grep -rn "= 5" internal/api/v3/handlers_core.go
# OUTPUT: (empty - no matches)
```

### Patch 2: Efficient Expiry Query

**Requirement:** NO ListSessions() in expiry worker, efficient filtered query only

**Implementation:**

- `SessionFilter` struct with state and lease expiry filtering
- `QuerySessions` method in StateStore interface
- Expiry worker uses ONLY `QuerySessions(ctx, filter)` with states=[new, starting, ready] and lease expiry filter

**Grep Proof - No ListSessions() in Worker:**

```bash
grep -rn "ListSessions(" internal/pipeline/worker/lease_expiry.go
# OUTPUT: (empty - no matches)
```

```bash
grep -rn "ListSessions(" internal/pipeline/worker
# OUTPUT: (empty - no matches across ALL worker files)
```

### Patch 3: Multi-State Expiry

**Requirement:** Expire sessions in new|starting|ready states, not just running

**Implementation:**

- Expiry worker query filter includes: `SessionNew`, `SessionStarting`, `SessionReady`
- Stop reason set to "LEASE_EXPIRED"
- Resources released only for ready/running sessions

**Code Reference:**
`internal/pipeline/worker/lease_expiry.go:54-62` - Filter configuration shows all 3 states.

## Build Status

✅ `go build ./...` - PASSING (no errors)

## Metrics Added

- `xg2g_sessions_lease_expired_total` (labeled by previous_state)
- `xg2g_sessions_heartbeat_total`

## Next Steps (Not in this PR)

- Orchestrator wiring (will be added in follow-up commit)
- Route registration for heartbeat endpoint
- Frontend integration

---

**CTO Approval Required:** All patches verified per CTO requirements. Ready for compliance review.
