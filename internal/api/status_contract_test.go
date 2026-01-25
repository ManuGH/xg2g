package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
)

// TestStatusContract enforces the Trust & Visibility contract.
// 1. 401 Unauthorized (No Token)
// 2. 403 Forbidden (Token without v3:status scope)
// 3. 200 OK (Valid Token + Scope)
// 4. Determinism (No 'drift' field)
func TestStatusContract(t *testing.T) {
	// Setup Server
	cfg := config.AppConfig{
		APIToken:       "valid-token",
		APITokenScopes: []string{string(v3.ScopeV3Status)},
		// Add read-only token for negative test
		APITokens: []config.ScopedToken{
			{Token: "readonly-token", Scopes: []string{string(v3.ScopeV3Read)}},
		},
		APIListenAddr: ":0",
		DataDir:       t.TempDir(), // Sandbox
	}

	// Initialize minimal API server setup
	mgr := config.NewManager(cfg.DataDir)
	s := api.New(cfg, mgr)
	handler := s.Handler()

	t.Run("A: 401 Unauthorized (No Auth)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("B: 403 Forbidden (Insufficient Scope)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/status", nil)
		// Use token that has only v3:read, not v3:status
		req.Header.Set("Authorization", "Bearer readonly-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("C: 200 OK (Valid)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v3/status", nil)
		req.Header.Set("Authorization", "Bearer valid-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		// 1. Status Code
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		// 2. Schema Validation (Strict)
		var raw map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &raw)
		require.NoError(t, err)

		// 3. Verify Determinism (Drift field must be ABSENT)
		_, hasDrift := raw["drift"]
		assert.False(t, hasDrift, "Contract violation: 'drift' field must be absent")

		// 4. Verify Required Fields
		assert.Equal(t, "healthy", raw["status"])
		assert.NotEmpty(t, raw["release"])
		assert.NotEmpty(t, raw["runtime"])

		// 5. Verify Runtime (Go/FFmpeg present)
		runtime, ok := raw["runtime"].(map[string]interface{})
		require.True(t, ok)
		assert.NotEmpty(t, runtime["go"])
		assert.NotEmpty(t, runtime["ffmpeg"])
	})
}
