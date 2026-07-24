# Spec: Epic A2 — Split `internal/control/http/v3` God-Package

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epics R0–R5, A1, A5 complete.

---

## 1. Context and Problem Statement

`internal/control/http/v3` contains 103 flat files in a single package. Per `SPEC_MODERNIZATION_2026.md` §A2, we mechanically decompose the package into targeted subpackages:
1. `v3/tokens` (Slice A2.1)
2. `v3/sessions` (Slice A2.2)
3. `v3/playbackinfo` (Slice A2.3)
4. `v3/hls` (Slice A2.4)

Routing and public API behavior remain 100% byte-identical.

---

## 2. Invariants

- **I1 — Routing Parity.** Generated routes and handlers must remain 100% byte-identical before and after each extraction.
- **I2 — Mechanical Move-Only.** No behavior, name, or signature changes in move PRs.

---

## 3. Implementation Plan

### Slice A2.1 — Extract `v3/tokens` Subpackage
- **Files**:
  - Move token-related handler files into `internal/control/http/v3/tokens/`.
