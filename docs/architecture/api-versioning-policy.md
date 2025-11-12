# API Versioning Policy

**Version:** 1.0
**Status:** Active
**Last Updated:** 2025-11-12

## Purpose

This policy establishes predictable, backward-compatible API evolution for xg2g, ensuring clients can adapt to changes with sufficient notice and clear migration paths.

## Versioning Scheme

### Path-Based Versioning

All API endpoints MUST include a version prefix in the URL path:

```
/api/v1/lineup.json
/api/v1/discover.json
/api/v2/streams/<channel_id>
```

**Rationale:** Path-based versioning provides:
- Clear, explicit version visibility in logs/traces
- Simple routing and middleware isolation
- No ambiguity with header-based negotiation

### Version Format

- **Major version only**: `/api/v1/`, `/api/v2/`, etc.
- **No minor/patch in path**: Internal changes within a major version remain backward-compatible
- **Integer increments**: v1 → v2 → v3 (no v1.5 or v1a)

## Compatibility Guarantees

### Within Major Versions (e.g., v1.0 → v1.9)

**Allowed (Non-Breaking Changes):**
- ✅ Adding new endpoints
- ✅ Adding optional query parameters
- ✅ Adding fields to JSON responses
- ✅ Adding new HTTP methods to existing endpoints (if idempotent)
- ✅ Relaxing validation constraints
- ✅ Fixing bugs that don't change semantics

**Forbidden (Breaking Changes):**
- ❌ Removing or renaming endpoints
- ❌ Removing fields from JSON responses
- ❌ Changing field types or semantics
- ❌ Making required parameters optional (or vice versa)
- ❌ Changing HTTP status codes for existing behaviors
- ❌ Modifying rate limit semantics

### Across Major Versions (e.g., v1 → v2)

Breaking changes REQUIRE a new major version. Both versions MUST coexist during the deprecation window.

**Example Breaking Changes:**
- Changing `/api/v1/lineup.json` response schema
- Renaming query parameters (e.g., `bouquet` → `playlist`)
- Removing support for legacy HDHomeRun headers

## Deprecation Process

### 1. Announcement Phase (T+0)

When introducing a breaking change in v2, deprecated v1 endpoints MUST:

1. **Add HTTP headers** to all responses:
   ```http
   Sunset: Wed, 01 Apr 2026 00:00:00 GMT
   Deprecation: true
   Link: </docs/api-versioning-policy#migration-v1-to-v2>; rel="sunset"
   Warning: 299 - "API v1 is deprecated. Migrate to /api/v2/ by 2026-04-01"
   ```

2. **Update documentation**:
   - Mark endpoints as `[Deprecated]` in API docs
   - Publish migration guide with side-by-side examples
   - Add deprecation notice to README

3. **Emit structured logs**:
   ```json
   {
     "event": "api.deprecated_endpoint",
     "version": "v1",
     "path": "/api/v1/lineup.json",
     "client_ip": "192.168.1.100",
     "sunset_date": "2026-04-01T00:00:00Z"
   }
   ```

### 2. Deprecation Window (6 Months Minimum)

- **Minimum duration**: 6 months from announcement
- **Both versions operational**: v1 and v2 run in parallel
- **Metrics collection**: Track v1 vs v2 usage via Prometheus
  ```promql
  rate(http_requests_total{path=~"/api/v1/.*"}[5m])
  ```

### 3. Sunset Phase (T+6 months)

After the Sunset date:

1. **Final warning period (2 weeks)**:
   - Change `Warning: 299` to `Warning: 199 - "API v1 will be removed on 2026-04-15"`
   - Send email notifications to registered API consumers (if applicable)

2. **Decommissioning**:
   - Remove v1 routes from router
   - Return `410 Gone` with sunset information:
     ```json
     {
       "error": "This API version has been sunset",
       "sunset_date": "2026-04-01T00:00:00Z",
       "migration_guide": "https://docs.xg2g.io/migration/v1-to-v2"
     }
     ```

## Support Windows

| Version | Release Date | Deprecation Date | Sunset Date | Status |
|---------|--------------|------------------|-------------|--------|
| v1      | 2024-01-15   | TBD              | TBD         | Active |
| v2      | (future)     | N/A              | N/A         | Planned |

**Policy:** Each major version receives support for a minimum of:
- **6 months** after the next major version is released (active support)
- **2 weeks** final warning period before removal

## Version Discovery

Clients SHOULD discover available versions via:

1. **Root endpoint** (`GET /api`):
   ```json
   {
     "versions": {
       "v1": {
         "status": "deprecated",
         "sunset": "2026-04-01T00:00:00Z",
         "docs": "/docs/api/v1"
       },
       "v2": {
         "status": "stable",
         "docs": "/docs/api/v2"
       }
     },
     "recommended": "v2"
   }
   ```

2. **HTTP Link header** on all responses:
   ```http
   Link: </api>; rel="index"
   ```

## Implementation Guidelines

### Router Setup Example

