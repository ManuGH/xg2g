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

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	store "github.com/ManuGH/xg2g/internal/domain/session/store"
	pipelinelease "github.com/ManuGH/xg2g/internal/pipeline/lease"
	paths "github.com/ManuGH/xg2g/internal/platform/paths"
)

// TestWiring_BootsMinimalStack is the mechanical proof for P2 Components Wiring.
// It verifies that:
// 1. The factory constructs a valid graph.
// 2. The server boots.
// 3. Middleware (RequestID) is active.
// 4. Config is injected.
func TestWiring_BootsMinimalStack(t *testing.T) {
	skipIfNoFFmpeg(t)
	// 1. Setup minimal test config
	t.Setenv("XG2G_INITIAL_REFRESH", "false") // Disable background refresh to prevent network hangs
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

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
recordings:
  target_signing_key: "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
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

func TestWiring_MetricsDefaultBindsLocalhost(t *testing.T) {
	skipIfNoFFmpeg(t)
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	tmpDir, err := os.MkdirTemp("", "xg2g-metrics-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
metrics:
  enabled: true
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9090", container.Deps.MetricsAddr)
}

func TestWiring_TLSEnabledForcesHTTPSByDefault(t *testing.T) {
	skipIfNoFFmpeg(t)
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	tmpDir, err := os.MkdirTemp("", "xg2g-tls-forcehttps-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
tls:
  enabled: true
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)
	require.True(t, container.Config.ForceHTTPS)
}

func TestBootstrap_WiresTrackedTunerLeaseControllerAndEnforcesStartupGate(t *testing.T) {
	skipIfNoFFmpeg(t)
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
store:
  backend: sqlite
  path: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
`
	err := os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Clean startup MUST succeed via WireServices
	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)
	require.NotNil(t, container)
	if fs, ok := container.IntentStore.(*pipelinelease.FileIntentStore); ok {
		_ = fs.Close()
	}

	// 2. Seed a missing active intent into intents.json
	intentsPath, err := paths.ResolveDataFilePath(tmpDir, "intents.json", true)
	require.NoError(t, err)
	corruptIntentContent := `{"intent-missing-1":{"intent_id":"intent-missing-1","lease_id":"lse-missing","owner":"session-missing","scope":"tuner:0","state":"ACTIVE","revision":1,"created_at":"2026-08-02T10:00:00Z","updated_at":"2026-08-02T10:00:00Z"}}`
	err = os.WriteFile(intentsPath, []byte(corruptIntentContent), 0600)
	require.NoError(t, err)

	// 3. Restart WireServices -> MUST fail during ExecuteStartupReconciliation
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	_, err = WireServices(ctx2, "test-v3", "test-commit", "now", configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mandatory startup reconciliation failed")
}

func TestBootstrap_EnforcesBackendIdentityAndRestartRecovery(t *testing.T) {
	skipIfNoFFmpeg(t)
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	t.Run("OrphanedLease_Refused", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XG2G_STORE_PATH", tmpDir)

		configPath := filepath.Join(tmpDir, "config.yaml")
		content := `
version: v3
dataDir: ` + tmpDir + `
store:
  backend: sqlite
  path: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
`
		err := os.WriteFile(configPath, []byte(content), 0600)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		st, err := store.NewSqliteStore(filepath.Join(tmpDir, "sessions.sqlite"))
		require.NoError(t, err)
		_, ok, err := st.TryAcquireLease(ctx, "tuner:0", "untracked-orphan-owner", 10*time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		_ = st.Close()

		_, err = WireServices(ctx, "test-v3", "test-commit", "now", configPath)
		require.Error(t, err, "WireServices MUST fail when reconciler observes orphaned lease in backend store")
		assert.Contains(t, err.Error(), "mandatory startup reconciliation failed")
	})

	t.Run("MatchingIntent_Accepted", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XG2G_STORE_PATH", tmpDir)

		configPath := filepath.Join(tmpDir, "config.yaml")
		content := `
version: v3
dataDir: ` + tmpDir + `
store:
  backend: sqlite
  path: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver
`
		err := os.WriteFile(configPath, []byte(content), 0600)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		st, err := store.NewSqliteStore(filepath.Join(tmpDir, "sessions.sqlite"))
		require.NoError(t, err)
		_, ok, err := st.TryAcquireLease(ctx, "tuner:0", "untracked-orphan-owner", 10*time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		_ = st.Close()

		intentsPath, err := paths.ResolveDataFilePath(tmpDir, "intents.json", true)
		require.NoError(t, err)
		matchingIntent := `{"intent-matching-1":{"intent_id":"intent-matching-1","lease_id":"tuner:0","owner":"untracked-orphan-owner","scope":"tuner:0","state":"ACTIVE","revision":1,"created_at":"2026-08-02T10:00:00Z","updated_at":"2026-08-02T10:00:00Z"}}`
		err = os.WriteFile(intentsPath, []byte(matchingIntent), 0600)
		require.NoError(t, err)

		container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
		require.NoError(t, err, "WireServices MUST succeed when intent and backend lease match")
		require.NotNil(t, container)
		_ = container.Close()
	})
}
