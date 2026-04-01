package v3_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/ManuGH/xg2g/internal/receipts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceiptRoundTripThroughRouter(t *testing.T) {
	cfg := config.AppConfig{
		APITokens: []config.ScopedToken{
			{Token: "viewer-token", User: "viewer", Scopes: []string{"v3:admin", "v3:read"}},
			{Token: "admin-token", User: "admin", Scopes: []string{"v3:admin", "v3:read"}},
		},
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
		Monetization: config.MonetizationConfig{
			Enabled:        true,
			Model:          config.MonetizationModelOneTimeUnlock,
			ProductName:    "xg2g Unlock",
			RequiredScopes: []string{"xg2g:unlock", "xg2g:dvr"},
			PurchaseURL:    "https://example.com/unlock",
			Enforcement:    config.MonetizationEnforcementRequired,
			ProductMappings: []config.MonetizationProductMapping{
				{Provider: receipts.ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock", "xg2g:dvr"}},
				{Provider: receipts.ProviderGooglePlay, ProductID: "xg2g.partial", Scopes: []string{"xg2g:unlock"}},
			},
		},
	}

	entitlementStore, err := entitlements.NewStore("sqlite", t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, entitlementStore.Close()) }()

	entitlementService := entitlements.NewService(entitlementStore, entitlements.WithCacheTTL(time.Hour))
	receiptService, err := receipts.NewService(
		cfg.Monetization.Normalized(),
		entitlementService,
		&mockReceiptVerifier{
			provider: receipts.ProviderGooglePlay,
			results: map[string]receipts.VerifyResult{
				"token-partial": {
					Provider:  receipts.ProviderGooglePlay,
					ProductID: "xg2g.partial",
					Source:    entitlements.SourceGooglePlay,
					State:     receipts.PurchaseStatePurchased,
				},
				"token-full": {
					Provider:  receipts.ProviderGooglePlay,
					ProductID: "xg2g.unlock",
					Source:    entitlements.SourceGooglePlay,
					State:     receipts.PurchaseStatePurchased,
				},
				"token-revoked": {
					Provider:  receipts.ProviderGooglePlay,
					ProductID: "xg2g.unlock",
					Source:    entitlements.SourceGooglePlay,
					State:     receipts.PurchaseStateRevoked,
				},
			},
		},
	)
	require.NoError(t, err)

	server := v3.NewServer(cfg, nil, nil)
	server.SetDependencies(v3.Dependencies{
		Entitlements: entitlementService,
		Receipts:     receiptService,
	})

	handler, err := v3.NewHandler(server, cfg)
	require.NoError(t, err)

	t.Run("partial mapping stays locked", func(t *testing.T) {
		resp := postEntitlementReceipt(t, handler, "viewer-token", nil, "xg2g.partial", "token-partial")
		assert.Equal(t, "granted", resp.Action)
		assert.Equal(t, "purchased", resp.PurchaseState)
		require.NotNil(t, resp.EntitlementStatus.Unlocked)
		assert.False(t, *resp.EntitlementStatus.Unlocked)
		assert.Equal(t, []string{"xg2g:unlock"}, resp.MappedScopes)

		cfgResp := getBootstrapConfig(t, handler, "viewer-token")
		require.NotNil(t, cfgResp.Monetization)
		require.NotNil(t, cfgResp.Monetization.Unlocked)
		assert.False(t, *cfgResp.Monetization.Unlocked)
	})

	t.Run("purchased receipt unlocks immediately", func(t *testing.T) {
		resp := postEntitlementReceipt(t, handler, "viewer-token", nil, "xg2g.unlock", "token-full")
		assert.Equal(t, "granted", resp.Action)
		assert.Equal(t, "purchased", resp.PurchaseState)
		require.NotNil(t, resp.EntitlementStatus.Unlocked)
		assert.True(t, *resp.EntitlementStatus.Unlocked)
		assert.ElementsMatch(t, []string{"xg2g:unlock", "xg2g:dvr"}, resp.MappedScopes)

		status := getEntitlementStatus(t, handler, "viewer-token", "")
		require.NotNil(t, status.Unlocked)
		assert.True(t, *status.Unlocked)

		cfgResp := getBootstrapConfig(t, handler, "viewer-token")
		require.NotNil(t, cfgResp.Monetization)
		require.NotNil(t, cfgResp.Monetization.Unlocked)
		assert.True(t, *cfgResp.Monetization.Unlocked)
	})

	t.Run("revoked receipt relocks without waiting for ttl", func(t *testing.T) {
		resp := postEntitlementReceipt(t, handler, "viewer-token", nil, "xg2g.unlock", "token-revoked")
		assert.Equal(t, "revoked", resp.Action)
		assert.Equal(t, "revoked", resp.PurchaseState)
		require.NotNil(t, resp.EntitlementStatus.Unlocked)
		assert.False(t, *resp.EntitlementStatus.Unlocked)

		status := getEntitlementStatus(t, handler, "viewer-token", "")
		require.NotNil(t, status.Unlocked)
		assert.False(t, *status.Unlocked)
		assert.ElementsMatch(t, []string{"xg2g:unlock", "xg2g:dvr"}, derefStringSlice(status.MissingScopes))
	})
}

