# xg2g Documentation Portal

[![Coverage](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml)

Welcome to the normative documentation for xg2g.
To ensure consistency and "Operator-Grade" quality,
all documentation follows a strict hierarchy.

## 🏁 Entry Point

The single source of truth for the system's design
and decision logic is the **Spec Index**:

👉 **[SPEC_INDEX.md](decision/SPEC_INDEX.md)**

---

## 📚 Documentation Structure

### [ADR/](ADR/)

Architectural Decision Records. These are immutable once accepted
and define the "why" behind the system's construction.

### [ops/](ops/)

Operational playbooks, incident management, and deployment invariants.

### [arch/](arch/)

High-level architectural diagrams and service boundary definitions.

### [archive/](archive/)

Legacy documentation and superseded ADRs. Reference only.

---

## 🏛️ Governance

All changes to the documentation MUST be reflected in the
[SPEC_INDEX.md](decision/SPEC_INDEX.md) and
adhere to the [PRODUCT_POLICY.md](PRODUCT_POLICY.md).
Branch protection required checks are enforced in CI for all pull requests.
