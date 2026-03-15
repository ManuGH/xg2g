# Repository Structure Audit

Status: Active  
Last Reviewed: 2026-03-01

## Purpose

This document is a quick orientation audit for developers who are new to this
repository. It summarizes where to look first and which structure documents are
authoritative.

## Current Assessment

- The repository follows a layered `internal/` architecture with explicit
  ownership boundaries.
- Build, test, and release workflows are highly gate-driven (`Makefile`,
  `.github/workflows/`).
- Runtime operations and governance policy are documented under `docs/ops/`.
- WebUI is developed in `frontend/webui/` and embedded into
  `backend/internal/control/http/dist/` for backend serving.

## Source Of Truth Documents

- Package boundaries and layering rules:
  `docs/arch/PACKAGE_LAYOUT.md`
- System-level architecture:
  `docs/arch/ARCHITECTURE.md`
- Operator and governance posture:
  `docs/ops/EXTERNAL_AUDIT_MODE.md`
- Development and contribution flow:
  `backend/WORKFLOW.md`

## Scope Notes

This file is an index-level audit entrypoint. Detailed controls, contracts, and
policy checks are maintained in the linked source documents.