func TestReceiptRoundTripRejectsCrossPrincipalWritesWithoutAdminScope(t *testing.T) {
	cfg := config.AppConfig{
		APITokens: []config.ScopedToken{
			{Token: "viewer-token", User: "viewer", Scopes: []string{"v3:read"}},
			{Token: "admin-token", User: "admin", Scopes: []string{"v3:admin", "v3:read"}},
		},
		Monetization: config.MonetizationConfig{
			Enabled:        true,
			Model:          config.MonetizationModelOneTimeUnlock,
			ProductName:    "xg2g Unlock",
			RequiredScopes: []string{"xg2g:unlock"},
			Enforcement:    config.MonetizationEnforcementRequired,
			ProductMappings: []config.MonetizationProductMapping{
				{Provider: receipts.ProviderGooglePlay, ProductID: "xg2g.unlock", Scopes: []string{"xg2g:unlock"}},
			},
		},
	}

	entitlementService := entitlements.NewService(entitlements.NewMemoryStore())
	receiptService, err := receipts.NewService(
		cfg.Monetization.Normalized(),
		entitlementService,
		&mockReceiptVerifier{
			provider: receipts.ProviderGooglePlay,
			results: map[string]receipts.VerifyResult{
				"token-full": {
					Provider:  receipts.ProviderGooglePlay,
					ProductID: "xg2g.unlock",
					Source:    entitlements.SourceGooglePlay,
					State:     receipts.PurchaseStatePurchased,
				},
			},
		},
	)
	require.NoError(t, err)

	server := v3.NewServer(cfg, nil, nil)
	server.SetDependencies(v3.Dependencies{
		Entitlements: entitlementService,
		Receipts:     receiptService,
	})

	handler, err := v3.NewHandler(server, cfg)
	require.NoError(t, err)

	otherPrincipal := "other"
	body, err := json.Marshal(v3.PostSystemEntitlementReceiptJSONRequestBody{
		PrincipalId:   &otherPrincipal,
		Provider:      receipts.ProviderGooglePlay,
		ProductId:     "xg2g.unlock",
		PurchaseToken: "token-full",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v3/system/entitlements/receipts", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

type mockReceiptVerifier struct {
	provider string
	results  map[string]receipts.VerifyResult
}

func (m *mockReceiptVerifier) Provider() string {
	return m.provider
}

func (m *mockReceiptVerifier) Verify(_ context.Context, req receipts.VerifyRequest) (receipts.VerifyResult, error) {
	result, ok := m.results[req.PurchaseToken]
	if !ok {
		result, ok = m.results[req.ProductID]
	}
	if !ok {
		return receipts.VerifyResult{}, &receipts.Error{
			Kind:    receipts.ErrorKindInvalidInput,
			Message: "unknown test product",
		}
	}
	return result, nil
}

func postEntitlementReceipt(t *testing.T, handler http.Handler, token string, principalID *string, productID, purchaseToken string) v3.EntitlementReceiptResponse {
	t.Helper()

	body, err := json.Marshal(v3.PostSystemEntitlementReceiptJSONRequestBody{
		PrincipalId:   principalID,
		Provider:      receipts.ProviderGooglePlay,
		ProductId:     productID,
		PurchaseToken: purchaseToken,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v3/system/entitlements/receipts", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp v3.EntitlementReceiptResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp
}
