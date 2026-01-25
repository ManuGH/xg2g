package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerification_Defaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("XG2G_E2_HOST", "http://localhost") // Required
	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Verification.Enabled)
	assert.Equal(t, 60*time.Second, cfg.Verification.Interval)
}

func TestVerification_EnvOverride_Disabled(t *testing.T) {
	os.Clearenv()
	os.Setenv("XG2G_E2_HOST", "http://localhost")
	os.Setenv("XG2G_VERIFY_ENABLED", "false")
	defer os.Unsetenv("XG2G_VERIFY_ENABLED")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.False(t, cfg.Verification.Enabled)
}

func TestVerification_EnvOverride_Interval(t *testing.T) {
	os.Clearenv()
	os.Setenv("XG2G_E2_HOST", "http://localhost")
	os.Setenv("XG2G_VERIFY_INTERVAL", "30s")
	defer os.Unsetenv("XG2G_VERIFY_INTERVAL")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, cfg.Verification.Interval)
}
