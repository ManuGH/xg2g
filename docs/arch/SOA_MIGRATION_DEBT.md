# SOA Migration Debt

> [!WARNING]
> This document tracks explicit architectural debt incurred during "Strangler"
> migrations.
> All items listed here are temporary allowances to unblock migration steps
> without "Big Bang" refactors.
> **Silence is not consent**: New items require CTO approval.

## Active Whiteness Items (verify_deps.sh)

### 1. Session Manager -> Pipeline Infrastructure

* **Allowed Import**: `internal/domain/session/manager` -> `internal/pipeline/*`
  * Specifically: `pipeline/bus`, `pipeline/exec`, `pipeline/scan`, `pipeline/resume`
* **Reason**: The Orchestrator logic was moved "as-is" (pure move) to the Domain layer in
  Step 3B. Refactoring deep dependencies (FFmpeg, localized Bus) was out of scope
  to preserve behavior and minimize risk.
* **Exit Criteria**:
  * Decoupling of Playback logic (FFmpeg runner) via Domain Interfaces
    (Ports & Adapters).
  * Injection of Bus/Event adapters rather than direct struct usage.
* **Deadline**: 2026-02-15 (Start of Q1 '26 cleanup sprint)
* **Owner**: @ManuGH / Core Backend Team

## Change Log

* **2026-01-08**: Initial whitelist created for Step 3B (Session Manager move).
