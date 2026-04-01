package receipts

import (
	"context"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/entitlements"
)

type Service struct {
	entitlementService *entitlements.Service
	mappings           map[string]config.MonetizationProductMapping
	verifiers          map[string]Verifier
}

func NewService(monetization config.MonetizationConfig, entitlementService *entitlements.Service, verifiers ...Verifier) (*Service, error) {
	normalized := monetization.Normalized()
	svc := &Service{
		entitlementService: entitlementService,
		mappings:           make(map[string]config.MonetizationProductMapping, len(normalized.ProductMappings)),
		verifiers:          make(map[string]Verifier, len(verifiers)),
	}

	for _, verifier := range verifiers {
		if verifier == nil {
			continue
		}
		provider := normalizeProvider(verifier.Provider())
		if provider == "" {
			return nil, fmt.Errorf("receipt verifier provider must not be empty")
		}
		if _, exists := svc.verifiers[provider]; exists {
			return nil, fmt.Errorf("duplicate receipt verifier for provider %q", provider)
		}
		svc.verifiers[provider] = verifier
	}

	for _, mapping := range normalized.ProductMappings {
		key := productMappingKey(mapping.Provider, mapping.ProductID)
		if _, exists := svc.mappings[key]; exists {
			return nil, fmt.Errorf("duplicate product mapping for %s/%s", mapping.Provider, mapping.ProductID)
		}
		svc.mappings[key] = mapping
	}

	return svc, nil
}

func (s *Service) VerifyAndApply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	if s == nil {
		return ApplyResult{}, newError(ErrorKindUnavailable, "receipt service unavailable", nil)
	}
	if s.entitlementService == nil {
		return ApplyResult{}, newError(ErrorKindUnavailable, "entitlement service unavailable", nil)
	}

	principalID := normalizePrincipalID(req.PrincipalID)
	if principalID == "" {
		return ApplyResult{}, newError(ErrorKindInvalidInput, "principalId must not be empty", nil)
	}
	provider := normalizeProvider(req.Provider)
	if provider == "" {
		return ApplyResult{}, newError(ErrorKindInvalidInput, "provider must not be empty", nil)
	}
	productID := normalizeProductID(req.ProductID)
	if productID == "" {
		return ApplyResult{}, newError(ErrorKindInvalidInput, "productId must not be empty", nil)
	}
	purchaseToken := normalizeProductID(req.PurchaseToken)
	if purchaseToken == "" {
		return ApplyResult{}, newError(ErrorKindInvalidInput, "purchaseToken must not be empty", nil)
	}

	mapping, ok := s.mappings[productMappingKey(provider, productID)]
	if !ok {
		return ApplyResult{}, newError(ErrorKindInvalidInput, fmt.Sprintf("no monetization product mapping found for provider %q and productId %q", provider, productID), ErrUnknownProductMapping)
	}
	verifier, ok := s.verifiers[provider]
	if !ok {
		return ApplyResult{}, newError(ErrorKindUnavailable, fmt.Sprintf("receipt verifier for provider %q is not configured", provider), ErrVerifierUnavailable)
	}

	verification, err := verifier.Verify(ctx, VerifyRequest{
		Provider:      provider,
		ProductID:     productID,
		PurchaseToken: purchaseToken,
		UserID:        normalizeUserID(req.UserID),
	})
	if err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{
		Verification: verification,
		MappedScopes: append([]string(nil), mapping.Scopes...),
		Action:       ApplyActionNone,
	}

	switch verification.State {
	case PurchaseStatePurchased:
		for _, scope := range mapping.Scopes {
			if err := s.entitlementService.Grant(ctx, entitlements.Grant{
				PrincipalID: principalID,
				Scope:       scope,
				Source:      verification.Source,
				GrantedAt:   derefTimeOrZero(verification.PurchaseTime),
			}); err != nil {
				return ApplyResult{}, newError(ErrorKindUpstream, "failed to persist verified entitlement grant", err)
			}
		}
		result.Action = ApplyActionGranted
	case PurchaseStateCancelled, PurchaseStateRevoked:
		for _, scope := range mapping.Scopes {
			if err := s.entitlementService.Revoke(ctx, principalID, scope, verification.Source); err != nil {
				return ApplyResult{}, newError(ErrorKindUpstream, "failed to revoke entitlements for non-active purchase", err)
			}
		}
		result.Action = ApplyActionRevoked
	case PurchaseStatePending:
		result.Action = ApplyActionNone
	default:
		return ApplyResult{}, newError(ErrorKindUpstream, "unsupported purchase state returned by receipt verifier", wrapUnexpectedState(verification.State))
	}

	return result, nil
}

func productMappingKey(provider, productID string) string {
	return normalizeProvider(provider) + "\x00" + normalizeProductID(productID)
}

func derefTimeOrZero(ts *time.Time) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.UTC()
}
