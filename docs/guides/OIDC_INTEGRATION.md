# OIDC Integration for xg2g

## Overview

This document outlines the design and implementation strategy for OpenID Connect (OIDC) authentication in xg2g, enabling cloud-native deployments with centralized identity management.

## Current State

### Existing Authentication

xg2g currently implements **simple token-based authentication**:

```go
// Location: internal/api/http.go:186-216, 832-859
// Header: X-API-Token
// Config: XG2G_API_TOKEN environment variable
```

**Current Flow:**
1. Client sends request with `X-API-Token: <token>` header
2. Server validates against `XG2G_API_TOKEN` using constant-time comparison
3. Access granted/denied based on match

**Limitations:**
- Single shared secret (no per-user auth)
- No token expiration or rotation
- Manual distribution required
- No audit trail of individual users
- Not suitable for multi-tenant or enterprise deployments

## Why OIDC?

### Benefits

1. **Centralized Identity Management**: Single source of truth for users
2. **Enterprise Integration**: Works with existing SSO providers (Google Workspace, Azure AD, Okta)
3. **Security**: Token expiration, refresh, revocation
4. **Audit**: Per-user access logs
5. **Scalability**: No shared secrets to distribute
6. **Standards-Based**: RFC 7519 (JWT), RFC 6749 (OAuth 2.0)

### Cloud Provider Support

All major cloud platforms provide OIDC endpoints:

| Provider | Service | Default Issuer |
|----------|---------|----------------|
| **Google Cloud** | Identity Platform | `https://accounts.google.com` |
| **AWS** | Cognito | `https://cognito-idp.{region}.amazonaws.com/{userPoolId}` |
| **Azure** | Active Directory | `https://login.microsoftonline.com/{tenantId}/v2.0` |
| **Keycloak** | Self-hosted | `https://{your-domain}/realms/{realm}` |

## Architecture Design

### Dual-Mode Authentication

Support **both** simple token and OIDC to maintain backward compatibility:

```
┌─────────────────────────────────────────────────────────────┐
│                      Incoming Request                        │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
          ┌──────────────────────────────┐
          │  Check X-API-Token header?   │
          └──────┬───────────────┬────────┘
                 │               │
           Yes   │               │  No
                 ▼               ▼
        ┌──────────────┐  ┌──────────────┐
        │ Simple Token │  │ Check Bearer │
        │ Validation   │  │ Token (JWT)  │
        └──────┬───────┘  └──────┬───────┘
               │                  │
               ▼                  ▼
        ┌──────────────┐  ┌──────────────┐
        │ Validate vs  │  │ Validate JWT │
        │ XG2G_API_*   │  │ Signature    │
        └──────┬───────┘  └──────┬───────┘
               │                  │
               └────────┬─────────┘
                        ▼
                ┌──────────────┐
                │ Grant Access │
                └──────────────┘
```

### Configuration Model

New environment variables:

```bash
# Existing (retained for compatibility)
XG2G_API_TOKEN=simple-shared-secret

# New OIDC configuration
XG2G_OIDC_ENABLED=true
XG2G_OIDC_ISSUER=https://accounts.google.com
XG2G_OIDC_AUDIENCE=xg2g-api
XG2G_OIDC_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs  # Optional, auto-discovered
XG2G_OIDC_CLAIMS_EMAIL=email                                    # Claim for user identity
XG2G_OIDC_ALLOWED_DOMAINS=example.com,example.org              # Optional email domain filter
```

### Middleware Stack (Updated)

```go
// internal/api/middleware.go (enhancement)

func withMiddlewares(h http.Handler) http.Handler {
    return chain(h,
        panicRecoveryMiddleware,
        requestIDMiddleware,
        metricsMiddleware,
        corsMiddleware,
        securityHeaders,
        rl.middleware,
        // NEW: Add OIDC middleware before rate limiting
        oidcMiddleware,  // <-- New middleware
    )
}
```

### JWT Validation Flow

