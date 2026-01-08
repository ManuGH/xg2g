# ADR 003: Transcoder Infrastructure Migration (Phase 8/9 Hardening)

## Status

Proposed (2026-01-08)

## Context

During the completion of Phase-8 and Phase-9 (VOD Hardening & FFmpeg Automation), the underlying execution infrastructure was moved from `internal/pipeline/exec/ffmpeg` (Runner-based) to `internal/infra/ffmpeg` (Executor-based).

The legacy streaming pipeline still relies on the old `Transcoder` interface. To allow immediate progress on production-ready FFmpeg builds without a complete (and risky) rewrite of the entire streaming session logic, a compatibility shim was required.

## Decision

We implement a `transcoderAdapter` in `internal/pipeline/exec/transcoder_adapter.go` that:

1. Implements the legacy `Transcoder` interface.
2. Uses the new `infra/ffmpeg.Executor` internally.
3. Provides minimal functional parity for the old pipeline.

## Timebox & Clean-up Plan

- **Scope**: Exclusive to legacy streaming sessions. New VOD features MUST use the `infra/ffmpeg.Executor` directly.
- **Deadline**: This adapter MUST be removed by **v3.2.0**.
- **Migration Path**: In Phase-10, the legacy session manager will be refactored to use the generic `Handle` and `Spec` patterns introduced in Phase-9.

## Consequences

- **Positive**: v3.1.3 can ship with a deterministic, hardened FFmpeg build.
- **Positive**: Separation of concerns between old "Policy" and new "Infra" is maintained via the Executor.
- **Negative**: Temporary architectural debt ("Shim").
- **Negative**: Old pipeline might see restricted functionality (e.g., partial logs) if the adapter is not fully mapped.

## Guardrails

- CI will flag new files importing `internal/pipeline/exec/ffmpeg` (already removed).
- The adapter code is explicitly marked with `[DEBT]` and a `DEADLINE`.
