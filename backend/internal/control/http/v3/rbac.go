// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/log"
)

// Scope defines a named permission for API access.
type Scope string

const (
	ScopeAll      Scope = "*"
	ScopeV3All    Scope = "v3:*"
	ScopeV3Read   Scope = "v3:read"
	ScopeV3Write  Scope = "v3:write"
	ScopeV3Admin  Scope = "v3:admin"
	ScopeV3Status Scope = "v3:status"
)

type scopeSet map[Scope]struct{}

func newScopeSet(scopes []string) scopeSet {
	set := scopeSet{}
	for _, scope := range normalizeScopes(scopes) {
		set[scope] = struct{}{}
	}
	applyImpliedScopes(set)
	return set
}

func normalizeScopes(scopes []string) []Scope {
	out := make([]Scope, 0, len(scopes))
	seen := map[Scope]struct{}{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(strings.ToLower(scope))
		if scope == "" {
			continue
		}
		canonical := Scope(scope)
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out
}

func applyImpliedScopes(set scopeSet) {
	if set == nil {
		return
	}
	if _, ok := set[ScopeAll]; ok {
		set[ScopeV3Admin] = struct{}{}
		set[ScopeV3Write] = struct{}{}
		set[ScopeV3Read] = struct{}{}
		return
	}
	if _, ok := set[ScopeV3All]; ok {
		set[ScopeV3Admin] = struct{}{}
		set[ScopeV3Write] = struct{}{}
		set[ScopeV3Read] = struct{}{}
	}
	if _, ok := set[ScopeV3Admin]; ok {
		set[ScopeV3Write] = struct{}{}
		set[ScopeV3Read] = struct{}{}
	}
	if _, ok := set[ScopeV3Write]; ok {
		set[ScopeV3Read] = struct{}{}
	}
}

func (s scopeSet) allows(required []Scope) bool {
	if len(required) == 0 {
		return true
	}
	return slices.ContainsFunc(required, s.has)
}

func (s scopeSet) has(scope Scope) bool {
	if s == nil {
		return false
	}
	if _, ok := s[ScopeAll]; ok {
		return true
	}
	if _, ok := s[ScopeV3All]; ok && strings.HasPrefix(string(scope), "v3:") {
		return true
	}
	_, ok := s[scope]
	return ok
}

// TokenPrincipal validates the token, projects active commercial entitlements,
// and returns the associated Principal.
func (s *Server) TokenPrincipal(ctx context.Context, token string) auth.Result {
	if token == "" {
		return auth.NoCredentials()
	}
	cfg := s.GetConfig()
	cfgToken := cfg.APIToken
	cfgTokenScopes := cfg.APITokenScopes
	cfgTokens := cfg.APITokens

	// 0. Check persistent Identity Web Sessions (WebAuthn / Passkeys / Recovery)
	if idSvc := s.getIdentityService(); idSvc != nil {
		if sess, user, err := idSvc.ValidateWebSession(ctx, token, "", ""); err == nil && sess != nil && user != nil {
			var scopes []string
			switch user.Role {
			case identity.RoleAdmin:
				scopes = []string{string(ScopeAll)}
			case identity.RoleMember:
				scopes = []string{string(ScopeV3Read), string(ScopeV3Write)}
			case identity.RoleViewer:
				scopes = []string{string(ScopeV3Read)}
			default:
				scopes = []string{string(ScopeV3Read)}
			}
			principal := auth.NewPrincipal(sess.SessionID, user.Username, scopes)
			principal = s.projectTokenPrincipal(ctx, principal, cfg)
			if principal != nil {
				return auth.Authenticated(principal)
			}
		}

		// 0b. Check DPoP Access Tokens (Android / Sender-Constrained API Clients)
		if req, ok := ctx.Value(dpopRequestContextKey{}).(*http.Request); ok && req != nil {
			dpopProof := req.Header.Get("DPoP")
			if dpopProof != "" {
				validator := s.getDPoPValidator()
				if validator != nil {
					proofClaims, err := validator.ValidateProof(req, dpopProof, token, time.Now().UTC())
					if err == nil && proofClaims != nil {
						if dpopToken, user, err := idSvc.ValidateDPoPAccessToken(ctx, token, proofClaims.JWKThumbprint); err == nil && dpopToken != nil && user != nil {
							var scopes []string
							if dpopToken.Scopes != "" {
								scopes = strings.Split(dpopToken.Scopes, " ")
							} else {
								scopes = []string{string(ScopeV3Read), string(ScopeV3Write)}
							}
							principal := auth.NewPrincipal(dpopToken.TokenHash, user.Username, scopes)
							principal = s.projectTokenPrincipal(ctx, principal, cfg)
							if principal != nil {
								return auth.Authenticated(principal)
							}
						}
					}
				}
			}
		}
	}

	// 1. Check legacy single token
	if cfgToken != "" && auth.AuthorizeToken(token, cfgToken) {
		if len(cfgTokenScopes) == 0 {
			// The token is genuine; the deployment granted it nothing.
			return auth.MisconfiguredToken()
		}
		return auth.Authenticated(s.projectTokenPrincipal(ctx, auth.NewPrincipal(token, "", cfgTokenScopes), cfg))
	}

	// 2. Check scoped tokens list
	for _, entry := range cfgTokens {
		if auth.AuthorizeToken(token, entry.Token) {
			if len(entry.Scopes) == 0 {
				return auth.MisconfiguredToken()
			}
			return auth.Authenticated(s.projectTokenPrincipal(ctx, auth.NewPrincipal(token, entry.User, entry.Scopes), cfg))
		}
	}

	if result := s.deviceAccessPrincipal(ctx, token, cfg); result.Outcome != auth.OutcomeInvalidCredentials {
		return result
	}

	return auth.InvalidCredentials()
}

func (s *Server) deviceAccessPrincipal(ctx context.Context, token string, cfg config.AppConfig) auth.Result {
	store := s.deviceAuthStore()
	if store == nil || token == "" {
		return auth.InvalidCredentials()
	}

	session, err := store.GetAccessSessionByTokenHash(ctx, deviceauthmodel.HashOpaqueSecret(token))
	if err != nil || session == nil {
		return auth.InvalidCredentials()
	}

	now := time.Now().UTC()
	if !session.IsActive(now) {
		return auth.InvalidCredentials()
	}

	device, err := store.GetDevice(ctx, session.DeviceID)
	if err != nil || device == nil || !device.CanIssueSessions(now) {
		return auth.InvalidCredentials()
	}

	principal := s.projectTokenPrincipal(ctx, auth.NewPrincipal(token, session.SubjectID, session.Scopes), cfg)
	if principal == nil {
		return auth.InvalidCredentials()
	}

	// The binding gate is deliberately NOT consulted here yet.
	//
	// Every credential in this store is cryptographically unbound — the schema
	// has no key column — so evaluating honestly would refuse every device
	// session, including a freshly paired one, before the bound issuance path
	// exists to replace it. That would be a hard cutover in the middle of the
	// implementation rather than in the finished behaviour.
	//
	// devicebinding.Evaluate and its transport mapping are built and tested;
	// they are simply not wired. The final step of the convergence replaces
	// this with an evaluation of the session's real binding state, at which
	// point an unbound credential is refused with DEVICE_REAUTH_REQUIRED and
	// there is no bearer fallback for a bound grant.
	return auth.Authenticated(principal)
}

func (s *Server) RequestScopes(r *http.Request) (scopeSet, bool) {
	// Optimisation: If authMiddleware already ran, get principal from context
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		return newScopeSet(p.Scopes), true
	}

	// Fallback for cases where authMiddleware might not have run (should not happen in protected routes)
	cfg := s.GetConfig()
	token, _ := s.extractTokenDetailedWithLegacyPolicy(r, !cfg.APIDisableLegacyTokenSources)
	if token != "" {
		if result := s.TokenPrincipal(r.Context(), token); result.OK() {
			return newScopeSet(result.Principal.Scopes), true
		}
		return nil, false
	}
	return nil, false
}

