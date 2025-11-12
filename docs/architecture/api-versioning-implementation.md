# API Versioning Implementation Guide

This document provides practical code examples for implementing API versioning in xg2g according to the [API Versioning Policy](api-versioning-policy.md).

## Current State (Pre-Versioning)

The xg2g codebase currently serves endpoints at the root level:

```
/lineup.json       → HDHomeRun discovery
/discover.json     → Device info
/lineup_status.json
/auto/<channel>    → Stream proxy (smart detection)
```

**Migration Strategy:** Introduce `/api/v1/` prefix while maintaining backward compatibility via redirects.

## Phase 1: Add v1 Prefix (Non-Breaking)

### Step 1: Create Version Router

```go
// internal/api/server.go
func (s *Server) setupRoutes() {
    // Root version discovery
    s.router.Get("/api", s.handleVersionDiscovery)

    // v1 routes (current stable)
    s.setupV1Routes(s.router.Route("/api/v1", nil))

    // Backward compatibility redirects (deprecated)
    s.setupLegacyRedirects()
}

func (s *Server) setupV1Routes(r chi.Router) {
    // Apply v1-specific middleware (no deprecation yet)
    r.Use(versionHeaderMiddleware("v1", "stable"))

    // HDHomeRun discovery
    r.Get("/lineup.json", s.handleLineup)
    r.Get("/discover.json", s.handleDiscover)
    r.Get("/lineup_status.json", s.handleLineupStatus)

    // Stream endpoints
    r.Get("/auto/{channel}", s.handleAutoStream)
    r.Get("/proxy/{channel}", s.handleProxyStream)
    r.Get("/transcode/{channel}", s.handleTranscodeStream)
}

func (s *Server) setupLegacyRedirects() {
    // Redirect root-level endpoints to /api/v1/ with 308 Permanent Redirect
    redirectToV1 := func(path string) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            target := "/api/v1" + path
            w.Header().Set("Location", target)
            w.Header().Set("Deprecation", "true")
            w.Header().Set("Link", "</docs/migration/legacy-to-v1>; rel=\"deprecation\"")
            http.Error(w, fmt.Sprintf("Moved to %s", target), http.StatusPermanentRedirect)
        }
    }

    s.router.Get("/lineup.json", redirectToV1("/lineup.json"))
    s.router.Get("/discover.json", redirectToV1("/discover.json"))
    s.router.Get("/lineup_status.json", redirectToV1("/lineup_status.json"))
    // Note: /auto/, /proxy/, /transcode/ kept at root for HDHomeRun compatibility
}

func (s *Server) handleVersionDiscovery(w http.ResponseWriter, r *http.Request) {
    versions := map[string]any{
        "versions": map[string]any{
            "v1": map[string]string{
                "status": "stable",
                "docs":   "/docs/api/v1",
                "base":   "/api/v1",
            },
        },
        "recommended": "v1",
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(versions)
}

func versionHeaderMiddleware(version, status string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("X-API-Version", version)
            w.Header().Set("X-API-Status", status)
            next.ServeHTTP(w, r)
        })
    }
}
```

### Step 2: Update Tests

```go
// internal/api/server_test.go
func TestAPIVersioning(t *testing.T) {
    srv := newTestServer(t)

    tests := []struct {
        name           string
        path           string
        wantStatus     int
        wantAPIVersion string
        wantLocation   string
    }{
        {
            name:           "v1 lineup",
            path:           "/api/v1/lineup.json",
            wantStatus:     http.StatusOK,
            wantAPIVersion: "v1",
        },
        {
            name:           "legacy lineup redirects",
            path:           "/lineup.json",
            wantStatus:     http.StatusPermanentRedirect,
            wantLocation:   "/api/v1/lineup.json",
        },
        {
            name:       "version discovery",
            path:       "/api",
            wantStatus: http.StatusOK,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", tt.path, nil)
            rec := httptest.NewRecorder()
            srv.ServeHTTP(rec, req)

            if rec.Code != tt.wantStatus {
                t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
            }

            if tt.wantAPIVersion != "" {
                got := rec.Header().Get("X-API-Version")
                if got != tt.wantAPIVersion {
                    t.Errorf("X-API-Version = %q, want %q", got, tt.wantAPIVersion)
                }
            }

            if tt.wantLocation != "" {
                got := rec.Header().Get("Location")
                if got != tt.wantLocation {
                    t.Errorf("Location = %q, want %q", got, tt.wantLocation)
                }
            }
        })
    }
}
```

