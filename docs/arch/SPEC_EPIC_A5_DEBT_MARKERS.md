# Spec: Epic A5 — Make Transitional Debt Visible in Code & TODO-Linter

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epics R0–R5 and Epic A1.3 complete.

---

## 1. Context and Problem Statement

Per `SPEC_MODERNIZATION_2026.md` §A5:
Transitional debt mechanisms (legacy payload acceptance, fallback paths, etc.) must be clearly referenced in code so future sessions never lose context. Furthermore, a linter check in `lint-invariants` enforces that any `TODO` or `FIXME` comment must reference a spec or ticket via `(SPEC_...)` or `(#...)`.

---

## 2. Implementation Plan

### Slice A5.1 — Debt Markers & TODO Linter
- **Files**:
  - `[MODIFY] backend/internal/control/http/v3/recordings/cache.go` (Add `// TODO(SPEC_MODERNIZATION_2026 §A5): legacy bare-target payload acceptance`).
  - `[MODIFY] backend/internal/control/http/v3/recordings/artifacts/resolver.go` (Add `// TODO(SPEC_MODERNIZATION_2026 §A5): copy-default fallback`).
  - `[MODIFY] backend/scripts/ci/check-todo-format.sh` [NEW] (Linter checking for naked TODO/FIXME comments without reference).
  - `[MODIFY] .github/workflows/lint.yml` (Add `check-todo-format` job).
