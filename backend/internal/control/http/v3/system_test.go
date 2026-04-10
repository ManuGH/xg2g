package v3_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/entitlements"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemConfigContract verifies the API contract for /system/config
// It ensures that:
// 1. streaming.deliveryPolicy is "universal"
// 2. streaming.default_profile does NOT exist (implicit by struct definition)
// 3. streaming.allowedProfiles does NOT exist (implicit by struct definition)
func TestSystemConfigContract(t *testing.T) {
	// Setup minimal config
	cfg := config.AppConfig{
		Streaming: config.StreamingConfig{
			DeliveryPolicy: "universal",
		},
	}

	// Create handler using NewServer
	server := v3.NewServer(cfg, nil, nil)

	// Create a request
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	w := httptest.NewRecorder()

	// Call the handler
	server.GetSystemConfig(w, req)

	// Check status code
	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse body map to verify fields existence/non-existence
	var body map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	// Verify Structure
	// body = { "streaming": { "deliveryPolicy": "universal" }, ... }

	streaming, ok := body["streaming"].(map[string]interface{})
	require.True(t, ok, "response should have 'streaming' object")

	// 1. Verify deliveryPolicy
	policy, ok := streaming["deliveryPolicy"].(string)
	require.True(t, ok, "streaming should have 'deliveryPolicy'")
	assert.Equal(t, "universal", policy)

	// 2. Verify NO defaultProfile
	_, hasDefault := streaming["defaultProfile"]
	assert.False(t, hasDefault, "response must NOT contain 'defaultProfile'")

	// 3. Verify NO allowedProfiles
	_, hasAllowed := streaming["allowedProfiles"]
	assert.False(t, hasAllowed, "response must NOT contain 'allowedProfiles'")
}

func TestSystemConfigIncludesMonetizationUnlockState(t *testing.T) {
	cfg := config.AppConfig{
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

	server := v3.NewServer(cfg, nil, nil)
	tests := []struct {
		name     string
		scopes   []string
		unlocked bool
	}{
		{
			name:     "locked without required scopes",
			scopes:   []string{},
			unlocked: false,
		},
		{
			name:     "locked with partial required scopes",
			scopes:   []string{"v3:admin", "xg2g:unlock"},
			unlocked: false,
		},
		{
			name:     "unlocked with all required scopes",
			scopes:   []string{"v3:admin", "xg2g:unlock", "xg2g:dvr"},
			unlocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", tt.scopes)))
			w := httptest.NewRecorder()

			server.GetSystemConfig(w, req)

			resp := w.Result()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var body map[string]interface{}
			err := json.NewDecoder(resp.Body).Decode(&body)
			require.NoError(t, err)

			monetization, ok := body["monetization"].(map[string]interface{})
			require.True(t, ok, "response should include monetization object")
			assert.Equal(t, "one_time_unlock", monetization["model"])
			assert.Equal(t, "required", monetization["enforcement"])
			assert.Equal(t, "https://example.com/unlock", monetization["purchaseUrl"])
			assert.Equal(t, tt.unlocked, monetization["unlocked"])
			assert.ElementsMatch(t, []string{"xg2g:dvr", "xg2g:unlock"}, monetization["requiredScopes"])
		})
	}
}

func TestSystemConfigSanitizesAdminFieldsForReadScope(t *testing.T) {
	cfg := config.AppConfig{
		DataDir:  "/srv/xg2g/data",
		LogLevel: "debug",
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver.local",
			Username:   "operator",
			StreamPort: 8001,
		},
	}

	server := v3.NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "reader", []string{"v3:read"})))
	w := httptest.NewRecorder()

	server.GetSystemConfig(w, req)

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	_, hasDataDir := body["dataDir"]
	assert.False(t, hasDataDir, "read scope must not receive dataDir")
	_, hasLogLevel := body["logLevel"]
	assert.False(t, hasLogLevel, "read scope must not receive logLevel")

	openWebIF, ok := body["openWebIF"].(map[string]any)
	require.True(t, ok, "response should include openWebIF")
	assert.Equal(t, "http://receiver.local", openWebIF["baseUrl"])
	assert.EqualValues(t, 8001, openWebIF["streamPort"])
	_, hasUsername := openWebIF["username"]
	assert.False(t, hasUsername, "read scope must not receive openWebIF.username")
}

func TestSystemConfigKeepsAdminFieldsForAdminScope(t *testing.T) {
	cfg := config.AppConfig{
		DataDir:  "/srv/xg2g/data",
		LogLevel: "debug",
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver.local",
			Username:   "operator",
			StreamPort: 8001,
		},
	}

	server := v3.NewServer(cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/config", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "admin", []string{"v3:admin"})))
	w := httptest.NewRecorder()

	server.GetSystemConfig(w, req)

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.Equal(t, "/srv/xg2g/data", body["dataDir"])
	assert.Equal(t, "debug", body["logLevel"])

	openWebIF, ok := body["openWebIF"].(map[string]any)
	require.True(t, ok, "response should include openWebIF")
	assert.Equal(t, "operator", openWebIF["username"])
}

