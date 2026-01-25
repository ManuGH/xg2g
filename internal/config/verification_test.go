package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerification_Defaults(t *testing.T) {
	// Use t.Setenv for automatic cleanup and isolation
	t.Setenv("XG2G_E2_HOST", "http://localhost")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Verification.Enabled)
	assert.Equal(t, 60*time.Second, cfg.Verification.Interval)
}

func TestVerification_EnvOverride_Disabled(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://localhost")
	t.Setenv("XG2G_VERIFY_ENABLED", "false")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.False(t, cfg.Verification.Enabled)
}

func TestVerification_EnvOverride_Interval(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://localhost")
	t.Setenv("XG2G_VERIFY_INTERVAL", "30s")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, cfg.Verification.Interval)
}
