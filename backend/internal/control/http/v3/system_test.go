package v3_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
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
