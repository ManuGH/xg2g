# ADR-016: Offline-Deterministic Reviews via Vendoring

**Status:** ACCEPTED  
**Date:** 2026-01-11  
**Author:** Engineering Team  

---

## Context

To ensure project security, architecture, and contracts can be verified in restricted environments, we require a workflow that functions without internet access or live repository connectivity.

Previously, reviews relied on live module downloads which introduced non-determinism and required network access.

## Decision

We migrate to a mandatory vendoring model for all official reviews and verification cycles.

### Key Implementation Details

1. **Mandatory Vendoring**: The `vendor/` directory is now a first-class citizen in the repository. All dependencies must be present locally.
2. **Toolchain Locking**: `GOTOOLCHAIN=local` is enforced to prevent automatic downloads of Go versions.
3. **Build Mode**: All build and test operations must use `-mod=vendor`.
4. **Authoritative Artifact**: The official review artifact is a ZIP snapshot generated via:

    ```bash
    git archive --format=zip HEAD -o xg2g-main.zip
    ```

5. **Purity Guardrail**: CI will enforce that the review artifact contains no binary executables (ELF, Mach-O, PE).

## Rationale

- **Deterministic Reviews**: Guarantees that the exact same code and dependencies are seen by reviewers and CI.
- **Offline Readiness**: Enables verification in air-gapped or restricted environments.
- **SSOT**: The ZIP file becomes the single source of truth for the review, superseding any local-only repository state.

## Consequences

- **Increased Repository Size**: The inclusion of `vendor/` increases the repository and artifact size (to approximately 50 MB compressed). This is explicitly accepted as the new normal.
- **Manual Dependency Updates**: Adding or updating dependencies requires a manual `make deps-vendor` call to refresh the vendor directory.
- **Artifact Hygiene**: Reviewers can rely on the ~50 MB ZIP being source-only and complete.

---

**Status:** ACCEPTED  
**Effort:** LOW (Formalization of implemented workflow)
