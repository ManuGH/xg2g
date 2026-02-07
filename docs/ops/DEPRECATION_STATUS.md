# Deprecation Status (Best Practice Inventory)

## Purpose

Track active deprecations, live usage in code, and removal criteria. This is the
operational view used for release planning and enforcement.

## Inventory

### Config/Env Deprecations

| Item | Replacement | Phase | Remove In | Code Usage (non-exhaustive) | Source |
| :--- | :--- | :--- | :--- | :--- | :--- |
| `XG2G_STREAM_PORT` | `enigma2.streamPort` | warn | v3.5.0 | `internal/config/registry.go`, `internal/config/merge.go` | `docs/deprecations.json`, `docs/DEPRECATION_POLICY.md` |

### Schema/Protocol Deprecations

- Legacy decision schema (TitleCase keys): sunset plan to remove in v4.0.
  - Code: `internal/control/recordings/decision/decode.go`
  - Ops: `docs/ops/DEPRECATION_SUNSET.md`

### Packages/Modules

- `internal/core/*` is deprecated (no new code). Still imported by:
  - `internal/config/snapshot.go`
  - `internal/config/runtime_env.go`
  - Policy: `internal/core/README.md`
- `internal/infrastructure/*` imports are banned (deprecated namespace).
  - Gate: `internal/validate/imports_test.go`
  - Policy: `docs/arch/ARCHITECTURE.md`

### Storage Backends

- BoltDB/BadgerDB/JSON are REMOVED; SQLite is the durable truth.
  - ADR: `docs/ADR/ADR-020_STORAGE_STRATEGY.md`, `ADR-021`
  - Code: `internal/domain/session/store/sqlite_store.go`, `internal/pipeline/resume/sqlite_store.go`,
    `internal/pipeline/scan/sqlite_store.go`

## Best Practice Checklist (Removal Process)

1. **Signal**: Document deprecation in `docs/deprecations.json` and release notes.
2. **Telemetry**: Emit warnings and metrics for deprecated usage.
3. **Fail-Start**: Switch to hard-fail in config validation once grace period ends.
4. **Migration**: Provide idempotent tooling and operator playbook.
5. **Removal**: Delete code paths, config keys, tests, and docs in the removal version.
6. **Audit**: Run `scripts/check_deprecations.py` and update this inventory.

## Current Actions (Suggested)

- Legacy decision schema: keep telemetry (`xg2g.decision.schema`) until
  sunset criteria are met, then remove decode support in v4.0.
- Bolt/Badger/JSON: Durable = SQLite only; Ephemeral = memory; migration via `xg2g-migrate`.
  Implementations removed from production packages as of Phase 2.4.