```go
// Pseudo-code for OIDC middleware

func oidcMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 1. Extract Bearer token
        authHeader := r.Header.Get("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            // Fall back to simple token auth or public endpoints
            next.ServeHTTP(w, r)
            return
        }

        tokenString := strings.TrimPrefix(authHeader, "Bearer ")

        // 2. Parse JWT (without verification yet)
        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            // 3. Fetch JWKS from issuer's .well-known endpoint
            set := cachedJWKS(issuerURL)

            // 4. Find matching key by "kid" (key ID)
            key := set.Key(token.Header["kid"])
            return key.PublicKey, nil
        })

        // 5. Verify signature, expiration, audience, issuer
        if err != nil || !token.Valid {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        // 6. Extract claims (email, sub, etc.)
        claims := token.Claims.(jwt.MapClaims)
        email := claims["email"].(string)

        // 7. Optional: Check allowed domains
        if !isAllowedDomain(email) {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }

        // 8. Add user context for logging
        ctx := context.WithValue(r.Context(), "user_email", email)

        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

## Implementation Plan

### Phase 1: Core OIDC Validation (Week 1)

**Goal**: Validate JWT tokens from any OIDC provider

**Tasks:**
1. Add dependency: `github.com/golang-jwt/jwt/v5`
2. Create `internal/auth/oidc.go` package:
   - `NewOIDCValidator(issuer, audience string) *OIDCValidator`
   - `ValidateToken(tokenString string) (*Claims, error)`
   - JWKS fetching and caching (refresh every 24h)
3. Add configuration parsing in `internal/config/config.go`
4. Implement OIDC middleware in `internal/api/middleware.go`
5. Write tests with mock OIDC server

**Dependencies:**
```go
// go.mod additions
require (
    github.com/golang-jwt/jwt/v5 v5.2.0
    github.com/lestrrat-go/jwx/v2 v2.0.18  // For JWKS fetching
)
```

**Deliverable**: JWT validation working with Google OIDC tokens

### Phase 2: Provider Templates (Week 2)

**Goal**: Provide copy-paste configs for major providers

**Create:** `docs/auth/` directory with provider-specific guides:

- `docs/auth/google-cloud.md` - Google Workspace integration
- `docs/auth/aws-cognito.md` - AWS Cognito setup
- `docs/auth/azure-ad.md` - Azure Active Directory
- `docs/auth/keycloak.md` - Self-hosted Keycloak

**Each guide includes:**
1. Provider setup (create OAuth app, get client ID)
2. xg2g configuration snippet
3. Example curl command with token
4. Troubleshooting checklist

### Phase 3: Advanced Features (Week 3)

**3.1 Claims-Based Authorization**

Support role-based access:

```bash
XG2G_OIDC_CLAIM_ROLE=groups
XG2G_OIDC_ALLOWED_ROLES=xg2g-admin,xg2g-viewer
```

**3.2 Multi-Issuer Support**

Allow multiple identity providers:

```yaml
# config.yaml (future enhancement)
oidc:
  issuers:
    - name: google
      issuer: https://accounts.google.com
      audience: xg2g-prod
    - name: corporate-ad
      issuer: https://login.microsoftonline.com/tenant-id/v2.0
      audience: api://xg2g
```

**3.3 Token Introspection**

Cache validated tokens to reduce JWKS lookups:

```go
// In-memory cache with TTL
type TokenCache struct {
    cache map[string]*CachedToken  // token hash -> claims
    ttl   time.Duration             // Max 5 minutes
}
```

### Phase 4: Observability (Week 4)

**Metrics:**
```go
// Prometheus metrics
oidc_token_validations_total{provider="google",result="success"}
oidc_token_validations_total{provider="google",result="expired"}
oidc_token_validations_total{provider="google",result="invalid_signature"}
oidc_jwks_refresh_total{provider="google",result="success"}
```

**Logging:**
```json
{
  "event": "oidc.token_validated",
  "user_email": "user@example.com",
  "provider": "google",
  "duration_ms": 12,
  "request_id": "abc-123"
}
```

**Health Check:**
```go
// GET /healthz response includes OIDC status
{
  "status": "ok",
  "oidc": {
    "enabled": true,
    "issuer": "https://accounts.google.com",
    "jwks_last_refresh": "2025-11-01T10:00:00Z",
    "jwks_keys_count": 3
  }
}
```

## Migration Guide

### For Existing Deployments

**Step 1: Enable OIDC alongside existing tokens**

```bash
# .env file
XG2G_API_TOKEN=existing-token-keep-for-scripts  # Keep for backward compat
XG2G_OIDC_ENABLED=true
XG2G_OIDC_ISSUER=https://accounts.google.com
XG2G_OIDC_AUDIENCE=xg2g-api
```

**Step 2: Test OIDC with new clients**

```bash
# Get token from your provider (example: Google)
TOKEN=$(gcloud auth print-identity-token --audiences=xg2g-api)

# Test API with OIDC
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/status
```

**Step 3: Gradually migrate scripts**

```bash
# Old scripts (still work)
curl -H "X-API-Token: $XG2G_API_TOKEN" http://localhost:8080/api/refresh

