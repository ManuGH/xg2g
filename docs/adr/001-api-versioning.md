# ADR-001: API Versioning Strategy

**Status**: Accepted
**Date**: 2025-01-21
**Deciders**: Development Team
**Technical Story**: Priority 1 Implementation

## Context and Problem Statement

xg2g exposes HTTP endpoints for playlist management, status queries, and refresh triggers. As the API evolves, we need a strategy to introduce new features and breaking changes without disrupting existing clients (Plex, Jellyfin, custom scripts).

Without versioning, any API changes risk breaking existing integrations, forcing all clients to upgrade simultaneously.

## Decision Drivers

- Backward compatibility for existing Plex/Jellyfin integrations
- Ability to introduce breaking changes safely
- Clear deprecation path for legacy endpoints
- Standards compliance (RFC 8594 for deprecation)
- Minimal overhead for clients

## Considered Options

1. **URL Path Versioning** (`/api/v1/`, `/api/v2/`)
2. **Header-based Versioning** (`Accept: application/vnd.xg2g.v1+json`)
3. **Query Parameter Versioning** (`/api/status?version=v1`)
4. **No Versioning** (breaking changes with major version bumps only)

## Decision Outcome

**Chosen option**: "URL Path Versioning (`/api/v1/`)"

### Rationale

1. **Developer-Friendly**: Most intuitive for API consumers
2. **Cache-Friendly**: Different URLs allow caching strategies per version
3. **Tooling Support**: Standard REST clients, Postman, curl all work naturally
4. **Clear Separation**: Each version can have independent middleware, handlers, tests
5. **Industry Standard**: Widely adopted pattern (Stripe, GitHub, Twilio)

### Positive Consequences

- Existing `/api/*` endpoints remain functional (backward compatible)
- New features can be introduced in `/api/v2/` without breaking v1
- Clear deprecation timeline using `Sunset` and `Deprecation` headers (RFC 8594)
- Version can be feature-flagged for gradual rollout

### Negative Consequences

- Slightly longer URLs (`/api/v1/status` vs `/api/status`)
- Need to maintain multiple API versions during transition periods
- Potential code duplication between versions (mitigated by shared handlers)

## Pros and Cons of the Options

### URL Path Versioning

- **Good**, because most RESTful and developer-friendly
- **Good**, because allows version-specific caching
- **Good**, because tooling works out-of-the-box
- **Bad**, because requires URL changes for clients

### Header-based Versioning

- **Good**, because URLs stay constant
- **Bad**, because less discoverable (not visible in URL)
- **Bad**, because complicates caching strategies
- **Bad**, because requires custom header handling in all clients

### Query Parameter Versioning

- **Good**, because backward compatible (default version)
- **Bad**, because pollutes URLs with boilerplate
- **Bad**, because inconsistent with REST principles

### No Versioning

- **Good**, because simplest implementation
- **Bad**, because forces breaking changes on all clients simultaneously
- **Bad**, because no graceful deprecation path

## Implementation

### URL Structure

```
/api/v1/status       # Versioned endpoint
/api/v1/refresh      # Versioned endpoint
/api/v1/channels     # New endpoint (v1 only)

/api/status          # Legacy endpoint (deprecated)
/api/refresh         # Legacy endpoint (deprecated)
```

### Deprecation Headers (RFC 8594)

```http
HTTP/1.1 200 OK
Deprecation: true
Sunset: Wed, 01 Jul 2026 00:00:00 GMT
Link: </api/v1/status>; rel="successor-version"
```

### Code Structure

```
internal/api/
├── v1/
│   ├── handlers.go      # v1-specific handlers
│   ├── handlers_test.go
│   └── routes.go
├── middleware/
│   ├── deprecation.go   # Deprecation header middleware
│   └── versioning.go
└── router.go            # Main router with version routing
```

### Migration Path

**Phase 1 (Current)**: Dual Support
- `/api/v1/*` fully functional
- `/api/*` functional with deprecation warnings
- Both versions point to same handlers (no breaking changes yet)

**Phase 2 (Future)**: Divergence
- `/api/v2/*` introduced with new features
- `/api/v1/*` maintained for compatibility
- `/api/*` returns HTTP 410 Gone after sunset date

**Phase 3 (Long-term)**: Cleanup
- Remove `/api/*` legacy endpoints
- Consider `/api/v1/*` deprecation if v2 is stable

### Feature Flag Support

```go
// Enable v2 preview for testing
export XG2G_API_V2_ENABLED=true
```

### Testing

```go
func TestAPIVersioning(t *testing.T) {
    // Test v1 endpoint
    req := httptest.NewRequest("GET", "/api/v1/status", nil)
    // ...

    // Test legacy endpoint returns deprecation headers
    req = httptest.NewRequest("GET", "/api/status", nil)
    // Verify Deprecation and Sunset headers
}
```

## Links

- [RFC 8594 - Sunset HTTP Header](https://datatracker.ietf.org/doc/html/rfc8594)
- [GitHub API Versioning](https://docs.github.com/en/rest/overview/api-versions)
- [Stripe API Versioning](https://stripe.com/docs/api/versioning)
- Implementation PR: Priority 1 completion
- Related: ADR-004 (OpenTelemetry) - version-aware tracing

## Notes

### Lessons Learned

1. **Early Versioning Pays Off**: Introducing versioning before breaking changes simplifies future development
2. **Deprecation Headers Are User-Friendly**: Clients get clear warnings without service disruption
3. **Testing Both Versions**: Ensure comprehensive tests for both legacy and versioned endpoints

### Future Considerations

1. **API v2 Scope**: Consider GraphQL or gRPC for v2 if REST limitations emerge
2. **Automated Client Migration**: Provide migration scripts/tools for major version bumps
3. **Version Lifecycle Policy**: Define clear support windows (e.g., v1 supported for 2 years after v2 release)
4. **Breaking Change Documentation**: Maintain changelog highlighting API differences between versions
