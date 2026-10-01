// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBootstrapConfigDir(t *testing.T, iptvSecret string) string {
	t.Helper()
	skipIfNoFFmpeg(t)

	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	tmpDir, err := os.MkdirTemp("", "xg2g-bootstrap-iptv-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	if iptvSecret != "" {
		t.Setenv("XG2G_IPTV_SOURCE_SECRET", iptvSecret)
	} else {
		t.Setenv("XG2G_IPTV_SOURCE_SECRET", "")
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver.invalid
  username: root
  password: "dummy-password"
recordings:
  target_signing_key: "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	return configPath
}

func TestBootstrap_IPTVResolver_DisabledWhenSecretUnset(t *testing.T) {
	configPath := setupBootstrapConfigDir(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.NoError(t, err)
	defer container.Close()

	// Invariant: secret unset => resolver nil, startup OK
	assert.Nil(t, container.IPTVResolver, "container.IPTVResolver must be nil when secret unset")
	assert.Nil(t, container.Server.IPTVResolver(), "server.IPTVResolver() must be nil when secret unset")
}

func TestBootstrap_IPTVResolver_EnabledWhenSecretSet(t *testing.T) {
	validSecret := "0123456789abcdef0123456789abcdef" // exactly 32 bytes
	configPath := setupBootstrapConfigDir(t, validSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.NoError(t, err)
	defer container.Close()

	// Invariant: secret set => resolver present and wired to server
	assert.NotNil(t, container.IPTVResolver, "container.IPTVResolver must be present when secret set")
	assert.NotNil(t, container.Server.IPTVResolver(), "server.IPTVResolver() must be present when secret set")

	// Resolving a non-IPTV ref behaves as pass-through
	raw, kind, err := container.IPTVResolver.ResolveInbound("intents", "1:0:19:1:1:1:C00000:0:0:0:")
	require.NoError(t, err)
	assert.Equal(t, "1:0:19:1:1:1:C00000:0:0:0:", raw)
	assert.Equal(t, edge.KindPassThrough, kind)
}

func TestBootstrap_IPTVResolver_StartupErrorNamesFieldNeverSecret(t *testing.T) {
	// Directly test that any error in parsing names IPTVSourceSecret and never the secret value
	fakeSecret := "short-secret"
	cfg := wireBootstrapState{
		cfg: config.AppConfig{
			IPTVSourceSecret: fakeSecret,
		},
	}
	_ = cfg

	// When an invalid secret is provided (bypassing earlier validation), WireServices fails naming the field
	secretMarker := "CANARY_SECRET_VAL_DO_NOT_LEAK"
	// Create config with invalid length secret (10 chars < 32 bytes)
	// Note: config loader might reject it, but if it reaches bootstrap, error names field not value
	configPath := setupBootstrapConfigDir(t, secretMarker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.Error(t, err)

	errStr := err.Error()
	// Must name the field
	assert.True(t, strings.Contains(errStr, "IPTVSourceSecret") || strings.Contains(errStr, "iptv_source_secret"),
		"startup error must name configuration field")
	// Must never leak the secret value
	assert.False(t, strings.Contains(errStr, secretMarker),
		"startup error must NEVER contain secret value")
}
