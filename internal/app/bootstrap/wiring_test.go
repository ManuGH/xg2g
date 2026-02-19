package bootstrap_test

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

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
)

// TestWiring_BootsMinimalStack is the mechanical proof for P2 Components Wiring.
// It verifies that:
// 1. The factory constructs a valid graph.
// 2. The server boots.
// 3. Middleware (RequestID) is active.
// 4. Config is injected.
func TestWiring_BootsMinimalStack(t *testing.T) {
	// 1. Setup minimal test config
	t.Setenv("XG2G_INITIAL_REFRESH", "false") // Disable background refresh to prevent network hangs
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	tmpDir, err := os.MkdirTemp("", "xg2g-wiring-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0" # Random port
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

	// 2. Wire the App
	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err, "Wiring failed")
	require.NotNil(t, container.Server)
	require.NotNil(t, container.App)

	// 3. Verify Server Handler (Middlewares active?)
	// Note: We deliberately do NOT call container.Start() here to verify that
	// the graph is constructible and the handler is wired *before* background processes start.
	// This proves construction purity.

	// However, for the mechanical proof of a "booted stack", we SHOULD start it
	// to ensure no startup panics occur in background routines.
	err = container.Start(ctx)
	require.NoError(t, err)

	handler := container.Server.Handler()
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// 4. Assertions
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Check Request-ID presence (Proof of Middleware Wiring)
	// STRICT: Canonical headers only.
	reqID := resp.Header.Get(controlhttp.HeaderRequestID)
	assert.NotEmpty(t, reqID, "X-Request-ID header missing")

	// Check Config Injection
	assert.Equal(t, tmpDir, container.Config.DataDir, "Config DataDir mismatch")
}
