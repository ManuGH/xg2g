# ADR-002: Config Precedence and Determinism

**Status:** Accepted  
**Date:** 2025-12-19

## Context

Historically, “read ENV anywhere” leads to:

- config drift within a single request/job (multiple reads see different values)
- hard-to-reproduce bugs and flaky behavior during reloads
- hidden coupling between packages and process environment

We need deterministic, reviewable runtime behavior.

## Decision

- Configuration is loaded into an immutable snapshot at startup and on explicit reload.
- Environment variables are read **only** during load/reload (never in the hot path).
- A configuration “epoch” is monotonically incremented on swap; a single operation (HTTP request/job/run) must use exactly one snapshot.
- Precedence remains: **ENV overrides file overrides defaults**.

## Consequences

- Reload changes are atomic: no request should observe mixed configuration.
- Code must pass `*config.Snapshot` (or values derived from it) into lower layers instead of reading globals/ENV.

## Enforcement

- CI tripwire forbids `os.Getenv` / `os.LookupEnv` outside `internal/config` for non-test code.

## References (Docs / Code)

- Normative contract: `docs/SECURITY_INVARIANTS.md` (§3.1)
- Env reading and snapshot build: `internal/config`
- CI tripwire: `.github/workflows/lint.yml`

