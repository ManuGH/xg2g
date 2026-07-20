package bootstrap

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/require"
)

// L23 — RED control. An env-only deploy (no config file) must be able to reload: the reload
// loader is wired with the real (empty) effectiveConfigPath so Load() skips the file and
// re-derives from env. Wiring it to the fabricated DataDir/config.yaml made every reload
// fail on a non-existent file.
func TestBuildWireConfigState_EnvOnlyReloadSucceeds(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_E2_HOST", "http://mock-receiver") // Validate requires a non-empty Enigma2.BaseURL
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	tmpDir := t.TempDir()
	t.Setenv("XG2G_DATA", tmpDir)

	cfg := config.AppConfig{DataDir: tmpDir}
	_, cfgHolder, _, _ := buildWireConfigState(cfg, "test", "") // effectiveConfigPath == "" → env-only

	require.NotNil(t, cfgHolder)
	if err := cfgHolder.Reload(context.Background()); err != nil {
		t.Fatalf("env-only reload must succeed (no config file to read); got: %v", err)
	}
}
