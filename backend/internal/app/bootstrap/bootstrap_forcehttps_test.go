package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// L22 — RED control. The TLS->ForceHTTPS promotion must reach the runtime SNAPSHOT (the cfg
// the server is built from), not just container.Config (assigned later, which was true even
// with the bug, masking it). With the promotion left after buildWireConfigState,
// container.snapshot.App.ForceHTTPS was false despite TLS enabled.
func TestWiring_TLSEnabled_SnapshotCarriesForceHTTPS(t *testing.T) {
	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := "version: v3\n" +
		"dataDir: " + tmpDir + "\n" +
		"api:\n  listenAddr: \":0\"\n  token: test-token\n  tokenScopes:\n    - v3:read\n" +
		"engine:\n  tunerSlots: [0]\n" +
		"enigma2:\n  baseUrl: http://mock-receiver\n" +
		"tls:\n  enabled: true\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "now", configPath)
	require.NoError(t, err)
	require.True(t, container.Config.ForceHTTPS, "resolved cfg should advertise ForceHTTPS")
	require.True(t, container.snapshot.App.ForceHTTPS,
		"runtime snapshot must ALSO carry ForceHTTPS — it is the cfg the server is built from")
}