func TestSystemEntitlementsStatusReflectsActiveAndMissingScopes(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	store := entitlements.NewMemoryStore()
	service := entitlements.NewService(
		store,
		entitlements.WithClock(func() time.Time { return now }),
		entitlements.WithCacheTTL(time.Hour),
	)
	expiresAt := now.Add(2 * time.Hour)
	require.NoError(t, service.Grant(httptest.NewRequest(http.MethodGet, "/", nil).Context(), entitlements.Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      entitlements.SourceAdminOverride,
		GrantedAt:   now.Add(-time.Hour),
		ExpiresAt:   &expiresAt,
	}))

	cfg := config.AppConfig{
		Monetization: config.MonetizationConfig{
			Enabled:        true,
			Model:          config.MonetizationModelOneTimeUnlock,
			ProductName:    "xg2g Unlock",
			RequiredScopes: []string{"xg2g:unlock", "xg2g:dvr"},
			PurchaseURL:    "https://example.com/unlock",
			Enforcement:    config.MonetizationEnforcementRequired,
		},
	}

	server := v3.NewServer(cfg, nil, nil)
	server.SetDependencies(v3.Dependencies{Entitlements: service})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/system/entitlements", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))
	w := httptest.NewRecorder()

	server.GetSystemEntitlements(w, req, v3.GetSystemEntitlementsParams{})

	resp := w.Result()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body v3.EntitlementStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	require.NotNil(t, body.RequiredScopes)
	assert.Equal(t, []string{"xg2g:dvr", "xg2g:unlock"}, *body.RequiredScopes)
	require.NotNil(t, body.GrantedScopes)
	assert.Equal(t, []string{"xg2g:unlock"}, *body.GrantedScopes)
	require.NotNil(t, body.MissingScopes)
	assert.Equal(t, []string{"xg2g:dvr"}, *body.MissingScopes)
	require.NotNil(t, body.Unlocked)
	assert.False(t, *body.Unlocked)
	require.NotNil(t, body.Grants)
	if assert.Len(t, *body.Grants, 1) {
		assert.Equal(t, "xg2g:unlock", derefString((*body.Grants)[0].Scope))
		assert.Equal(t, entitlements.SourceAdminOverride, derefString((*body.Grants)[0].Source))
		assert.Equal(t, true, derefBool((*body.Grants)[0].Active))
	}
}

func TestSystemEntitlementOverrideRoundTrip(t *testing.T) {
	now := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	service := entitlements.NewService(
		entitlements.NewMemoryStore(),
		entitlements.WithClock(func() time.Time { return now }),
		entitlements.WithCacheTTL(time.Hour),
	)

	cfg := config.AppConfig{
		Monetization: config.MonetizationConfig{
			Enabled:        true,
			Model:          config.MonetizationModelOneTimeUnlock,
			ProductName:    "xg2g Unlock",
			RequiredScopes: []string{"xg2g:unlock", "xg2g:dvr"},
			Enforcement:    config.MonetizationEnforcementRequired,
		},
	}

	server := v3.NewServer(cfg, nil, nil)
	server.SetDependencies(v3.Dependencies{Entitlements: service})

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	overrideBody, err := json.Marshal(v3.PostSystemEntitlementOverrideJSONRequestBody{
		Scopes:    []string{"xg2g:unlock", "xg2g:dvr"},
		ExpiresAt: &expiresAt,
	})
	require.NoError(t, err)

	postReq := httptest.NewRequest(http.MethodPost, "/api/v3/system/entitlements/overrides", bytes.NewReader(overrideBody))
	postReq = postReq.WithContext(auth.WithPrincipal(postReq.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:admin"})))
	postReq.Header.Set("Content-Type", "application/json")
	postRes := httptest.NewRecorder()

	server.PostSystemEntitlementOverride(postRes, postReq)
	require.Equal(t, http.StatusNoContent, postRes.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v3/system/entitlements", nil)
	getReq = getReq.WithContext(auth.WithPrincipal(getReq.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))
	getRes := httptest.NewRecorder()

	server.GetSystemEntitlements(getRes, getReq, v3.GetSystemEntitlementsParams{})
	require.Equal(t, http.StatusOK, getRes.Code)

	var unlockedBody v3.EntitlementStatus
	require.NoError(t, json.NewDecoder(getRes.Body).Decode(&unlockedBody))
	require.NotNil(t, unlockedBody.Unlocked)
	assert.True(t, *unlockedBody.Unlocked)
	require.NotNil(t, unlockedBody.GrantedScopes)
	assert.ElementsMatch(t, []string{"xg2g:unlock", "xg2g:dvr"}, *unlockedBody.GrantedScopes)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v3/system/entitlements/overrides/viewer/xg2g:dvr", nil)
	deleteReq = deleteReq.WithContext(auth.WithPrincipal(deleteReq.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:admin"})))
	deleteRes := httptest.NewRecorder()

	server.DeleteSystemEntitlementOverride(deleteRes, deleteReq, "viewer", "xg2g:dvr")
	require.Equal(t, http.StatusNoContent, deleteRes.Code)

	recheckReq := httptest.NewRequest(http.MethodGet, "/api/v3/system/entitlements", nil)
	recheckReq = recheckReq.WithContext(auth.WithPrincipal(recheckReq.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))
	recheckRes := httptest.NewRecorder()

	server.GetSystemEntitlements(recheckRes, recheckReq, v3.GetSystemEntitlementsParams{})
	require.Equal(t, http.StatusOK, recheckRes.Code)

	var lockedBody v3.EntitlementStatus
	require.NoError(t, json.NewDecoder(recheckRes.Body).Decode(&lockedBody))
	require.NotNil(t, lockedBody.Unlocked)
	assert.False(t, *lockedBody.Unlocked)
	require.NotNil(t, lockedBody.MissingScopes)
	assert.Equal(t, []string{"xg2g:dvr"}, *lockedBody.MissingScopes)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}
