package v3_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v3 "github.com/ManuGH/xg2g/internal/api/v3"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemConfigContract verifies the API contract for /system/config
// It ensures that:
// 1. streaming.delivery_policy is "universal"
// 2. streaming.default_profile does NOT exist (implicit by struct definition)
// 3. streaming.allowed_profiles does NOT exist (implicit by struct definition)
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
	// body = { "streaming": { "delivery_policy": "universal" }, ... }

	streaming, ok := body["streaming"].(map[string]interface{})
	require.True(t, ok, "response should have 'streaming' object")

	// 1. Verify delivery_policy
	policy, ok := streaming["delivery_policy"].(string)
	require.True(t, ok, "streaming should have 'delivery_policy'")
	assert.Equal(t, "universal", policy)

	// 2. Verify NO default_profile
	_, hasDefault := streaming["default_profile"]
	assert.False(t, hasDefault, "response must NOT contain 'default_profile'")

	// 3. Verify NO allowed_profiles
	_, hasAllowed := streaming["allowed_profiles"]
	assert.False(t, hasAllowed, "response must NOT contain 'allowed_profiles'")
}