## Phase 2: Introduce v2 with Breaking Changes

### Example: Change Response Schema

**Scenario:** v2 uses snake_case JSON keys instead of PascalCase.

```go
// internal/api/models.go

// LineupResponseV1 is the HDHomeRun-compatible format (PascalCase)
type LineupResponseV1 struct {
    GuideNumber string `json:"GuideNumber"`
    GuideName   string `json:"GuideName"`
    URL         string `json:"URL"`
}

// LineupResponseV2 uses standard REST conventions (snake_case)
type LineupResponseV2 struct {
    GuideNumber string `json:"guide_number"`
    GuideName   string `json:"guide_name"`
    URL         string `json:"url"`
    APIVersion  string `json:"api_version"` // New field
}

// LineupService provides version-agnostic business logic
type LineupService struct {
    bouquetLoader BouquetLoader
}

func (s *LineupService) GetChannels(ctx context.Context) ([]Channel, error) {
    // Version-agnostic logic
    return s.bouquetLoader.Load(ctx)
}
```

### Step 3: Implement v2 Handlers

```go
// internal/api/handlers_v1.go
func (s *Server) handleLineupV1(w http.ResponseWriter, r *http.Request) {
    channels, err := s.lineupService.GetChannels(r.Context())
    if err != nil {
        http.Error(w, "Failed to load lineup", http.StatusInternalServerError)
        return
    }

    // Transform to v1 format
    v1Response := make([]LineupResponseV1, len(channels))
    for i, ch := range channels {
        v1Response[i] = LineupResponseV1{
            GuideNumber: ch.Number,
            GuideName:   ch.Name,
            URL:         fmt.Sprintf("http://%s/auto/%s", r.Host, ch.Reference),
        }
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(v1Response)
}

// internal/api/handlers_v2.go
func (s *Server) handleLineupV2(w http.ResponseWriter, r *http.Request) {
    channels, err := s.lineupService.GetChannels(r.Context())
    if err != nil {
        // v2 uses RFC 7807 Problem Details
        s.sendProblemJSON(w, http.StatusInternalServerError, "Failed to load lineup", err)
        return
    }

    // Transform to v2 format
    v2Response := make([]LineupResponseV2, len(channels))
    for i, ch := range channels {
        v2Response[i] = LineupResponseV2{
            GuideNumber: ch.Number,
            GuideName:   ch.Name,
            URL:         fmt.Sprintf("http://%s/api/v2/stream/%s", r.Host, ch.Reference),
            APIVersion:  "v2",
        }
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(v2Response)
}

func (s *Server) sendProblemJSON(w http.ResponseWriter, status int, title string, err error) {
    problem := map[string]any{
        "type":   "about:blank",
        "title":  title,
        "status": status,
        "detail": err.Error(),
    }
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(problem)
}
```

### Step 4: Update Router with Deprecation

