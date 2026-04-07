package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/log"
)

// GetSystemEntitlements implements ServerInterface.
func (s *Server) GetSystemEntitlements(w http.ResponseWriter, r *http.Request, params GetSystemEntitlementsParams) {
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	}

	targetPrincipalID, baseScopes, err := s.resolveEntitlementStatusTarget(principal, params)
	if err != nil {
		RespondError(w, r, http.StatusForbidden, ErrForbidden, err.Error())
		return
	}

	status, err := s.buildEntitlementStatus(r.Context(), targetPrincipalID, baseScopes)
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Str("principal_id", targetPrincipalID).Msg("failed to build entitlement status")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to resolve entitlement status")
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// PostSystemEntitlementOverride implements ServerInterface.
func (s *Server) PostSystemEntitlementOverride(w http.ResponseWriter, r *http.Request) {
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	}

	var req EntitlementOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "override request body must be valid JSON")
		return
	}

	normalizedMonetization := s.GetConfig().Monetization.Normalized()
	allowedScopes := normalizedMonetization.RequiredScopes
	if len(allowedScopes) == 0 {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "monetization.requiredScopes must be configured before granting overrides")
		return
	}

	targetPrincipalID := principal.ID
	if req.PrincipalId != nil && strings.TrimSpace(*req.PrincipalId) != "" {
		targetPrincipalID = strings.TrimSpace(*req.PrincipalId)
	}

	overrideScopes, err := validateEntitlementOverrideScopes(req.Scopes, allowedScopes)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	expiresAt, err := parseEntitlementOverrideExpiry(req.ExpiresAt)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	service := s.entitlementServiceSnapshot()
	for _, scope := range overrideScopes {
		if err := service.Grant(r.Context(), entitlements.Grant{
			PrincipalID: targetPrincipalID,
			Scope:       scope,
			Source:      entitlements.SourceAdminOverride,
			ExpiresAt:   expiresAt,
		}); err != nil {
			log.FromContext(r.Context()).Error().Err(err).Str("principal_id", targetPrincipalID).Str("scope", scope).Msg("failed to grant entitlement override")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to grant entitlement override")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteSystemEntitlementOverride implements ServerInterface.
func (s *Server) DeleteSystemEntitlementOverride(w http.ResponseWriter, r *http.Request, principalId string, scope string) {
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	}

	normalizedMonetization := s.GetConfig().Monetization.Normalized()
	if len(normalizedMonetization.RequiredScopes) == 0 {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "monetization.requiredScopes must be configured before revoking overrides")
		return
	}

	overrideScopes, err := validateEntitlementOverrideScopes([]string{scope}, normalizedMonetization.RequiredScopes)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	service := s.entitlementServiceSnapshot()
	if err := service.Revoke(r.Context(), principalId, overrideScopes[0], entitlements.SourceAdminOverride); err != nil {
		log.FromContext(r.Context()).Error().Err(err).Str("principal_id", principalId).Str("scope", overrideScopes[0]).Msg("failed to revoke entitlement override")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to revoke entitlement override")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) buildEntitlementStatus(ctx context.Context, principalID string, baseScopes []string) (*EntitlementStatus, error) {
	cfg := s.GetConfig()
	normalizedMonetization := cfg.Monetization.Normalized()
	service := s.entitlementServiceSnapshot()

	status, err := service.Status(ctx, entitlements.StatusRequest{
		PrincipalID:    principalID,
		BaseScopes:     baseScopes,
		RequiredScopes: normalizedMonetization.RequiredScopes,
		Model:          normalizedMonetization.Model,
		ProductName:    normalizedMonetization.ProductName,
		PurchaseURL:    normalizedMonetization.PurchaseURL,
		Enforcement:    normalizedMonetization.Enforcement,
	})
	if err != nil {
		return nil, err
	}

	resp := &EntitlementStatus{
		PrincipalId:    &status.PrincipalID,
		Model:          &status.Model,
		ProductName:    &status.ProductName,
		Enforcement:    &status.Enforcement,
		RequiredScopes: &status.RequiredScopes,
		GrantedScopes:  &status.GrantedScopes,
		MissingScopes:  &status.MissingScopes,
		Unlocked:       &status.Unlocked,
	}
	if status.PurchaseURL != "" {
		purchaseURL := status.PurchaseURL
		resp.PurchaseUrl = &purchaseURL
	}
	if len(status.Grants) > 0 {
		grants := make([]EntitlementGrant, 0, len(status.Grants))
		for _, grant := range status.Grants {
			entry := EntitlementGrant{
				Scope:     &grant.Scope,
				Source:    &grant.Source,
				GrantedAt: &grant.GrantedAt,
				Active:    &grant.Active,
			}
			if grant.ExpiresAt != nil {
				expiresAt := grant.ExpiresAt.UTC()
				entry.ExpiresAt = &expiresAt
			}
			grants = append(grants, entry)
		}
		resp.Grants = &grants
	}
	return resp, nil
}

func (s *Server) resolveEntitlementStatusTarget(principal *auth.Principal, params GetSystemEntitlementsParams) (string, []string, error) {
	return s.resolveEntitlementTarget(principal, params.PrincipalId)
}

func (s *Server) resolveEntitlementTarget(principal *auth.Principal, requestedPrincipalID *string) (string, []string, error) {
	if principal == nil {
		return "", nil, fmt.Errorf("principal is required")
	}

	targetPrincipalID := principal.ID
	baseScopes := append([]string(nil), principal.Scopes...)
	if requestedPrincipalID == nil {
		return targetPrincipalID, baseScopes, nil
	}

	targetOverride := strings.TrimSpace(*requestedPrincipalID)
	if targetOverride == "" || targetOverride == principal.ID {
		return targetPrincipalID, baseScopes, nil
	}

	if !newScopeSet(principal.Scopes).has(ScopeV3Admin) {
		return "", nil, fmt.Errorf("admin scope required to inspect a different principal")
	}

	return targetOverride, configuredPrincipalScopes(s.GetConfig(), targetOverride), nil
}

func (s *Server) entitlementServiceSnapshot() *entitlements.Service {
	s.mu.RLock()
	service := s.entitlementService
	s.mu.RUnlock()
	return service
}

func validateEntitlementOverrideScopes(scopes []string, allowedScopes []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(allowedScopes))
	for _, scope := range allowedScopes {
		allowed[strings.ToLower(strings.TrimSpace(scope))] = struct{}{}
	}

	normalized := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			return nil, fmt.Errorf("override scopes must not be empty")
		}
		if _, ok := allowed[scope]; !ok {
			return nil, fmt.Errorf("override scope %q is not configured in monetization.requiredScopes", scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("override scopes must not be empty")
	}
	return normalized, nil
}

func parseEntitlementOverrideExpiry(raw *time.Time) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	expiresAt := raw.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("override expiresAt must be in the future")
	}
	return &expiresAt, nil
}

func configuredPrincipalScopes(cfg config.AppConfig, principalID string) []string {
	targetPrincipalID := strings.TrimSpace(principalID)
	if targetPrincipalID == "" {
		return nil
	}

	scopes := make([]string, 0)
	for _, entry := range cfg.APITokens {
		if strings.TrimSpace(entry.User) != targetPrincipalID {
			continue
		}
		scopes = append(scopes, entry.Scopes...)
	}
	return dedupeStringScopes(scopes)
}

func dedupeStringScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		normalized := strings.ToLower(strings.TrimSpace(scope))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
