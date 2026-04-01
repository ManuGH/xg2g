package v3_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntitlementOverrideRoundTripThroughRouter(t *testing.T) {
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
		},
	}

	store, err := entitlements.NewStore("sqlite", t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	service := entitlements.NewService(store, entitlements.WithCacheTTL(time.Hour))
	server := v3.NewServer(cfg, nil, nil)
	server.SetDependencies(v3.Dependencies{Entitlements: service})

	handler, err := v3.NewHandler(server, cfg)
	require.NoError(t, err)

	t.Run("partial grant stays locked", func(t *testing.T) {
		postEntitlementOverride(t, handler, "admin-token", "viewer", []string{"xg2g:unlock"})

		status := getEntitlementStatus(t, handler, "viewer-token", "")
		require.NotNil(t, status.Unlocked)
		assert.False(t, *status.Unlocked)
		assert.Equal(t, []string{"xg2g:unlock"}, derefStringSlice(status.GrantedScopes))
		assert.Equal(t, []string{"xg2g:dvr"}, derefStringSlice(status.MissingScopes))

		cfgResp := getBootstrapConfig(t, handler, "viewer-token")
		require.NotNil(t, cfgResp.Monetization)
		require.NotNil(t, cfgResp.Monetization.Unlocked)
		assert.False(t, *cfgResp.Monetization.Unlocked)
	})

	t.Run("full grant unlocks immediately and is readable via admin inspection", func(t *testing.T) {
		postEntitlementOverride(t, handler, "admin-token", "viewer", []string{"xg2g:dvr"})

		status := getEntitlementStatus(t, handler, "viewer-token", "")
		require.NotNil(t, status.Unlocked)
		assert.True(t, *status.Unlocked)
		assert.ElementsMatch(t, []string{"xg2g:unlock", "xg2g:dvr"}, derefStringSlice(status.GrantedScopes))
		assert.Empty(t, derefStringSlice(status.MissingScopes))

		adminStatus := getEntitlementStatus(t, handler, "admin-token", "viewer")
		require.NotNil(t, adminStatus.Unlocked)
		assert.True(t, *adminStatus.Unlocked)
		assert.Equal(t, "viewer", derefEntitlementString(adminStatus.PrincipalId))

		cfgResp := getBootstrapConfig(t, handler, "viewer-token")
		require.NotNil(t, cfgResp.Monetization)
		require.NotNil(t, cfgResp.Monetization.Unlocked)
		assert.True(t, *cfgResp.Monetization.Unlocked)
	})

	t.Run("revoke invalidates cache without waiting for ttl", func(t *testing.T) {
		deleteEntitlementOverride(t, handler, "admin-token", "viewer", "xg2g:dvr")

		status := getEntitlementStatus(t, handler, "viewer-token", "")
		require.NotNil(t, status.Unlocked)
		assert.False(t, *status.Unlocked)
		assert.Equal(t, []string{"xg2g:unlock"}, derefStringSlice(status.GrantedScopes))
		assert.Equal(t, []string{"xg2g:dvr"}, derefStringSlice(status.MissingScopes))

		cfgResp := getBootstrapConfig(t, handler, "viewer-token")
		require.NotNil(t, cfgResp.Monetization)
		require.NotNil(t, cfgResp.Monetization.Unlocked)
		assert.False(t, *cfgResp.Monetization.Unlocked)
	})
}

func TestEntitlementStatusRejectsCrossPrincipalReadsWithoutAdminScope(t *testing.T) {
	cfg := config.AppConfig{
		APITokens: []config.ScopedToken{
			{Token: "viewer-token", User: "viewer", Scopes: []string{"v3:read"}},
			{Token: "other-token", User: "other", Scopes: []string{"v3:read"}},
		},
		Monetization: config.MonetizationConfig{
			Enabled:        true,
			Model:          config.MonetizationModelOneTimeUnlock,
			ProductName:    "xg2g Unlock",
			RequiredScopes: []string{"xg2g:unlock"},
			Enforcement:    config.MonetizationEnforcementRequired,
		},
	}

	server := v3.NewServer(cfg, nil, nil)
	handler, err := v3.NewHandler(server, cfg)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/entitlements?principalId=other", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func postEntitlementOverride(t *testing.T, handler http.Handler, token, principalID string, scopes []string) {
	t.Helper()

	principalIDCopy := principalID
	body, err := json.Marshal(v3.PostSystemEntitlementOverrideJSONRequestBody{
		PrincipalId: &principalIDCopy,
		Scopes:      scopes,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v3/system/entitlements/overrides", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func deleteEntitlementOverride(t *testing.T, handler http.Handler, token, principalID, scope string) {
	t.Helper()

	path := "/api/v3/system/entitlements/overrides/" + url.PathEscape(principalID) + "/" + url.PathEscape(scope)
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func getEntitlementStatus(t *testing.T, handler http.Handler, token, principalID string) v3.EntitlementStatus {
	t.Helper()

	path := "/api/v3/system/entitlements"
	if principalID != "" {
		path += "?principalId=" + url.QueryEscape(principalID)
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var status v3.EntitlementStatus
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	return status
}

func getBootstrapConfig(t *testing.T, handler http.Handler, token string) v3.AppConfig {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var cfgResp v3.AppConfig
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&cfgResp))
	return cfgResp
}

func derefStringSlice(values *[]string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), (*values)...)
}

func derefEntitlementString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
