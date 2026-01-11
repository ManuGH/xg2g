package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
)

// TestVODPlayback_Path_Wiring_ScopeEnforcement verifies that scope policy is enforced at the router level.
// Requirements:
// 1. Missing scope -> 403 Forbidden.
// 2. No Auth -> 401 Unauthorized.
// 3. Resolver MUST NOT be called.
func TestVODPlayback_Path_Wiring_ScopeEnforcement(t *testing.T) {
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	tmpDir, err := os.MkdirTemp("", "xg2g-vod-scope-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config with two tokens:
	// 1. valid-token: v3:read (used by success path, not here)
	// 2. guest-token: v3:guest (insufficient for v3:read)
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
  tokens:
    - token: "guest-token"
      user: "guest"
      scopes: ["v3:status"]
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
  username: root
  password: "dummy-password"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)

	// Inject Mock Resolver that Fails Test if called
	mock := &mockResolver{
		ResolveFunc: func(ctx context.Context, recordingID string, intent recservice.PlaybackIntent, profile recservice.PlaybackProfile) (recservice.PlaybackInfoResult, error) {
			assert.Fail(t, "Resolver MUST NOT be called when auth/scope fails")
			return recservice.PlaybackInfoResult{}, nil
		},
	}
	container.Server.SetResolver(mock)

	err = container.Start(ctx)
	require.NoError(t, err)
	handler := container.Server.Handler()

	t.Run("MissingScope_Forbidden", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/recordings/any-id/stream-info", nil)
		req.Header.Set("Authorization", "Bearer guest-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		// Check standard error headers if any (RFC 7807)
		assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))
	})

	t.Run("NoAuth_Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v3/recordings/any-id/stream-info", nil)
		// No Authorization header
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		resp := w.Result()

		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, "application/problem+json", resp.Header.Get("Content-Type"))
	})
}
