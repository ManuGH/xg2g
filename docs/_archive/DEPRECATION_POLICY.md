# Deprecation Policy

This project follows Semantic Versioning and treats deprecations as a release-stability contract.

## Rules

- Every deprecation must state: the replacement, the removal version, and a migration path.
- Default removal target: the next major release (N+1). Earlier removal (next minor) requires explicit justification and at least one minor release of notice.
- Deprecated behavior must emit a WARN-level log with the removal version and the replacement.
- Documentation must list active deprecations in one place.
- The registry in `docs/deprecations.json` is the source of truth and is validated in CI.

## Active Deprecations

Source of truth: `docs/deprecations.json`.

None.