```go
// internal/api/server.go
func (s *Server) setupRoutes() {
    // Root version discovery
    s.router.Get("/api", s.handleVersionDiscovery)

    // Version-specific routers
    s.setupV1Routes(s.router.Route("/api/v1", nil))
    // Future: s.setupV2Routes(s.router.Route("/api/v2", nil))
}

func (s *Server) setupV1Routes(r chi.Router) {
    r.Use(deprecationMiddleware("v1", "2026-04-01T00:00:00Z"))
    r.Get("/lineup.json", s.handleLineup)
    r.Get("/discover.json", s.handleDiscover)
}

func deprecationMiddleware(version, sunsetDate string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Sunset", sunsetDate)
            w.Header().Set("Deprecation", "true")
            w.Header().Set("Link", fmt.Sprintf("</docs/api-versioning-policy#migration-%s>; rel=\"sunset\"", version))
            w.Header().Set("Warning", fmt.Sprintf("299 - \"API %s is deprecated. Migrate to /api/v2/ by %s\"", version, sunsetDate))

            // Audit log
            log.WithComponentFromContext(r.Context(), "api").Warn().
                Str("event", "api.deprecated_endpoint").
                Str("version", version).
                Str("path", r.URL.Path).
                Str("client_ip", clientIP(r)).
                Str("sunset_date", sunsetDate).
                Msg("deprecated API endpoint accessed")

            next.ServeHTTP(w, r)
        })
    }
}
```

### Migration Guide Template

When introducing v2, create `docs/migration/v1-to-v2.md`:

```markdown
# Migration Guide: API v1 → v2

**Deprecation Date:** 2025-10-01
**Sunset Date:** 2026-04-01

## Breaking Changes

### 1. Lineup Endpoint Response Schema

**v1 (deprecated):**
\```json
{
  "FriendlyName": "xg2g",
  "LineupURL": "http://..."
}
\```

**v2 (current):**
\```json
{
  "friendly_name": "xg2g",  // ← snake_case
  "lineup_url": "http://...",
  "api_version": "v2"        // ← new field
}
\```

**Migration Steps:**
1. Update JSON parser to handle snake_case keys
2. Handle new `api_version` field (can be ignored)
3. Test with `GET /api/v2/lineup.json`

### 2. Rate Limiting Headers

v2 uses standardized RateLimit headers (RFC 9110):

**v1 (deprecated):**
\```http
X-RateLimit-Limit: 10/s
X-RateLimit-Remaining: 5
\```

**v2 (current):**
\```http
RateLimit-Limit: 10
RateLimit-Remaining: 5
RateLimit-Reset: 1
\```

## Timeline

| Date       | Action |
|------------|--------|
| 2025-10-01 | v2 released, v1 deprecated |
| 2025-12-01 | v1 usage metrics review |
| 2026-03-15 | Final warning period begins |
| 2026-04-01 | v1 sunset (410 Gone) |
```

## Monitoring Deprecated APIs

### Prometheus Metrics

Track version usage:
```promql
# Total requests by API version
sum by (api_version) (rate(http_requests_total{path=~"/api/v.*/.*"}[5m]))

# Deprecated endpoint usage (alert if > 5% after T+3 months)
sum(rate(http_requests_total{path=~"/api/v1/.*"}[5m])) /
sum(rate(http_requests_total{path=~"/api/.*/.*"}[5m])) > 0.05
```

### Grafana Dashboard

Create "API Version Adoption" dashboard with:
- Request rate by version (time series)
- Unique clients per version (gauge)
- Top 10 v1 consumers by IP (table)
- Time to sunset (countdown)

### Alerts

Configure Prometheus alerts:
```yaml
groups:
  - name: api_versioning
    rules:
      - alert: DeprecatedAPIUsageHigh
        expr: |
          sum(rate(http_requests_total{path=~"/api/v1/.*"}[5m])) /
          sum(rate(http_requests_total{path=~"/api/.*/.*"}[5m])) > 0.10
        for: 24h
        labels:
          severity: warning
        annotations:
          summary: "v1 API usage still >10% after deprecation"
          description: "Consider extending sunset date or improving migration docs"
```

## Client Responsibilities

Well-behaved clients SHOULD:
1. **Parse `Sunset` header**: Plan migrations proactively
2. **Follow `Link` rel="sunset"**: Read migration guides
3. **Log `Warning` headers**: Alert human operators
4. **Version pin intentionally**: Avoid implicit latest-version behavior
5. **Test v2 early**: Don't wait until sunset date

## Edge Cases

### What if a security vulnerability requires breaking v1?

**Answer:** Security takes precedence over compatibility:
1. Patch v1 immediately (even if breaking)
2. Accelerate v2 adoption (reduce sunset window to 1 month)
3. Notify all known consumers directly
4. Document as CVE fix, not routine deprecation

### Can we backport v2 features to v1?

**Answer:** Only if backward-compatible:
- ✅ New optional fields: Yes
- ✅ Performance improvements: Yes
- ❌ Schema changes: No (requires v2)

### What about undocumented/internal APIs?

**Answer:** Internal APIs have no compatibility guarantees:
- Prefix with `/internal/` or `/_/` to signal instability
- Can change without versioning
- No Sunset headers required

## References

- **RFC 8594**: The Sunset HTTP Header Field
- **RFC 9110**: HTTP Semantics (RateLimit headers)
- **Semantic Versioning**: https://semver.org/
- **Stripe API Versioning**: https://stripe.com/docs/api/versioning (industry example)

## Revision History

| Version | Date       | Changes |
|---------|------------|---------|
| 1.0     | 2025-11-12 | Initial policy (P3.2 implementation) |