# New scripts (OIDC)
curl -H "Authorization: Bearer $OIDC_TOKEN" http://localhost:8080/api/refresh
```

**Step 4: Disable simple token (optional)**

```bash
# After all clients migrated
unset XG2G_API_TOKEN  # Forces OIDC-only
```

## Security Considerations

### Token Storage

**DON'T:**
- Store OIDC tokens in environment variables (they're short-lived)
- Log full JWT tokens (contains user info)
- Share tokens between clients

**DO:**
- Fetch tokens on-demand from provider
- Use service accounts for automated tasks
- Implement token refresh when expired

### Attack Mitigation

| Attack | Mitigation |
|--------|------------|
| **Token theft** | Short expiration (1h max), HTTPS only |
| **Replay attacks** | Check `exp` claim, use request IDs |
| **JWKS poisoning** | Pin issuer URL, verify HTTPS cert |
| **Algorithm confusion** | Only accept RS256/ES256 (no HS256) |
| **Audience bypass** | Always validate `aud` claim |

### Fail-Safe Design

```go
// If OIDC validation fails due to network/config error:
// 1. Log error with high severity
// 2. Deny access (fail-closed)
// 3. Return 503 Service Unavailable (not 401)
// 4. Include retry-after header

if err := fetchJWKS(); err != nil {
    log.Error().Err(err).Msg("OIDC JWKS fetch failed - denying access")
    w.Header().Set("Retry-After", "60")
    http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
    return
}
```

## Example: Google Cloud Run Deployment

### 1. Enable Google OIDC

```yaml
# cloud-run-service.yaml
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: xg2g
spec:
  template:
    spec:
      containers:
        - image: ghcr.io/manugh/xg2g:latest
          env:
            - name: XG2G_OIDC_ENABLED
              value: "true"
            - name: XG2G_OIDC_ISSUER
              value: "https://accounts.google.com"
            - name: XG2G_OIDC_AUDIENCE
              value: "xg2g-production"
            - name: XG2G_OIDC_ALLOWED_DOMAINS
              value: "example.com"
```

### 2. Client Authentication

```bash
# Service account auth
gcloud auth print-identity-token \
  --audiences=xg2g-production \
  --impersonate-service-account=xg2g-client@project.iam.gserviceaccount.com

# User auth (interactive)
gcloud auth login
TOKEN=$(gcloud auth print-identity-token --audiences=xg2g-production)
curl -H "Authorization: Bearer $TOKEN" https://xg2g-prod.run.app/api/status
```

### 3. IAM Policy

```bash
# Grant access to specific service account
gcloud run services add-iam-policy-binding xg2g \
  --member='serviceAccount:xg2g-client@project.iam.gserviceaccount.com' \
  --role='roles/run.invoker'
```

## Example: AWS Cognito Setup

### 1. Create Cognito User Pool

```bash
aws cognito-idp create-user-pool \
  --pool-name xg2g-users \
  --auto-verified-attributes email \
  --username-attributes email

# Output: UserPool.Id = eu-west-1_ABC123
```

### 2. Create App Client

```bash
aws cognito-idp create-user-pool-client \
  --user-pool-id eu-west-1_ABC123 \
  --client-name xg2g-api \
  --generate-secret \
  --explicit-auth-flows ALLOW_USER_PASSWORD_AUTH ALLOW_REFRESH_TOKEN_AUTH

# Output: ClientId, ClientSecret
```

### 3. Configure xg2g

```bash
export XG2G_OIDC_ENABLED=true
export XG2G_OIDC_ISSUER=https://cognito-idp.eu-west-1.amazonaws.com/eu-west-1_ABC123
export XG2G_OIDC_AUDIENCE=<ClientId>
```

### 4. Get Token

```bash
# Create user
aws cognito-idp admin-create-user \
  --user-pool-id eu-west-1_ABC123 \
  --username user@example.com

# Authenticate
aws cognito-idp initiate-auth \
  --auth-flow USER_PASSWORD_AUTH \
  --client-id <ClientId> \
  --auth-parameters USERNAME=user@example.com,PASSWORD=<Password>

