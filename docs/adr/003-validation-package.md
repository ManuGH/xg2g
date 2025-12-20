# ADR-003: Validation Packages

**Status:** Accepted  
**Date:** 2025-12-19

## Context

Validation occurs in two very different places:

1. **Startup/preflight checks** (operator-facing, fail-fast)
2. **Runtime input validation** (request/job-facing, structured errors)

Mixing these concerns usually produces brittle code (either too strict at runtime, or too weak at startup).

## Decision

- Use `internal/validation` for **startup checks** that gate process startup (filesystem, listen addr, critical URLs).
- Use `internal/validate` for **reusable validation primitives** that can be used by config parsing, runtime checks, and tests.

## Consequences

- Startup validation can be strict and opinionated (better operator feedback).
- Runtime code can validate inputs without importing “startup-only” dependencies.

## References (Code)

- Startup checks: `internal/validation/startup.go`
- Reusable validators: `internal/validate/validate.go`

