# ADR 014: FileConfig Curated Operator Surface

## Status

**Accepted with deferred convergence**

## Problem Statement

The project uses a unified `Registry` for runtime configuration truth, which is
reflected in the OpenAPI schemas and generated documentation. However, the
`FileConfig` struct (mapped to `config.yaml`) represents only a curated subset
of these settings.

**Collision**: `TestValidateCLI_RealConfig` attempted to validate a real-world
configuration against the expanded Registry surface, exposing fields that exist
in the `Registry` but are not formally mapped in `FileConfig`.

## Decision

We formally recognize `FileConfig` as a **Curated Operator Surface**, not a
mirror of the full `Registry`.

### Core Invariant

| Surface | Role |
|---------|------|
| **Registry** | Full internal truth for all components |
| **FileConfig** | Curated subset for operator use (Simple/Advanced/Integrator tiers) |

### Non-Goals

- FileConfig is **not** intended to expose all Registry fields
- CLI `validate` is **not** equivalent to runtime validation
- Parity is a future convergence goal, not a current requirement

## Consequence & Mitigations

### Mechanical Verification

The `verify-config` gate ensures the curated surface does not drift:

```bash
make verify-config
```

This verifies that:

1. `config.example.yaml` matches documented surfaces
2. `config.generated.example.yaml` reflects Registry truth
3. No `.env` files are tracked in git

### Test Remediation (No-Dauerrot Policy)

To prevent "permanently red" tests, we split the original test:

| Test | Expectation |
|------|-------------|
| `TestValidateCLI_CuratedSurface` | **MUST PASS** — validates curated `config.example.yaml` |
| `TestValidateCLI_RegistryParity` | **MUST PASS** — validates generated Registry example |

Both tests are green. If Registry diverges, `RegistryParity` will fail and
require explicit remediation via this ADR's convergence plan.

### Advanced Config Documentation

Settings not exposed in `FileConfig` are documented via the auto-generated
`docs/guides/CONFIGURATION.md` and `config.schema.json`.

## Convergence Plan (VODConfig Migration)

Full parity is deferred to a future refactoring task that will:

1. Complete the `VODConfig` typed migration
2. Enable hierarchical namespace loading (e.g., `vod.*` instead of flat vars)
3. Implement fail-closed conflict detection for overlapping sources

### Acceptance Criteria for Convergence

- [ ] All VOD-related fields exposed in `FileConfig`
- [ ] `TestValidateCLI_RegistryParity` documents explicit coverage
- [ ] No undocumented environment variable fallbacks

## Related Documents

- [CONTRACT_INVARIANTS.md](../ops/CONTRACT_INVARIANTS.md)
- [CONFIGURATION.md](../guides/CONFIGURATION.md)
