# ADR-003: Legacy Pipeline Exec Adapter Removal

**Status:** Accepted
**Date:** 2026-02-16
**Context:** Sprint 4 (structure cleanup)

## Decision

Remove the unused legacy `internal/pipeline/exec` root package, including
the incomplete `transcoderAdapter` shim.

## Why

- The adapter path was not imported by production code.
- `transcoderAdapter` was intentionally non-functional (`Start/Wait` returned errors),
  which made it a latent failure path and a maintenance trap.
- Keeping dead compatibility layers increases coupling noise between API/control
  and media pipeline responsibilities.

## Scope

Removed files:

- `internal/pipeline/exec/interfaces.go`
- `internal/pipeline/exec/real_factory.go`
- `internal/pipeline/exec/stubs.go`
- `internal/pipeline/exec/transcoder_adapter.go`

`internal/pipeline/exec/enigma2/*` remains and is still used by active code paths.

## Consequences

- Architecture boundary is clearer: active runtime code uses current pipeline/media
  abstractions only.
- Dead adapter contract cannot be accidentally revived in future changes.
