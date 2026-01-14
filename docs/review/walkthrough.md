# Walkthrough - Refining TruthProvider Semantics

## Verification Results

### Tests Passed

All tests in `internal/control/recordings` passed, confirming the fix for "Preparing forever" and "Silent probe failure".

```bash
go test -v ./internal/control/recordings/...
```

#### Key Test Cases Verified

1. **Impossible Probe Fails Fast**: `TestTruthProvider_ImpossibleProbe_FailsFast` confirms that requests without a local path and no configured remote probe fail immediately with `ErrUpstream` or `ErrRemoteProbeUnsupported`.
2. **Probe Failure Persistence**: `TestTruthProvider_ProbeFailure_PersistsState` confirms that if an async probe fails, the state is updated to `FAILED`, preventing subsequent requests from re-triggering the probe.
3. **Contract Hardening**: `duration_truth_test.go` and `truth_table_test.go` were updated to enforce strict dependency injection (no nil runners) and align with the new fail-fast behavior.

## Implementation Details

### TruthProvider Logic

- **Probe Configured Flag**: Added `probeConfigured` to deterministically check if probing is possible.
- **Fail Fast Gates**:
  - **Gate 1**: `localPath == ""` AND `!probeConfigured` -> `ErrUpstream`.
  - **Gate 2**: `localPath == ""` AND `probeConfigured` -> `ErrRemoteProbeUnsupported`.
  - **Gate 3**: `State == FAILED` -> `ErrUpstream` (prevents retry).

### ProfileResolver

- **User-Agent Removed**: The `User-Agent` field is no longer populated in `ClientProfile` to avoid contract drift.

### Test Hardening

- **Mock Injection**: Fixed `NewManager` calls in tests to always provide non-nil `Runner` and `Prober` mocks, satisfying the new strict invariants.

### Feedback Remediation (C1/C2)

- **Atomic Failure Persistence**: Implemented `MarkFailed` in `vod.Manager` to perform atomic Read-Modify-Write updates on metadata failure.
  - Ensures existing fields (e.g., `ResolvedPath`, `PlaylistPath`) are **preserved** during failure transitions.
  - Ensures `UpdatedAt` timestamp uses `UnixNano()` via `m.touch()`, consistent with the rest of the system.
- **TruthProvider Hardening**: `TruthProvider` now uses `MarkFailed` instead of overwriting metadata, preventing data loss.
- **Verification**: `TestTruthProvider_ProbeFailure_PersistsState` was enhanced to verify that `ResolvedPath` persists after a probe failure and that timestamps increase monotonically.

### Success Persistence Hardening (C1 Symmetry)

- **Atomic Success Updates**: Implemented `MarkProbed` in `vod.Manager` to handle probe success with the same atomic, non-destructive rigor as failure.
  - Signature: `MarkProbed(id, resolvedPath, info, fingerprint)` allows persisting resolution, probe data, and cache fingerprints.
  - Merges new probe data (Codecs, Duration, Container) into existing metadata. Duration uses `math.Round` for precision.
  - Preserves `PlaylistPath`, `ArtifactPath`, and `ResolvedPath`.
  - Infers `ArtifactPath` if `ResolvedPath` ends in `.mp4`.
  - Updates `State` to `READY`, clears `Error`, and touches `UpdatedAt`/`StateGen`.
- **Atomic Failure Updates**: Added `MarkFailure(id, state, reason, resolvedPath, fp)` to allow granular failure states (`Missing`, `preparing`) without overwrites.
- **TruthProvider**: Refactored to use `MarkProbed` instead of `UpdateMetadata`.
- **Prober (Background)**: Refactored to use `MarkProbed` and `MarkFailure`, eliminating the last production usage of `UpdateMetadata` for state transitions.
- **Gate Semantics**: Relaxed "Impossible Probe" gate to respect `HasPlaylist()`.
- **Verification**: Added `TestManager_MarkProbed_PreservesFields` (real implementation test) and updated integration tests.
  - Renamed `UpdateMetadata` to `SeedMetadata` and removed it from the `MetadataManager` interface.
  - This ensures production code cannot inadvertently overwrite metadata, enforcing C1 Symmetry by compilation.
  - Fixed `prober.go` loop correctness (channels) and dead code.

