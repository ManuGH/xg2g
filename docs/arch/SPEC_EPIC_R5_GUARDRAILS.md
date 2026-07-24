# Spec: Epic R5 — Guardrail Completions & Decision-Literal Linter

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epic R4 (Unified Intent Envelope) merged to `main`.

---

## 1. Context and Problem Statement

To prevent architectural degradation and policy leaks (such as hardcoded codec/resolution literals resurfacing in builder or HTTP packages), Epic R5 introduces technical guardrail linters:
1. **Decision-Literal Linter**: Extends `lint-invariants` with a grep check forbidding codec/container/resolution literals (`"hevc"`, `"mpeg2video"`, `"h264"`, `1920`, `1080`, …) outside of `internal/domain/playbackprofile` and planner packages.
2. **CI/Local Parity Drift Gate**: Ensures `make pre-push` and `make ci-pr` remain byte-identical mirrors of GitHub Actions workflow targets.

---

## 2. Invariants

- **I1 — Invariant I1 Technical Enforcement.** No code outside domain playback profile packages may make hardcoded decision assumptions.
- **I2 — CI/Local Drift Lock.** Local `make pre-push` / `make ci-pr` must invoke exact same targets as GitHub Actions workflows.

---

## 3. Implementation Plan

### Slice R5.1 — Decision-Literal Linter & Drift Lock
- **Files**:
  - `[MODIFY] backend/scripts/ci_gate_lint_invariants.sh`
  - `[MODIFY] Makefile`
