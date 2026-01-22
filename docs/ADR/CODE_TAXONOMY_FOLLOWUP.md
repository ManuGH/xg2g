# Ticket: Define v3 Error Taxonomy (ProblemDetails.code)

## Context

The `code` field was added to `ProblemDetails` as an optional field to provide stable, machine-readable short codes (e.g., `NOT_FOUND`). However, it currently lacks a formal definition and taxonomy.

## Goal

Establish a rigid, operator-grade taxonomy for error codes to prevent drift and ensure consistent client handling.

## Requirements

1. **Naming Convention**: Enforce a strict naming rule (e.g., `LOWERCASE.DOT.SEPARATED` or `UPPER_SNAKE_CASE`).
2. **Centralized Registry**: Create a single source of truth (enum or registry) for all valid v3 error codes.
3. **Mandatory Transition**: Define the milestone at which the `code` field becomes **required** (e.g., once 80% of handlers are mapped).
4. **Mapping Logic**: Refactor `writeProblem` to accept a stable code from the registry rather than ad-hoc strings.

## Proposed Strategy

- Audit existing `writeProblem` calls.
- Categorize errors into structural domains (Auth, IO, Logic, Downstream).
- Implement a mechanical gate to prevent unregistered strings from being used as codes.
