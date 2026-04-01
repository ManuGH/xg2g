package receipts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/entitlements"
)

type mockVerifier struct {
	provider string
	verifyFn func(context.Context, VerifyRequest) (VerifyResult, error)
}

func (m *mockVerifier) Provider() string {
	return m.provider
}

func (m *mockVerifier) Verify(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
	return m.verifyFn(ctx, req)
}

func TestServiceVerifyAndApplyGrantsMappedScopes(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:dvr", "xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderGooglePlay,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			return VerifyResult{
				Provider:     ProviderGooglePlay,
				ProductID:    req.ProductID,
				Source:       entitlements.SourceGooglePlay,
				State:        PurchaseStatePurchased,
				OrderID:      "order-123",
				PurchaseTime: ptrTime(time.Date(2026, time.April, 1, 11, 0, 0, 0, time.UTC)),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.VerifyAndApply(context.Background(), ApplyRequest{
		PrincipalID:   "viewer",
		Provider:      ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-1",
	})
	if err != nil {
		t.Fatalf("VerifyAndApply: %v", err)
	}

	if result.Action != ApplyActionGranted {
		t.Fatalf("expected granted action, got %s", result.Action)
	}
	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(grants))
	}
}

func TestServiceVerifyAndApplyPendingDoesNotGrant(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderGooglePlay,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			return VerifyResult{
				Provider:  ProviderGooglePlay,
				ProductID: req.ProductID,
				Source:    entitlements.SourceGooglePlay,
				State:     PurchaseStatePending,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.VerifyAndApply(context.Background(), ApplyRequest{
		PrincipalID:   "viewer",
		Provider:      ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-2",
	})
	if err != nil {
		t.Fatalf("VerifyAndApply: %v", err)
	}
	if result.Action != ApplyActionNone {
		t.Fatalf("expected no-op action, got %s", result.Action)
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected no grants, got %d", len(grants))
	}
}

func TestServiceVerifyAndApplyRevokesExistingScopes(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	requireGrant(t, entitlementService, entitlements.Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      entitlements.SourceGooglePlay,
	})

	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderGooglePlay,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			return VerifyResult{
				Provider:  ProviderGooglePlay,
				ProductID: req.ProductID,
				Source:    entitlements.SourceGooglePlay,
				State:     PurchaseStateRevoked,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.VerifyAndApply(context.Background(), ApplyRequest{
		PrincipalID:   "viewer",
		Provider:      ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "purchase-token-3",
	})
	if err != nil {
		t.Fatalf("VerifyAndApply: %v", err)
	}
	if result.Action != ApplyActionRevoked {
		t.Fatalf("expected revoked action, got %s", result.Action)
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected no grants after revoke, got %d", len(grants))
	}
}

func TestServiceVerifyAndApplyIsIdempotentForRepeatedPurchase(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderGooglePlay,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			return VerifyResult{
				Provider:  ProviderGooglePlay,
				ProductID: req.ProductID,
				Source:    entitlements.SourceGooglePlay,
				State:     PurchaseStatePurchased,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := service.VerifyAndApply(context.Background(), ApplyRequest{
			PrincipalID:   "viewer",
			Provider:      ProviderGooglePlay,
			ProductID:     "xg2g.unlock",
			PurchaseToken: "purchase-token-4",
		}); err != nil {
			t.Fatalf("VerifyAndApply call %d: %v", i, err)
		}
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 idempotent grant, got %d", len(grants))
	}
}

func TestServiceVerifyAndApplyPropagatesVerifierErrors(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderGooglePlay,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			return VerifyResult{}, &Error{Kind: ErrorKindInvalidInput, Message: "invalid purchase token", Err: errors.New("bad token")}
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.VerifyAndApply(context.Background(), ApplyRequest{
		PrincipalID:   "viewer",
		Provider:      ProviderGooglePlay,
		ProductID:     "xg2g.unlock",
		PurchaseToken: "bad-token",
	})
	if err == nil {
		t.Fatal("expected verifier error")
	}

	var receiptErr *Error
	if !errors.As(err, &receiptErr) {
		t.Fatalf("expected receipts.Error, got %T", err)
	}
	if receiptErr.Kind != ErrorKindInvalidInput {
		t.Fatalf("expected invalid_input error kind, got %s", receiptErr.Kind)
	}
}

func TestServiceVerifyAndApplyPassesAmazonUserIDToVerifier(t *testing.T) {
	store := entitlements.NewMemoryStore()
	entitlementService := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	service, err := NewService(config.MonetizationConfig{
		ProductMappings: []config.MonetizationProductMapping{
			{Provider: ProviderAmazonAppstore, ProductID: "xg2g.unlock.firetv", Scopes: []string{"xg2g:unlock"}},
		},
	}, entitlementService, &mockVerifier{
		provider: ProviderAmazonAppstore,
		verifyFn: func(ctx context.Context, req VerifyRequest) (VerifyResult, error) {
			if req.UserID != "amzn-user-1" {
				t.Fatalf("expected amazon user id to be passed through, got %q", req.UserID)
			}
			return VerifyResult{
				Provider:  ProviderAmazonAppstore,
				ProductID: req.ProductID,
				Source:    entitlements.SourceAmazonAppstore,
				State:     PurchaseStatePurchased,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.VerifyAndApply(context.Background(), ApplyRequest{
		PrincipalID:   "viewer",
		Provider:      ProviderAmazonAppstore,
		ProductID:     "xg2g.unlock.firetv",
		PurchaseToken: "amazon-receipt-1",
		UserID:        "amzn-user-1",
	})
	if err != nil {
		t.Fatalf("VerifyAndApply: %v", err)
	}
}

func requireGrant(t *testing.T, svc *entitlements.Service, grant entitlements.Grant) {
	t.Helper()
	if err := svc.Grant(context.Background(), grant); err != nil {
		t.Fatalf("grant entitlement: %v", err)
	}
}

func ptrTime(ts time.Time) *time.Time {
	value := ts.UTC()
	return &value
}
