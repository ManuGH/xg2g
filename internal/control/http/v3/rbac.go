// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
)

// Scope defines a named permission for API access.
type Scope string

const (
	ScopeAll     Scope = "*"
	ScopeV3All   Scope = "v3:*"
	ScopeV3Read  Scope = "v3:read"
	ScopeV3Write Scope = "v3:write"
	ScopeV3Admin Scope = "v3:admin"
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
	for _, scope := range required {
		if s.has(scope) {
			return true
		}
	}
	return false
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

// TokenPrincipal validates the token and returns the associated Principal.
func (s *Server) TokenPrincipal(token string) (*auth.Principal, bool) {
	if token == "" {
		return nil, false
	}
	cfg := s.GetConfig()
	cfgToken := cfg.APIToken
	cfgTokenScopes := cfg.APITokenScopes
	cfgTokens := cfg.APITokens

	// 1. Check legacy single token
	if cfgToken != "" && auth.AuthorizeToken(token, cfgToken) {
		if len(cfgTokenScopes) == 0 {
			return nil, false
		}
		// Single token has no explicit user field in config, so we use empty user (hash-based ID)
		return auth.NewPrincipal(token, "", cfgTokenScopes), true
	}

	// 2. Check scoped tokens list
	for _, entry := range cfgTokens {
		if auth.AuthorizeToken(token, entry.Token) {
			if len(entry.Scopes) == 0 {
				return nil, false
			}
			return auth.NewPrincipal(token, entry.User, entry.Scopes), true
		}
	}

	return nil, false
}

func (s *Server) RequestScopes(r *http.Request) (scopeSet, bool) {
	// Optimisation: If authMiddleware already ran, get principal from context
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		return newScopeSet(p.Scopes), true
	}

	// Fallback for cases where authMiddleware might not have run (should not happen in protected routes)
	token := extractToken(r)
	if token != "" {
		p, ok := s.TokenPrincipal(token)
		if ok {
			return newScopeSet(p.Scopes), true
		}
		return nil, false
	}
	return nil, false
}

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
