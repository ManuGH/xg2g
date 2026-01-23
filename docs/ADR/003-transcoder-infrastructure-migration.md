# ADR-003: Transcoder Infrastructure Migration

**Status:** Reserved
**Date:** 2025-01-23
**Context:** Phase 8/9 hardening

## Decision

This ADR number is **reserved** for documenting the transcoder infrastructure
migration from the legacy interface to the new `infra/ffmpeg.Executor` interface.

## Current State

The migration is in progress with a compatibility adapter in place:
- **Adapter:** `internal/pipeline/exec/transcoderAdapter`
- **Timeline:** Adapter scheduled for removal by v3.2.0 (Phase 10 cleanup)
- **Migration Status:** Partial - adapter exists to maintain compatibility

## Rationale

ADR-003 was referenced in code but never formally documented. To maintain
ADR numbering integrity and governance traceability, this placeholder reserves
the number until proper documentation can be completed.

## Technical Debt

The `transcoderAdapter` shim exists to avoid breaking legacy pipeline code.
When this adapter is removed in v3.2.0, this ADR should be updated with:
- Migration strategy and timeline
- Interface compatibility guarantees
- Breaking changes and migration guide
- Rollback plan (if needed)

## References

- Implementation: `internal/pipeline/exec/transcoder_adapter.go`
- Deadline: v3.2.0 (Phase 10 cleanup)
- Related: New executor in `internal/infra/ffmpeg/`