## Architectural Decisions (ADR)

### 1. READY State Semantics

- **Decision**: The `READY` state set by `MarkProbed` strictly implies **"MediaTruth is complete and source is accessible / prob-able"**.

- **Clarification**: It does *not* guarantee that a streamable artifact (HLS or transcoded MP4) exists, only that we have sufficient metadata (Container, Codecs, Duration) to make playback decisions.
- **Implication**: Playback logic must check `HasArtifact()` or `HasPlaylist()` explicitly to determine the playback capabilities. `READY` acts as the gate to allow this logic to proceed.

### 2. MISSING State Non-Terminality

- **Decision**: `ArtifactStateMissing` is treated as **non-terminal (transient)** by the `TruthProvider`.
- **Reasoning**: This allows the system to recover from transient issues such as network shares (NAS) mounting late or temporary file system unavailability.
- **Policy**: The system allows re-probing of `MISSING` items but **throttles** them to prevent storming (1-minute backoff). Terminally failed items (e.g. corrupt files) traverse to `FAILED`.

### 3. Invariant Protection

- **Mechanism**: A `lint-invariants` make target has been added to CI (wired into `quality-gates`).

- **Mechanism**: A `lint-invariants` make target has been added to CI (wired into `quality-gates`).

- **Rule**: `SeedMetadata` is prohibited outside of `_test.go` files and `manager.go` itself.
- **Outcome**: Destructive metadata overwrites are prevented by build policy.

### 4. Correctness Fixes

- **Throttling**: `prober.go` now correctly uses `time.Unix(0, UpdatedAt)` to handle nanosecond timestamps, preventing indefinite future-blocking.
- **Testing**: Assertions for `ErrPreparing` now use purely robust `errors.As` patterns.
- **Test Robustness**: `TestMediaTruth` mocks now use dynamic functional returns to handle async state transitions reliably without brittle call ordering assumptions.
- **Regression Tests**: Added specific test case `Invariant: Ready Meta != Artifact` to `truth_table_test.go` ensuring source fallback works when metadata is ready but artifacts are missing.

## Final Merge Summary

**PR Title:** Duration Truth: nanosecond UpdatedAt throttle, async probe truth-matrix hardening

**Summary:**
This PR finalizes Duration Truth with correctness fixes, async test hardening, and CI invariant enforcement. All hard gates pass and the change-set is regression-protected.

**Key Changes:**

1. **Throttle Clamp (Correctness + Robustness)**:
    - Hardened `ArtifactStateMissing` probe throttle against clock skew by clamping negative elapsed durations.
    - Corrected unit handling by interpreting UpdatedAt as nanoseconds via `time.Unix(0, UpdatedAt)`.
1. **Throttle Clamp (Correctness + Robustness)**:
    - Hardened `ArtifactStateMissing` probe throttle against clock skew by clamping negative elapsed durations.
    - Corrected unit handling by interpreting UpdatedAt as nanoseconds via `time.Unix(0, UpdatedAt)`.
1. **Dynamic Mock Returns (Async Test Stability)**:
    - Refactored `truth_table_test.go` to use dynamic mock returns with channel coordination, making async tests robust against timing/call-order variance and eliminating flakes.
1. **Invariant Enforcement (CI Gate + Regression Test)**:
    - Added `lint-invariants` to quality-gates to prohibit `SeedMetadata` in production code.
    - Added `TestMediaTruth/Invariant:_Ready_Meta_!=_Artifact` regression test ensuring source fallback works when metadata is Ready but artifacts are missing.
    - Updated test documentation to explicitly match observed behavior: “Direct play from source path”.
1. **Typed Error Assertions**:
    - Corrected `ErrPreparing` assertions to use `errors.As` / `assert.ErrorAs` for type-safe matching.

## Artifact Consolidation

* **Source of Truth**: Artifacts (`walkthrough.md`, `task.md`) are now committed to `docs/review/` to ensuring they are included in source archives (`git archive`).
- **CI Consistency**: `codeql.yml` verified to use `actions/checkout@v6` and correctly output dummy assets to `internal/control/http/dist`.
