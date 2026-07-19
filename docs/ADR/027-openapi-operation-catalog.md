# ADR-027: OpenAPI Operation Catalog as the Route and Policy Source

**Status:** Accepted
**Date:** 2026-07-19
**Triggers:** Route, authorization, exposure, and generated-server declarations had to remain synchronized by hand.

## Context

The v3 HTTP surface described each operation in several independent places:
OpenAPI, generated server wrappers, handwritten route modules, authorization
scope maps, and exposure-policy maps. Adding or changing one route therefore
required coordinated edits with no structural guarantee that production wiring
matched the contract. The handwritten route set also registered two live routes
twice.

Parity tests detected some drift after the fact, but they did not remove the
duplicate decision points. A route could still be valid in OpenAPI and absent,
duplicated, or differently protected in the runtime router.

## Decision

`backend/api/openapi.yaml` is the canonical operation catalog:

- OpenAPI paths and methods define the runtime route.
- `operationId` defines the generated wrapper method and policy key.
- OpenAPI `security` defines the required bearer scopes.
- `x-xg2g-operation-policies` defines exposure class, authentication kind,
  browser trust, rate limiting, audit, and error-redaction requirements.

`cmd/gen-operation-catalog` validates the complete catalog and generates both
the authorization tables and the route-registration table. The runtime router
mounts only that generated registration. Every OpenAPI operation must have
exactly one exposure policy; unknown policies, duplicate canonical operation
IDs, duplicate routes, unsupported security shapes, and auth-policy mismatches
fail generation.

## Change Contract

### Fixed

- OpenAPI security declarations for health, connectivity, config, and scan now
  match the already enforced runtime scope policy.
- Duplicate live-route registration is eliminated.

### Improved

- Route, scope, and exposure drift is rejected during generation and CI.
- Adding an operation no longer requires editing router modules and two policy
  registries independently.
- Generated artifacts are covered by the repository's exact governance list
  and deterministic drift checks.

### New

- A validated OpenAPI operation-policy extension.
- Generated runtime route and authorization catalogs.
- Negative generator tests and exact route-to-policy parity tests.

### Removed

- Handwritten v3 route modules and their pairing/device wrapper variants.
- Manual authorization and exposure maps.
- The unused `ExposurePolicy.PublicAllowed` field, which was always true and
  had no runtime consumer.

### Explicitly Unchanged

- Existing runtime scopes, exposure behavior, handlers, base paths, and
  middleware order.
- Handler implementation remains handwritten; only operation wiring is
  generated.

## Consequences and risks

OpenAPI changes now have a wider, intentional blast radius: route and policy
artifacts are regenerated together. Reviewers must treat changes to the custom
policy extension as security-sensitive. A generator defect could affect all
routes, so focused catalog tests, the full backend suite, WebUI tests, generated
artifact drift locks, and staged rollout remain mandatory.

The generated server wrapper is the only request-binding adapter. Custom
per-route adapters must be expressed in OpenAPI or handler code rather than by
creating a second router-registration path.

## Migration exit condition

Complete with this decision: all production v3 operations are mounted through
the generated catalog and the parallel handwritten registration files are
removed. Future routes must enter through OpenAPI and pass catalog generation.