```go
func (s *Server) setupRoutes() {
    s.router.Get("/api", s.handleVersionDiscovery)

    // v1 (deprecated as of 2025-10-01)
    v1Router := s.router.Route("/api/v1", nil)
    v1Router.Use(deprecationMiddleware("v1", "2026-04-01T00:00:00Z"))
    s.setupV1Routes(v1Router)

    // v2 (stable)
    v2Router := s.router.Route("/api/v2", nil)
    v2Router.Use(versionHeaderMiddleware("v2", "stable"))
    s.setupV2Routes(v2Router)
}

func (s *Server) setupV2Routes(r chi.Router) {
    r.Get("/lineup.json", s.handleLineupV2)
    r.Get("/discover.json", s.handleDiscoverV2)
    r.Get("/stream/{channel}", s.handleStreamV2) // Unified endpoint
}

func deprecationMiddleware(version, sunsetISO string) func(http.Handler) http.Handler {
    sunsetTime, _ := time.Parse(time.RFC3339, sunsetISO)
    sunsetHTTP := sunsetTime.Format(time.RFC1123)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Sunset", sunsetHTTP)
            w.Header().Set("Deprecation", "true")
            w.Header().Set("Link", "</docs/api-versioning-policy#migration-v1-to-v2>; rel=\"sunset\"")
            w.Header().Set("Warning", fmt.Sprintf(
                "299 - \"API %s is deprecated. Migrate to /api/v2/ by %s\"",
                version, sunsetTime.Format("2006-01-02"),
            ))

            // Audit logging
            log.WithComponentFromContext(r.Context(), "api").Warn().
                Str("event", "api.deprecated_endpoint").
                Str("version", version).
                Str("path", r.URL.Path).
                Str("client_ip", clientIP(r)).
                Str("sunset_date", sunsetISO).
                Msg("deprecated API endpoint accessed")

            next.ServeHTTP(w, r)
        })
    }
}
```

## Phase 3: Monitor and Sunset v1

### Prometheus Metrics

Add version labels to existing metrics:

```go
// internal/api/metrics.go
var (
    httpRequestsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "http_requests_total",
            Help: "Total HTTP requests",
        },
        []string{"method", "path", "status", "api_version"}, // ← Add version label
    )
)

func recordHTTPMetric(path string, status int, apiVersion string) {
    httpRequestsTotal.WithLabelValues(
        "GET",
        path,
        strconv.Itoa(status),
        apiVersion, // ← Capture version
    ).Inc()
}

// Update metricsMiddleware to extract version
func metricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
        next.ServeHTTP(recorder, r)

        // Extract API version from path
        apiVersion := "legacy"
        if strings.HasPrefix(r.URL.Path, "/api/v1/") {
            apiVersion = "v1"
        } else if strings.HasPrefix(r.URL.Path, "/api/v2/") {
            apiVersion = "v2"
        }

        recordHTTPMetric(r.URL.Path, recorder.status, apiVersion)
    })
}
```

### Grafana Queries

```promql
# Percentage of v1 traffic
sum(rate(http_requests_total{api_version="v1"}[5m])) /
sum(rate(http_requests_total{api_version=~"v1|v2"}[5m])) * 100

# Unique IPs hitting v1 (requires additional metric)
count(count by (client_ip) (http_requests_total{api_version="v1"}))
```

### Sunset Checklist

Before removing v1 (T+6 months):

- [ ] v1 traffic < 5% for 30 consecutive days
- [ ] All known clients contacted (if email list exists)
- [ ] Migration guide published and linked in docs
- [ ] `410 Gone` handler implemented and tested
- [ ] Runbook updated for rollback procedure

## Testing Strategy

### Contract Tests for Both Versions

