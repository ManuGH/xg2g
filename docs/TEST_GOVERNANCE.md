# Test Governance

This document is binding. It defines the explicit separation between
deterministic tests and optional integration/IO/ffmpeg tests.

## 1. Deterministic Tests (Default Gate)

Deterministic tests are the default gate and must be reliable via:

- `go test ./...`
- `make test`

Rules:

- Pure Go (CPU-only), no network, no external processes, no ffmpeg.
- Stable, repeatable, and not time-sensitive.
- Must not require special hardware, system state, or local config.

## 2. Integration / IO / ffmpeg Tests (Optional)

These tests are optional and opt-in. They can exercise external processes,
IO, or system dependencies (e.g. ffmpeg), and are not part of the default gate.

Rules:

- Must be behind build tags (e.g. `//go:build integration`).
- Must be run explicitly via `make test-integration` (or more specific targets).

## 3. Why This Split Is Mandatory

- `go test ./...` is trustworthy again.
- CI stays stable and deterministic.
- Fewer debates about whether a failure is a product bug or test flakiness.
