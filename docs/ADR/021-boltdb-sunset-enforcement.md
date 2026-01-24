# ADR-021: BoltDB/BadgerDB Sunset Policy and Enforcement

## Status

**Accepted** (Phase 2.3)
Supersedes: ADR-020 implementation guidance
Enforcement: Immediate (CI gates active)

---

## Context

### Problem Statement

ADR-020 established SQLite as "Single Durable Truth" and deprecated BoltDB/BadgerDB. However, as of Phase 2.3:

**Current State (Broken):**
```
CI gate: scripts/ci_gate_storage_purity.sh FAILS
Violations:
  ❌ internal/domain/session/store/bolt_store.go (production code)
  ❌ internal/domain/session/store/badger_store.go (production code)
  ❌ internal/pipeline/resume/store.go (imports bolt)
  ❌ internal/migration/* (migration tooling, wrongly flagged)
  ✓ cmd/xg2g-migrate (migration tooling, correctly exempted)
```

**Governance Gap:**
- ADR-020 declared deprecation but did NOT define enforcement mechanisms
- No timeline for removal
- No mechanical gates preventing bolt/badger re-introduction
- Mixed signals: factory.go supports `XG2G_STORAGE=bolt` but gates forbid it
- Migration tooling exemptions incomplete

**Damage:**
- Split-brain risk (dual durable backends)
- Operator confusion (which backend is truth?)
- Non-hermetic tests (backend-dependent behavior)
- CI gate failure normalized (broken windows)
- Accidental dual-write scenarios

---

## Decision

**Option Selected: A1 - Hard Sunset with Migration Grace Period**

### System Assumption

**SQLite is the ONLY durable truth.**
Legacy backends (BoltDB, BadgerDB) exist ONLY in migration tooling and MUST NOT be reachable in production runtime.

### Enforcement Policy

1. **Production Code: ZERO TOLERANCE**
   - `internal/` packages (except migration) MUST NOT import bolt/badger
   - `cmd/` binaries (except xg2g-migrate) MUST NOT import bolt/badger
   - `factory.go` MUST NOT instantiate bolt/badger stores in production mode

2. **Migration Tooling: EXPLICITLY ALLOWED**
   - `cmd/xg2g-migrate` MAY import bolt/badger (read-only, migration source)
   - `internal/migration/` MAY import bolt/badger (read-only, migration logic)

3. **Test Code: CONDITIONAL ALLOWANCE**
   - `*_test.go` MAY import bolt/badger ONLY for migration/compatibility tests
   - NOT for general unit testing (use memory store or sqlite)

---

## Mechanical Gates

### Gate 1: Import Purity (CI Enforcement)

**File:** `scripts/ci_gate_storage_purity.sh`

**Rule:**
Forbidden imports: `go.etcd.io/bbolt`, `github.com/dgraph-io/badger/v4`

**Exceptions (exhaustive list):**
```bash
EXCEPTIONS=(
    "internal/migration"      # Migration read logic
    "cmd/xg2g-migrate"        # Migration CLI tool
)
```

**Enforcement:**
```bash
# MUST pass on every PR
make verify  # includes ci_gate_storage_purity.sh
```

**Exit Code:**
Non-zero if ANY production code violates

---

### Gate 2: Factory Initialization Block

**File:** `internal/domain/session/store/factory.go`

**Rule:**
```go
switch backend {
case "bolt", "badger":
    return nil, fmt.Errorf("DEPRECATED: BoltDB/BadgerDB removed. Use 'sqlite' or migrate with xg2g-migrate")
case "sqlite":
    return NewSqliteStore(path)
case "memory":
    return NewMemoryStore(), nil  // Ephemeral only
default:
    return nil, fmt.Errorf("unknown backend: %s (supported: sqlite, memory)", backend)
}
```

**Effect:**
Runtime-enforced: bolt/badger initialization always fails in production binaries.

---

### Gate 3: Build Tag Isolation (Optional Hardening)

**Future Enhancement (not required for Phase 2.3):**

```go
// +build !production

package store

// bolt_store.go, badger_store.go only compile with migration tag
```

