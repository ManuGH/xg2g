# Spec: Epic A1 — Retire Legacy API Surface (`internal/api`)

Status: APPROVED FOR IMPLEMENTATION
Owner: Manuel (architecture sign-off) — implementation by coding agent
Date: 2026-07-24
Prerequisite: Epics R0–R5 complete; WebUI client 100% verified on `/api/v3/`.

---

## 1. Context and Problem Statement

`xg2g` previously carried two API layers: legacy `internal/api` (v1/v2 routes) and canonical `internal/control/http/v3` (`/api/v3`).
Per `SPEC_MODERNIZATION_2026.md` §A1:
- A1.1 (Telemetry & Logging): Implemented via `legacyAPIMiddleware` logging `xg2g_legacy_api_requests_total` + WARN logs.
- A1.2 (Config Gate): Implemented via `api.legacy_enabled` returning 410 Gone when `false`.
- A1.3 (Flip Default): Default `api.legacy_enabled` flips to `false` in configuration registry.

---

## 2. Invariants

- **I1 — Canonical v3 Surface.** All web UI and client interactions use `/api/v3/`.
- **I2 — Fail-Closed Deprecation Gate.** Legacy API endpoints return 410 Gone when `api.legacy_enabled = false`.

---

## 3. Implementation Plan

### Slice A1.3 — Flip Default to `api.legacy_enabled = false`
- **Files**:
  - `[MODIFY] backend/internal/config/registry.go` (Default flips to `false`).
  - `[MODIFY] backend/internal/config/registry_test.go` (Update tests expecting default value).
