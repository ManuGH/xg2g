# ADR-005: Post-Acquisition Cleanup

## Status

Accepted

## Context

The codebase currently reflects a "legacy v3 migration" state, with redundant logic, version-based naming (`v3` suffixes everywhere), and historical artifacts located in ad-hoc packages. To transition to a "domain-driven product" state, we must eliminate redundancy and clarify ownership boundaries.

However, strict constraints apply to this cleanup:

1. **Zero Trust Hardening**: Recent security and stability hardening (LAN Guard, Ingest Boundaries) must not be regressed.
2. **Public Contract stability**: `/api/v3` endpoints must remain stable.
3. **Deprecation Policy**: Deletions should be phased (Freeze -> Delete) to avoid breaking consumers or internal tooling.

## Strategy

We will classify all legacy/redundant components into three categories:

* **Delete Now**: Proven dead code with zero references or purely redundant files.
* **Freeze**: Code to be moved/replaced but kept as a thin wrapper (shim) for one release cycle.
* **Keep**: Canonical implementations or heavily used components.

### Classification Table

| Component | Path | Owner | Classification | Migration Notes |
| :--- | :--- | :--- | :--- | :--- |
| **Filesystem Utils** | `internal/fsutil` | Platform | **Freeze** | content moved to `platform/fs`; keep as shim for v3.3. |
| **Network Utils** | `internal/netutil` | Platform | **Freeze** | content moved to `platform/net`; keep as shim for v3.3. |
| **Startup Validation** | `internal/validation` | Core | **Delete Now** | Merge `PerformStartupChecks` into `internal/health` (canonical). |
| **V3 Handlers** | `internal/api/*_v3.go` | API | **Keep/Refactor** | Move to `internal/api/v3/` for strict encapsulation. |
| **V3 Setup Guide** | `docs/guides/v3-setup.md` | Docs | **Delete Now** | Integrate into `docs/guides/setup.md`. |
| **V3 Env Vars** | `XG2G_V3_*` | Config | **Freeze** | Set removal floor v3.4; verify usage before delete. |

## Implementation Plan

### Phase 1: Code Moves & Encapsulation (PR 1)

1. **Platform Utilities**:
    * Ensure canonical implementation in `platform/fs` and `platform/net`.
    * Restore `internal/fsutil` and `internal/netutil` as deprecated shims importing `platform/*`.
2. **Validation**:
    * Consolidate `internal/validation/startup.go` into `internal/health`.
    * Ensure single entrypoint for startup checks.
3. **V3 Handler Packaging**:
    * Strictly encapsulate V3 handlers in `internal/api/v3`.
    * Provide a canonical `v3.NewHandler(opts)` factory that **always** wires LAN Guard and Admin Token auth.
    * Ensure `openapi.yaml` and "Service" schema presence is validated by tests.

### Phase 2: Documentation & Cleanup (PR 2)

1. **Documentation**:
    * Merge `v3-setup.md` -> `docs/guides/CONFIGURATION.md` (canonical).
    * Make `v3-setup.md` a stub redirect.
    * Fix broken links using `ripgrep`.
2. **Deprecation Warnings**:
    * Add high-signal startup logs for frozen components/envs.

## Verification

* **Build**: `go test ./...` must pass.
* **V3 Contract**: `go test -v ./internal/api/v3/...` must pass (covering routing, auth, openapi).
* **OpenAPI**: Minimal test to ensure `openapi.yaml` loads and contains critical schemas.