Deferred: Adds complexity, runtime gate (Gate 2) is sufficient.

---

### Gate 4: Env Var Validation

**Removed:** `XG2G_STORAGE=bolt` is NO LONGER VALID.

**Allowed values:**
```
XG2G_STORAGE=sqlite       # Default (if unset)
XG2G_STORAGE=memory       # Ephemeral (testing/dev only)
```

**Migration Mode (xg2g-migrate only):**
```
XG2G_MIGRATION_MODE=true  # Allows bolt READ for migration
```

Production daemon MUST NOT honor `XG2G_MIGRATION_MODE`.

---

## Timeline and Removal Plan

### Phase 2.3 (Immediate - This ADR)

**Actions:**
1. ✅ Update `ci_gate_storage_purity.sh` exceptions to include `internal/migration`
2. ✅ Update `factory.go` to reject bolt/badger backends
3. ✅ Document that `XG2G_STORAGE=bolt` is invalid
4. ✅ CI gate must pass on all PRs

**DoD:** CI green, production code zero violations

---

### Phase 2.4 (Q1 2026 - Cleanup)

**Actions:**
1. Remove `bolt_store.go`, `badger_store.go` from `internal/domain/session/store/`
2. Move bolt migration logic entirely to `cmd/xg2g-migrate/` (consolidate)
3. Remove bolt imports from `internal/pipeline/resume/store.go`
4. Add deprecation notice to README: "Bolt/Badger no longer supported, use xg2g-migrate"

**Gate:** No code outside `cmd/xg2g-migrate/` may import bolt/badger

---

### Phase 3.0 (Q2 2026 - Migration Tooling Deprecation)

**Assumption:** 95%+ users migrated (6 months notice)

**Actions:**
1. Remove `cmd/xg2g-migrate` binary (archive in docs/archive/)
2. Remove `internal/migration/` package entirely
3. Update CI gate to forbid bolt/badger imports EVERYWHERE (no exceptions)

**DoD:** Zero bolt/badger code in repository

---

## Valid Configurations

### Production (Daemon)

```bash
# Default (recommended)
XG2G_STORAGE=sqlite
xg2g daemon

# Ephemeral (dev/testing only)
XG2G_STORAGE=memory
xg2g daemon --dev
```

### Migration (One-Time)

```bash
# Migrate from Bolt to SQLite
XG2G_MIGRATION_MODE=true xg2g-migrate \
  --from bolt \
  --from-path /data/xg2g.db \
  --to sqlite \
  --to-path /data/xg2g.sqlite
```

### Invalid (Rejected)

```bash
# ❌ FAILS at runtime (factory.go Gate 2)
XG2G_STORAGE=bolt xg2g daemon
→ Error: "DEPRECATED: BoltDB removed. Use 'sqlite' or migrate"

# ❌ FAILS at CI (Gate 1)
import bolt "go.etcd.io/bbolt"  // in production code
→ Error: "Storage purity check failed"
```

---

## Consequences

### Positive

- **Single Durable Truth Enforced:** No split-brain scenarios
- **Mechanical Enforcement:** Gates prevent accidental violations
- **Clear Migration Path:** `xg2g-migrate` documented and supported (6 months)
- **Simplified Testing:** No backend-dependent test variations
- **Binary Size:** Bolt/Badger deps removed from production binary
- **Operator Clarity:** Unambiguous storage backend (sqlite only)

### Negative

- **Migration Effort:** Users on bolt must run xg2g-migrate (one-time)
- **Breaking Change:** `XG2G_STORAGE=bolt` no longer works (deprecated warning added)
- **Cleanup Debt:** bolt_store.go remains until Phase 2.4 (gated, unreachable)

### Mitigation

- **6-Month Notice:** Phase 3.0 removal gives ample migration time
- **Migration Tool:** `xg2g-migrate` is restart-safe, idempotent, verifiable
- **Documentation:** README, CHANGELOG, and operator guide updated

---

## Compliance Checklist

### For Developers

Before merging ANY PR touching storage:

- [ ] `make verify` passes (includes ci_gate_storage_purity.sh)
- [ ] No imports of `go.etcd.io/bbolt` or `badger/v4` outside exempted paths
- [ ] `factory.go` does NOT instantiate bolt/badger stores
- [ ] Tests use `memory` or `sqlite` stores (NOT bolt)

### For Operators

Before upgrading to Phase 2.3+:

- [ ] Run `xg2g-migrate` if using BoltDB (one-time, before upgrade)
- [ ] Remove `XG2G_STORAGE=bolt` from config/env (use `sqlite` or omit)
- [ ] Verify `xg2g daemon` starts with sqlite backend
- [ ] Backup existing data before migration

---

## Implementation Tracking

**PR-1: Enforcement (Immediate)**
- Update `ci_gate_storage_purity.sh` exceptions
- Update `factory.go` to reject bolt/badger
- Add tests verifying factory rejects bolt
- Update README with deprecation notice

**PR-2: Cleanup (Phase 2.4)**
- Remove `bolt_store.go`, `badger_store.go`
- Consolidate bolt logic into `cmd/xg2g-migrate/`
- Remove bolt imports from `internal/pipeline/`

**PR-3: Sunset (Phase 3.0)**
- Remove `cmd/xg2g-migrate/`
- Remove `internal/migration/`
- Update CI gate to zero exceptions

---

## References

- **ADR-020:** Storage Strategy 2026 (SQLite as Durable Truth)
- **Migration Tool:** `cmd/xg2g-migrate/main.go`
- **CI Gate:** `scripts/ci_gate_storage_purity.sh`
- **Factory:** `internal/domain/session/store/factory.go`
- **Storage Invariants:** `docs/ops/STORAGE_INVARIANTS.md`

---

## Decision Record

**Decided By:** Engineering Lead
**Date:** 2026-01-24
**Reviewers:** [Pending]
**Status:** Accepted (awaiting PR-1 implementation)

---

## Appendix A: Migration Example

```bash
# 1. Backup current Bolt database
cp /data/xg2g.db /data/xg2g.db.backup

# 2. Run migration (idempotent, restart-safe)
XG2G_MIGRATION_MODE=true xg2g-migrate \
  --from bolt \
  --from-path /data/xg2g.db \
  --to sqlite \
  --to-path /data/xg2g.sqlite \
  --verify

# 3. Verify record counts match
xg2g-migrate --verify-only \
  --from bolt --from-path /data/xg2g.db \
  --to sqlite --to-path /data/xg2g.sqlite

# 4. Update config/env
# OLD: XG2G_STORAGE=bolt
# NEW: XG2G_STORAGE=sqlite (or omit, defaults to sqlite)

# 5. Restart daemon
systemctl restart xg2g

# 6. Verify daemon started with sqlite
journalctl -u xg2g | grep "storage backend: sqlite"
```

---

## Appendix B: Gate Failure Examples

### Example 1: Production Code Violates

```bash
$ make verify
🔍 Checking Storage Purity...
❌ VIOLATION: Forbidden import 'go.etcd.io/bbolt' found:
internal/domain/session/store/bolt_store.go:12: bolt "go.etcd.io/bbolt"
🚨 FAIL: Storage purity check failed.
```

**Fix:** Move bolt_store.go to cmd/xg2g-migrate/ or delete it.

---

### Example 2: Factory Runtime Rejection

```go
// User config: XG2G_STORAGE=bolt
store, err := OpenStateStore("bolt", "/data/xg2g.db")
// Returns: error("DEPRECATED: BoltDB removed. Use 'sqlite' or migrate")
```

**Fix:** Update config to `XG2G_STORAGE=sqlite` and run xg2g-migrate.

---

## Appendix C: Exception Justification

**Why `internal/migration/` is exempted:**

Migration code MUST read from bolt/badger sources to perform data migration. This is:
- Read-only (no dual-write risk)
- Temporary (removed in Phase 3.0)
- Isolated (not imported by production code)
- Gated (only runs with XG2G_MIGRATION_MODE=true)

**Why `cmd/xg2g-migrate/` is exempted:**

Migration CLI is a separate binary, not linked into production daemon. Users explicitly invoke it for one-time migration.