# Extract IdToken from response
```

## Testing Strategy

### Unit Tests

```go
// internal/auth/oidc_test.go
func TestOIDCValidator_ValidateToken(t *testing.T) {
    tests := []struct {
        name    string
        token   string
        wantErr bool
    }{
        {
            name:    "valid token",
            token:   testGenerateJWT(t, validClaims),
            wantErr: false,
        },
        {
            name:    "expired token",
            token:   testGenerateJWT(t, expiredClaims),
            wantErr: true,
        },
        {
            name:    "wrong audience",
            token:   testGenerateJWT(t, wrongAudClaims),
            wantErr: true,
        },
    }
    // ...
}
```

### Integration Tests

```go
// test/integration/oidc_test.go
func TestOIDCAuthentication(t *testing.T) {
    // Start mock OIDC server
    mockServer := httptest.NewServer(mockOIDCHandler())
    defer mockServer.Close()

    // Configure xg2g with mock issuer
    os.Setenv("XG2G_OIDC_ISSUER", mockServer.URL)

    // Test valid token
    token := generateMockJWT(mockServer.URL)
    resp := testRequest(t, "GET", "/api/status", token)
    assert.Equal(t, 200, resp.StatusCode)
}
```

### Manual Testing

```bash
# Test with jwt.io generated token
# 1. Go to https://jwt.io
# 2. Generate token with correct issuer/audience
# 3. Use RS256 algorithm
# 4. Test against xg2g:

curl -v \
  -H "Authorization: Bearer eyJhbGc..." \
  http://localhost:8080/api/status
```

## Performance Impact

### Benchmarks (Expected)

| Operation | Latency | Notes |
|-----------|---------|-------|
| **JWKS fetch** | 200-500ms | Once per 24h (cached) |
| **JWT parse** | 0.1-0.5ms | Per request |
| **Signature verify** | 0.5-2ms | Per request (RSA) |
| **Claims extract** | 0.05ms | Per request |
| **Total overhead** | ~2-3ms | Acceptable for API |

### Optimization

1. **JWKS caching**: Fetch once, cache 24h
2. **Token caching**: Hash valid tokens, cache 5min
3. **Concurrent validation**: Use goroutines for batch requests

```go
// Optimized validation with caching
type OIDCValidator struct {
    jwksCache  *JWKSCache    // 24h TTL
    tokenCache *TokenCache   // 5min TTL
}

func (v *OIDCValidator) ValidateToken(token string) (*Claims, error) {
    // Check token cache first
    if claims, ok := v.tokenCache.Get(hash(token)); ok {
        return claims, nil
    }

    // Full validation
    claims, err := v.validate(token)
    if err == nil {
        v.tokenCache.Set(hash(token), claims, 5*time.Minute)
    }
    return claims, err
}
```

## Documentation Updates Required

1. **README.md**: Add OIDC section to deployment modes
2. **docs/CONFIGURATION.md**: Document new env vars
3. **docs/SECURITY_HARDENING.md**: Add OIDC best practices
4. **api/openapi.yaml**: Add Bearer security scheme
5. **docs/PRODUCTION.md**: Add cloud deployment examples

## Backward Compatibility

### Deprecation Timeline

| Version | Status | Notes |
|---------|--------|-------|
| **v1.7.0** | Both auth methods supported | OIDC introduced |
| **v1.8.0** | Simple token deprecated | Warning logs |
| **v2.0.0** | OIDC only (breaking change) | Remove XG2G_API_TOKEN |

### Migration Path

```bash
# v1.7.0 - Both methods work
XG2G_API_TOKEN=legacy-token         # Still works
XG2G_OIDC_ENABLED=true              # Also works

# v1.8.0 - Warning when using simple token
2025-11-15T10:00:00Z WARN simple token auth is deprecated, migrate to OIDC

# v2.0.0 - Simple token removed
XG2G_API_TOKEN=legacy-token         # Ignored
XG2G_OIDC_ENABLED=true              # Required
```

## Open Questions

1. **Token refresh**: Should xg2g handle refresh tokens? (Probably not - client responsibility)
2. **Introspection endpoint**: Support RFC 7662 for opaque tokens? (Future enhancement)
3. **mTLS**: Combine OIDC with mutual TLS? (Enterprise feature)
4. **RBAC**: Full role-based access control? (Phase 3+)

## References

- [RFC 7519 - JWT](https://datatracker.ietf.org/doc/html/rfc7519)
- [RFC 6749 - OAuth 2.0](https://datatracker.ietf.org/doc/html/rfc6749)
- [OpenID Connect Core](https://openid.net/specs/openid-connect-core-1_0.html)
- [Google OIDC Documentation](https://developers.google.com/identity/protocols/oauth2/openid-connect)
- [AWS Cognito Developer Guide](https://docs.aws.amazon.com/cognito/latest/developerguide/)
- [Azure AD OIDC](https://learn.microsoft.com/en-us/azure/active-directory/develop/v2-protocols-oidc)

---

**Last Updated**: 2025-11-01
**Owner**: @ManuGH
**Status**: 📋 Design Phase
**Implementation**: Not started (awaiting approval)
