# ADR-003: Contract-First DTO Enforcement

## Status

Accepted (2026-01-11)

## Context

The xg2g project uses OpenAPI to define its API contract. However, technical debt has accumulated where some HTTP handlers used handwritten JSON structs instead of the generated DTOs. This led to "contract drift," where the actual API response fields (e.g., `stream_url`, `playback_type`) did not match the intended OpenAPI specification (e.g., `url`, `mode`).

## Decision

1. **Generated DTOs as Source of Truth**: All API responses in `internal/control/http/v3/` must use the generated types from the `types` package.
2. **Forbidden Handwritten Structs**: Handwritten JSON tags for API responses are strictly forbidden in handler files. All mapping must be done from domain models to generated DTOs.
3. **Strict CI Enforcement**:
    - CI must include a "Grep Gate" to detect known drifted field names/tags.
    - Contract tests must use strict JSON decoding to detect unknown or missing fields.

## Consequences

- **Correctness**: The API will strictly adhere to the OpenAPI specification.
- **Frontend Sync**: The WebUI can rely on the OpenAPI contract for type safety.
- **Maintenance**: Changes to the API surface must start with an OpenAPI modification, ensuring the contract remains the lead artifact.
- **Enforcement**: Any PR introducing handwritten response structs or drifted tags will be automatically rejected by CI.