func (s *Server) projectTokenPrincipal(ctx context.Context, principal *auth.Principal, cfg config.AppConfig) *auth.Principal {
	if principal == nil {
		return nil
	}

	s.mu.RLock()
	entitlementService := s.entitlementService
	s.mu.RUnlock()
	if entitlementService == nil {
		return principal
	}

	requiredScopes := cfg.Monetization.Normalized().RequiredScopes
	scopes, err := entitlementService.EffectiveScopes(ctx, principal.ID, principal.Scopes, requiredScopes)
	if err != nil {
		logger := log.FromContext(ctx).With().Str("component", "authz.entitlements").Logger()
		logger.Error().Err(err).Str("principal_id", principal.ID).Msg("failed to project active entitlements onto principal")
		return nil
	}
	principal.Scopes = scopes
	return principal
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const bearerAuthScopesKey contextKey = "BearerAuth.Scopes" // #nosec G101 -- OpenAPI auth-scheme context key, not a credential.

const (
	operationIDKey    contextKey = "operation_id"
	exposurePolicyKey contextKey = "exposure_policy"
)

// ScopeMiddleware enforces that a request has at least one required scope.
func (s *Server) ScopeMiddleware(required ...Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scopes, ok := s.RequestScopes(r)

			if !ok {
				RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
				return
			}
			if !scopes.allows(required) {
				logger := log.FromContext(r.Context()).With().Str("component", "authz").Logger()
				logger.Warn().
					Interface("required_scopes", scopesToStrings(required)).
					Interface("token_scopes", scopeSetToStrings(scopes)).
					Msg("insufficient scopes for request")
				RespondError(w, r, http.StatusForbidden, ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ScopeMiddlewareFromContext enforces the bearer-auth scopes injected by the v3 router.
func (s *Server) ScopeMiddlewareFromContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := r.Context().Value(bearerAuthScopesKey).([]string)
		if !ok || len(raw) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		required := make([]Scope, 0, len(raw))
		for _, scope := range raw {
			if scope == "" {
				continue
			}
			required = append(required, Scope(scope))
		}
		if len(required) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		s.ScopeMiddleware(required...)(next).ServeHTTP(w, r)
	})
}

func scopesToStrings(scopes []Scope) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

func scopeSetToStrings(scopes scopeSet) []string {
	if scopes == nil {
		return nil
	}
	out := make([]string, 0, len(scopes))
	for scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}
