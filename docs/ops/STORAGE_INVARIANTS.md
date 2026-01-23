# Storage Invariants

To maintain "Operator-Grade" stability and data integrity, all storage
operations in xg2g must adhere to these invariants.

## 1. Single Durable Truth (SDT)

- **Invariant**: At any point in time, there is exactly ONE durable store
  for a given entity type (e.g., Sessions).
- **Prohibition**: Dual-writing (writing to two durable stores) is forbidden.
- **Shadow Implementation**: "Shadow" stores MUST be unreachable from the
  production factory and MUST NOT perform dual-writes to durable media.

## 2. No Ephemeral Escalation

- **Invariant**: Data requiring durability must never be stored exclusively
  in a volatile cache (e.g., Memory or Redis).
- **Graceful Degradation**: If a cache is unavailable, the system MUST
  fallback to the Durable Truth (SQLite).

## 3. SQLite Configuration

All SQLite instances MUST use:

- `PRAGMA journal_mode=WAL`.
- `PRAGMA synchronous=NORMAL`.
- `PRAGMA busy_timeout=5000`.
- `PRAGMA foreign_keys=ON`.

### Connection Handling

- **Writer**: Exactly ONE writer connection per database file.
- **Reader Pool**: Multiple reader connections allowed (via WAL).
- **Retention**: Connections should be pooled but limited to prevent
  file-handle exhaustion in multi-instance environments.

## 4. Schema Evolution

- **Invariant**: All durable stores MUST implement schema versioning.
- **Mechanism**: Use `PRAGMA user_version`.
- **Fail-Closed**: If a migration fails, the system MUST stop the line.

## 5. Hermeticity and Purity

- **Invariant**: Only approved storage engines (SQLite) may be imported
  into the shipping dependency graph of module `internal/`.
- **Enforcement**: Validated by `scripts/ci_gate_storage_purity.sh`.
