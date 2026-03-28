package checks_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/verification/checks"
)

func minimalConfigYAML(dataDir string) string {
	return `version: "1"
dataDir: ` + dataDir + `
store:
  path: ` + filepath.Join(dataDir, "store") + `
enigma2:
  baseUrl: http://receiver.local
epg:
  enabled: true
`
}

func pinConfigEnv(t *testing.T, dataDir string) {
	t.Helper()
	t.Setenv("XG2G_DATA", dataDir)
	t.Setenv("XG2G_DATA_DIR", dataDir)
	t.Setenv("XG2G_STORE_PATH", filepath.Join(dataDir, "store"))
}

func TestConfigChecker_Drift(t *testing.T) {
	tmp := t.TempDir()
	pinConfigEnv(t, tmp)
	path := filepath.Join(tmp, "config.yaml")
	err := os.WriteFile(path, []byte(minimalConfigYAML(tmp)+"logLevel: info\n"), 0644)
	require.NoError(t, err)

	cfg, err := config.NewLoader(path, "test-version").Load()
	require.NoError(t, err)

	provider := &mockConfigProvider{cfg: &cfg}
	c := checks.NewConfigChecker(path, provider)
	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mismatches)

	provider.cfg.LogLevel = "debug"
	mismatches, err = c.Check(context.Background())
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Equal(t, verification.KindConfig, mismatches[0].Kind)
	assert.Equal(t, "config.fingerprint", mismatches[0].Key)
	assert.NotEqual(t, mismatches[0].Expected, mismatches[0].Actual)
}

func TestConfigChecker_EnvOverlayDoesNotDrift(t *testing.T) {
	tmp := t.TempDir()
	pinConfigEnv(t, tmp)
	path := filepath.Join(tmp, "config.yaml")
	err := os.WriteFile(path, []byte(minimalConfigYAML(tmp)+"logLevel: info\n"), 0644)
	require.NoError(t, err)

	t.Setenv("XG2G_LOG_LEVEL", "debug")

	cfg, err := config.NewLoader(path, "test-version").Load()
	require.NoError(t, err)
	require.Equal(t, "debug", cfg.LogLevel)

	provider := &mockConfigProvider{cfg: &cfg}
	c := checks.NewConfigChecker(path, provider)

	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mismatches)
}

type mockConfigProvider struct {
	cfg *config.AppConfig
}

func (m *mockConfigProvider) Current() *config.Snapshot {
	return &config.Snapshot{App: *m.cfg}
}

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

func TestRuntimeChecker_FFmpeg(t *testing.T) {
	runner := &mockRunner{
		output: []byte("ffmpeg version 8.1 Copyright (c) 2000-2026 the FFmpeg developers\n"),
	}

	c := checks.NewRuntimeChecker(runner, "", "8.1")
	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mismatches)

	// Verify cached behavior: changing runner output shouldn't matter now
	runner.output = []byte("ffmpeg version 6.0\n")
	mismatches, err = c.Check(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mismatches, "should use cached version")
}

func TestRuntimeChecker_Mismatch(t *testing.T) {
	runner := &mockRunner{
		output: []byte("ffmpeg version 6.0\n"),
	}
	c := checks.NewRuntimeChecker(runner, "", "8.1")

	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Equal(t, "runtime.ffmpeg.version", mismatches[0].Key)
	assert.Equal(t, "8.1", mismatches[0].Expected)
	assert.Equal(t, "6.0", mismatches[0].Actual)
}

// RealRunner implementation for real execution if needed
type RealRunner struct{}

func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
