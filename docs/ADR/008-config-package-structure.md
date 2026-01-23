# ADR-008: Config Package Structure and Responsibilities

**Status:** Accepted
**Date:** 2026-01-23

## Context

The `internal/config` package had grown into a monolith `config.go` file (approx 1300 lines) containing:

- Struct definitions
- Constants
- Loading logic (Loader)
- Merge logic (File vs Env)
- Validation helpers
- Environment variable parsing primitives

This violated the Single Responsibility Principle and made testing specific behaviors (like precedence logic vs parsing logic) difficult. It also obscured the "Split Boundary" between:

1. **Schema** (Types)
2. **Orchestration** (Loader)
3. **Business Logic** (Merge/Precedence)
4. **Primitives** (Env parsing)

## Decision

We split `internal/config` into 4 focused files with strict responsibilities:

### 1. `types.go` (Schema)

**Responsibility:** "What the config looks like."

- Contains `AppConfig`, `FileConfig`, and all sub-structs.
- Contains constants (e.g., defaults, policy strings).
- **Prohibited:** Logic, method receivers (except simple `String()` or `MaskSecrets`).

### 2. `loader.go` (Orchestration)

**Responsibility:** "How the config is loaded."

- Contains `Loader` struct and `NewLoader`.
- Implements the `Load()` method (the main entry point).
- Enforces the "Strict Validated Order": Defaults -> File -> Env -> Validate.
- Centralizes **all** `os.LookupEnv` calls (except for strict primitives in `env.go`).

### 3. `merge.go` (Business Logic)

**Responsibility:** "How values override each other."

- Contains `mergeFileConfig`, `mergeEnvConfig`, `setDefaults`.
- Implements the precedence rules (e.g., Env > File).
- **Constraint:** Must not own environment parsing semantics. May call helpers, but generally operates on values/structs, not raw keys/IO.
- **Pure Logic:** Should ideally be testable with in-memory structs and strings.

### 4. `env.go` (Primitives)

**Responsibility:** "How to read the environment."

- Contains primitive parsers: `ParseString`, `ParseBool`, `ParseInt`, `ParseDuration`.
- Parsing helpers (e.g., token parsing) should live here if they transform strings to data without config logic.
- Operates on strings/identifiers, not business configuration keys.

## Non-Goals

- **No Renames:** Files are split, but symbols (`AppConfig`, `Loader`) retain their names to minimize impact.
- **No Behavior Change:** Logic remains identical; this is a pure refactor.

## File Discovery

- `doc.go` (to be added/updated) should point to this ADR.

## Consequences

### Positive

- **Testability:** We can write targeted tests for "Merge Logic" (`merge_test.go`) separate from "Loading Flow" (`loader_test.go`).
- **Readability:** Developers looking for struct definitions go to `types.go`. Those debugging precedence go to `merge.go`.
- **Maintainability:** Reduced merge conflicts when adding new fields (add to `types.go`, primitive logic in `env.go`, merge logic in `merge.go`).

### Negative

- **Discovery:** New developers might look for `config.go`. A `doc.go` or README entry should clarify the split.

## Enforcement

- **Code Review:** Reject logic in `types.go`. Reject new struct definitions in `merge.go`. Ensure env parsing stays in `env.go` or `loader.go`.
- **CI:** Existing tests cover the behavior. New tests must respect the split.
