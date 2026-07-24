# Spec: Epic R3 — Single Delivery Format (CMAF/fMP4) + JIT Copy Path

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epic R2 (Slices R2.1–R2.4) merged to `main` (PR #718 merged, `PromoteFailedToReadyIfPlaylist` deleted, SQLite FSM active).

---

## 1. Context and Problem Statement

Currently, `xg2g` maintains dual delivery packagers: MPEG-TS (`.ts`) and fMP4 (`.mp4`/`.m4s`). For copy-mode playback (where the client natively decodes the source codec), `xg2g` still spawns an asynchronous build job that materializes full HLS segments on disk while making the client poll `PREPARING` (`503 Retry-After`).

### Core Objectives of Epic R3:
1. **Single Segmenter Interface (CMAF/fMP4)**: Standardize all new delivery packaging on CMAF/fMP4 fragments as the primary packager interface.
2. **JIT (Just-In-Time) Copy-Mode Remux**: For `copy`-mode playback, repackage incoming MPEG-TS into fMP4 fragments on the fly at request time without spawning background FFmpeg disk-build jobs or writing segments to disk.
3. **Seek-Ahead for Transcode Builds**: For transcode-mode builds, support seek-ahead (secondary segment-aligned FFmpeg run at the requested seek position).

---

## 2. Invariants

- **I1 — Planner decides, delivery executes.** Delivery packagers execute decisions issued by the planner (`TargetPlaybackProfile`).
- **I2 — Zero disk materialization for copy-mode.** Copy-mode playback streams fragments directly to HTTP responses with zero disk writes beyond logs/metrics.
- **I3 — Deterministic FSM state.** Transcode builds update the SQLite `ArtifactStore` via the FSM (Epic R2). Copy-mode requests bypass the artifact FSM build loop entirely (`READY` on demand).
- **I4 — No behavior change without characterization tests.** Every step is tested and verified against client playback expectations.

---

## 3. Slice Breakdown & Implementation Sequence

### Slice R3.1 — Delivery Packager Interface & CMAF/fMP4 Packager Foundation
- **Goal**: Abstract segment packaging behind a unified `Packager` interface (`internal/domain/delivery`).
- **Files**:
  - `[NEW] internal/domain/delivery/packager.go`
  - `[NEW] internal/domain/delivery/cmaf/packager.go`
  - `[NEW] internal/domain/delivery/cmaf/packager_test.go`

### Slice R3.2 — JIT Copy-Mode Remuxer
- **Goal**: Implement `JITRemuxer` for copy-mode playback (`internal/control/vod/jit_remuxer.go`).
- **Files**:
  - `[NEW] internal/control/vod/jit_remuxer.go`
  - `[NEW] internal/control/vod/jit_remuxer_test.go`
  - `[MODIFY] internal/control/vod/manager.go` (bypass build pipeline for copy-mode when JIT enabled)

### Slice R3.3 — Seek-Ahead Transcode Frontier & Resolver Cutover
- **Goal**: Support seek-ahead in transcode builds and cut VOD resolver over to JIT copy-mode remuxing.
- **Files**:
  - `[MODIFY] internal/control/http/v3/recordings/artifacts/resolver.go`
  - `[MODIFY] internal/control/vod/manager.go`

---

## 4. Verification Plan

### Automated Tests
- `go test -v ./internal/domain/delivery/...`
- `go test -v ./internal/control/vod/...`
- `go test -v ./internal/control/http/v3/...`
- `make ci-pr`

### Manual Verification
- Deploy to Staging (`./scripts/fast_deploy.sh --confirm-staging`) and verify zero-latency JIT copy-mode playback on `xg2g.home.matrixcentral.de`.