```go
// internal/api/contract_test.go
func TestLineupContractV1(t *testing.T) {
    srv := newTestServer(t)
    rec := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/api/v1/lineup.json", nil)
    srv.ServeHTTP(rec, req)

    require.Equal(t, http.StatusOK, rec.Code)

    var lineup []LineupResponseV1
    err := json.NewDecoder(rec.Body).Decode(&lineup)
    require.NoError(t, err)

    // Validate PascalCase keys
    if len(lineup) > 0 {
        assert.NotEmpty(t, lineup[0].GuideNumber, "v1 uses PascalCase")
    }
}

func TestLineupContractV2(t *testing.T) {
    srv := newTestServer(t)
    rec := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/api/v2/lineup.json", nil)
    srv.ServeHTTP(rec, req)

    require.Equal(t, http.StatusOK, rec.Code)

    var lineup []LineupResponseV2
    err := json.NewDecoder(rec.Body).Decode(&lineup)
    require.NoError(t, err)

    // Validate snake_case keys + new fields
    if len(lineup) > 0 {
        assert.NotEmpty(t, lineup[0].GuideNumber, "v2 uses snake_case")
        assert.Equal(t, "v2", lineup[0].APIVersion, "v2 includes version field")
    }
}
```

### Deprecation Header Tests

```go
func TestDeprecationHeaders(t *testing.T) {
    srv := newTestServer(t)
    rec := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/api/v1/lineup.json", nil)
    srv.ServeHTTP(rec, req)

    headers := rec.Header()
    assert.Equal(t, "true", headers.Get("Deprecation"))
    assert.NotEmpty(t, headers.Get("Sunset"), "Sunset header required")
    assert.Contains(t, headers.Get("Link"), "rel=\"sunset\"")
    assert.Contains(t, headers.Get("Warning"), "299")
}

func TestV2NoDeprecation(t *testing.T) {
    srv := newTestServer(t)
    rec := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/api/v2/lineup.json", nil)
    srv.ServeHTTP(rec, req)

    assert.Empty(t, rec.Header().Get("Deprecation"), "v2 should not be deprecated")
    assert.Empty(t, rec.Header().Get("Sunset"))
}
```

## Rollout Timeline Example

| Date       | Milestone | Actions |
|------------|-----------|---------|
| 2025-09-15 | v1 prefix | Add `/api/v1/` routes, legacy redirects |
| 2025-10-01 | v2 launch | Release `/api/v2/`, mark v1 deprecated |
| 2025-12-01 | Review | Check v1 usage metrics, extend sunset if needed |
| 2026-02-01 | Final warning | Update Warning header, email notifications |
| 2026-04-01 | Sunset | Remove v1 routes, return 410 Gone |

## Common Pitfalls

### ❌ Mistake: Version in Query Parameter

```go
// BAD: /api/lineup.json?version=2
// - Hard to route
// - Cache key ambiguity
// - Not RESTful
```

### ❌ Mistake: Breaking Changes Without New Version

```go
// BAD: Changing v1 response schema
// - Violates compatibility guarantee
// - Breaks existing clients without warning
```

### ❌ Mistake: Too Many Active Versions

```go
// BAD: Supporting v1, v2, v3, v4 simultaneously
// - High maintenance burden
// - Test matrix explosion
// Maximum: 2 concurrent versions (current + deprecated)
```

### ✅ Best Practice: Share Business Logic

```go
// GOOD: Version-specific handlers call shared service layer
type LineupService struct { /* version-agnostic */ }

func (s *Server) handleLineupV1(w, r) {
    data := s.lineupService.Get()
    respond(w, toV1Format(data))
}

func (s *Server) handleLineupV2(w, r) {
    data := s.lineupService.Get()
    respond(w, toV2Format(data))
}
```

## Integration with CI/CD

### Contract Test in Pipeline

```yaml
# .github/workflows/api-contract-tests.yml
name: API Contract Tests
on: [pull_request]

jobs:
  contract:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run contract tests
        run: go test -v -tags=contract ./internal/api/...

      - name: Validate OpenAPI spec (v2)
        run: |
          npx @redocly/cli lint docs/openapi-v2.yaml
```

## References

- [API Versioning Policy](api-versioning-policy.md)
- [chi Router Documentation](https://github.com/go-chi/chi)
- [RFC 7807 - Problem Details](https://tools.ietf.org/html/rfc7807)
- [Stripe API Versioning Example](https://stripe.com/docs/api/versioning)
