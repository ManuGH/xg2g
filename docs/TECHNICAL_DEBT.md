# Technical Debt & Known Flakes

This document tracks known issues that are not blockers for release but require future attention.

## Known Flakes

### 1. [Flaky: TestEvictRecordingCache_Concurrent under concurrent eviction]

- **Location**: `internal/control/vod/recording_cache_evictor_test.go`
- **Observation**: Observed non-deterministic failure (`no such file or directory` on `index.m3u8`) during high scheduler contention. Likely a race between eviction and directory creation.
- **Status**: Acknowledged. Not a release blocker.
- **Assigned Issue**: "Flaky: TestEvictRecordingCache_Concurrent under concurrent eviction"
- **CI Flake Watch Boundary**: If observed > 3 times/week in CI, this transitions to a **Release Blocker**.
- **Fix Strategy Candidates**:
  - Replace timing/scheduler sensitivity with deterministic barriers (channels).
  - Isolate filesystem lifecycle (ensure directory creation is fully signaled before eviction starts).

## Architectural Debt

### 1. FFmpeg Path Standardardization

- **Observation**: Discrepancy between Local build path (`/opt/xg2g/ffmpeg`) and Container build path (`/opt/ffmpeg`).
- **Impact**: Minor confusion in documentation and manual configuration.
- **Goal**: Standardize on `/opt/ffmpeg` for all 2026+ builds.
- **Prerequisite**: Update `scripts/build-ffmpeg.sh` default and verify non-root permission handling.

### 2. Missing Verifier Templates

- **Observation**: `scripts/render-docs.sh` expects `xg2g-verifier.service.tmpl` and `xg2g-verifier.timer.tmpl` in `templates/docs/ops/`, but they are missing.
- **Impact**: Broken documentation rendering pipeline. Temporarily commented out in `scripts/render-docs.sh`.
- **Goal**: Restore or recreate these templates.
