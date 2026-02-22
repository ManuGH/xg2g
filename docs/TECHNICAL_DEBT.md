# Technical Debt & Known Flakes

This document tracks known issues that are not blockers for release but require future attention.

## Known Flakes

### 1. [Flaky: TestEvictRecordingCache_Concurrent under concurrent eviction]

- **Location**: `internal/control/vod/recording_cache_evictor_test.go`
- **Observation**: Historical flake came from concurrent directory creation vs eviction in the same cache root.
- **Status**: Closed on 2026-02-20 by per-root eviction serialization + ENOENT-tolerant entry sampling + concurrent create/evict stress coverage.
- **Assigned Issue**: "Flaky: TestEvictRecordingCache_Concurrent under concurrent eviction"
- **Follow-up**:
  - Keep nightly stress execution active as an early warning for filesystem-level regressions.

### 2. [Stability: Unbounded full-test gate]

- **Location**: `Makefile`, `.github/workflows/ci-deep-scheduled.yml`
- **Observation**: `go test` invocations in primary/offline/nightly gates ran without explicit per-invocation timeout budgets.
- **Status**: Closed on 2026-02-20 by fail-closed timeout budgets in Make targets (`test`, `test-race`, `test-cover`, `coverage*`, `quality-gates-offline`) and nightly race/integration commands.
- **Follow-up**:
  - Keep timeout budgets aligned with observed CI percentiles; tighten only with historical evidence.

## Architectural Debt

### 1. FFmpeg Path Standardization

- **Status**: Completed on 2026-02-19.
- **Result**: Local build, wrappers, dev runtime hints, and docs now consistently use `/opt/ffmpeg`.
- **Follow-up**: Keep non-root permission validation in CI smoke checks.

### 2. Missing Verifier Templates

- **Status**: Completed on 2026-02-19.
- **Result**: Added `templates/docs/ops/xg2g-verifier.service.tmpl` and `templates/docs/ops/xg2g-verifier.timer.tmpl`; renderer now emits both units.
- **Follow-up**: Keep unit content aligned with `scripts/verify-runtime.sh` contract changes.

### 3. Remux Stub Cleanup

- **Status**: Completed on 2026-02-19.
- **Result**: Removed stale remux stub file `internal/control/http/v3/recordings_remux.go`; compile gate remained green after deletion.
- **Follow-up**: Keep remux behavior implemented only in active VOD pipeline paths.

### 4. WebUI Generated Client Wrapper Discipline

- **Status**: Completed on 2026-02-19.
- **Result**: Added `webui/src/client-ts/wrapper.ts`, removed direct `client-ts/*.gen` imports in product code, and added typed RFC7807 mapping tests (`webui/src/client-ts/wrapper.test.ts`).
- **Follow-up**: Preserve the wrapper boundary for all new API call sites and keep error mapping contract tests in CI.

### 5. Go Toolchain Directive Normalization

- **Status**: Completed on 2026-02-19.
- **Result**: Normalized `go.mod` directives to `go 1.25` and `toolchain go1.25.7`.
- **Follow-up**: Keep Go directive updates synchronized with CI toolchain image updates.

### 6. CI Wrapper Boundary Drift

- **Status**: Completed on 2026-02-19.
- **Result**: Centralized boundary check in `webui/scripts/verify-client-wrapper-boundary.sh` and reused it across PR/CI/nightly workflows to avoid regex drift.
- **Follow-up**: Keep all future WebUI API-boundary checks delegated to this script (single source of truth).
