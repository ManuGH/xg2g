package checks_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/verification"
	"github.com/ManuGH/xg2g/internal/verification/checks"
)

func TestConfigChecker_Drift(t *testing.T) {
	// Setup declared config file
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")

	// AppConfig matching declared
	cfg := &config.AppConfig{LogLevel: "info"}

	// We must write the correct key/values to matches ToFileConfig logic (which populates all fields).
	// We use YAML marshal to ensure tags match what the loader expects.
	fullCfg := config.ToFileConfig(cfg)
	data, _ := yaml.Marshal(fullCfg)
	err := os.WriteFile(path, data, 0644)
	require.NoError(t, err)

	provider := &mockConfigProvider{cfg: cfg}
	c := checks.NewConfigChecker(path, provider)
	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	assert.Empty(t, mismatches)

	// Modify AppConfig (Simulate Env Var Override)
	cfg.LogLevel = "debug"
	mismatches, err = c.Check(context.Background())
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Equal(t, verification.KindConfig, mismatches[0].Kind)
	assert.Equal(t, "config.fingerprint", mismatches[0].Key)
	assert.NotEqual(t, mismatches[0].Expected, mismatches[0].Actual)
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
		output: []byte("ffmpeg version 7.1.3 Copyright (c) 2000-2024 the FFmpeg developers\n"),
	}

	c := checks.NewRuntimeChecker(runner, "", "7.1.3")
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
	c := checks.NewRuntimeChecker(runner, "", "7.1.3")

	mismatches, err := c.Check(context.Background())
	require.NoError(t, err)
	require.Len(t, mismatches, 1)
	assert.Equal(t, "runtime.ffmpeg.version", mismatches[0].Key)
	assert.Equal(t, "7.1.3", mismatches[0].Expected)
	assert.Equal(t, "6.0", mismatches[0].Actual)
}

// RealRunner implementation for real execution if needed
type RealRunner struct{}

func (r *RealRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}
