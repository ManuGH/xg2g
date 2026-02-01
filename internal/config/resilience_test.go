// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupConfigTest is a helper to initialize a config loader and satisfy strict validation.
func setupConfigTest(t *testing.T) (*Loader, func(*AppConfig)) {
	l := NewLoader("", "v3")

	// satisfy strict validation
	t.Setenv("XG2G_OWI_BASE", "http://mock")
	t.Setenv("XG2G_STORE_PATH", "/tmp/store")
	t.Setenv("XG2G_HLS_ROOT", "/tmp/hls")

	// Helper to make config valid for strict mode
	makeValid := func(cfg *AppConfig) {
		cfg.Enigma2.BaseURL = "http://mock"
		cfg.Store.Path = "/tmp/store"
		cfg.HLS.Root = "/tmp/hls"
	}

	return l, makeValid
}

func TestResilienceDefaults(t *testing.T) {
	l, _ := setupConfigTest(t)

	// WHEN loading with no inputs
	cfg, err := l.Load()
	require.NoError(t, err)

	// THEN defaults should be operator-grade
	assert.Equal(t, 8, cfg.Limits.MaxSessions)
	assert.Equal(t, 2, cfg.Limits.MaxTranscodes)
	assert.True(t, cfg.Engine.Enabled) // Default from registry

	assert.Equal(t, 15*time.Second, cfg.Timeouts.TranscodeStart)
	assert.Equal(t, 30*time.Second, cfg.Timeouts.TranscodeNoProgress)
	assert.Equal(t, 2*time.Second, cfg.Timeouts.KillGrace)

	assert.Equal(t, 5*time.Minute, cfg.Breaker.Window)
	assert.Equal(t, 10, cfg.Breaker.MinAttempts)
	assert.Equal(t, 7, cfg.Breaker.FailuresThreshold)
	assert.Equal(t, 5, cfg.Breaker.ConsecutiveThreshold)
}

func TestResilienceZeroOverrides(t *testing.T) {
	// GIVEN a config loader
	l := NewLoader("", "v3")

	// satisfy strict validation
	t.Setenv("XG2G_OWI_BASE", "http://mock")
	t.Setenv("XG2G_STORE_PATH", "/tmp/store")
	t.Setenv("XG2G_HLS_ROOT", "/tmp/hls")

	// WHEN explicitly disabling transcodes via ENV
	t.Setenv("XG2G_MAX_TRANSCODES", "0") // Presume registered as limits.max_transcodes -> XG2G_MAX_TRANSCODES? No, we need to check registryEnv.

	// Wait, registry.go has:
	// {Path: "limits.max_transcodes", Env: "", ...}
	// So we can't use ENV yet unless we register it or use YAML.
	// Let's use YAML loading simulation or modify registry for test.
	// Actually, let's just use the fact that mergeFileConfig handles 0 correctly, and setDefaults should NOT override it.

	// Simulation:
	cfg := AppConfig{}
	// 1. Defaults applied (max_transcodes = 2)
	registry, _ := GetRegistry()
	registry.ApplyDefaults(&cfg)
	assert.Equal(t, 2, cfg.Limits.MaxTranscodes)

	// 2. YAML override (max_transcodes = 0)
	yamlSrc := FileConfig{
		Limits: &LimitsConfig{MaxTranscodes: 0},
	}
	// Initial state before merge: defaults are set.
	// But mergeFileConfig should effectively "rewrite" if it sees explicit 0?
	// No, mergeFileConfig writes to dst.

	// The problem described:
	// if src.Limits.MaxTranscodes >= 0 { dst.Limits.MaxTranscodes = src.Limits.MaxTranscodes }

	// So if we have default=2 in dst, and src has 0.
	l.mergeFileConfig(&cfg, &yamlSrc)

	// THEN it should be 0
	assert.Equal(t, 0, cfg.Limits.MaxTranscodes, "Explicit 0 in YAML must override default 2")
}

func TestFailOpenBooleans(t *testing.T) {
	_, _ = setupConfigTest(t)
	// WE need manually load here to inject YAML.

	// GIVEN a config with Engine.Enabled default=true (from registry)
	// WHEN we provide YAML with enabled: false
	yamlSrc := FileConfig{
		Engine: EngineFileConfig{
			Enabled: boolPtr(false),
		},
		Library: LibraryFileConfig{
			Enabled: boolPtr(false),
		},
	}

	cfg := AppConfig{}
	// Apply registry defaults
	reg, _ := GetRegistry()
	reg.ApplyDefaults(&cfg)
	assert.True(t, cfg.Engine.Enabled, "Registry default should be true")

	// Merge YAML
	l := NewLoader("", "v3")
	l.mergeFileConfig(&cfg, &yamlSrc)

	// THEN it should be false
	assert.False(t, cfg.Engine.Enabled, "YAML explicit false must override default true")
	assert.False(t, cfg.Library.Enabled, "YAML explicit false must override default true (Library)")
}

func TestLimitsEnv(t *testing.T) {
	l, _ := setupConfigTest(t)

	// WHEN setting limits via ENV
	t.Setenv("XG2G_MAX_SESSIONS", "42")
	t.Setenv("XG2G_MAX_TRANSCODES", "10")

	cfg, err := l.Load()
	require.NoError(t, err)

	// THEN config should reflect ENV
	assert.Equal(t, 42, cfg.Limits.MaxSessions)
	assert.Equal(t, 10, cfg.Limits.MaxTranscodes)
}

func TestResilienceValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*AppConfig)
		wantError string
	}{
		{
			name: "MaxSessions < 1",
			mutate: func(c *AppConfig) {
				c.Limits.MaxSessions = 0
			},
			wantError: "Limits.MaxSessions",
		},
		{
			name: "MaxTranscodes < 0",
			mutate: func(c *AppConfig) {
				c.Limits.MaxTranscodes = -1
			},
			wantError: "Limits.MaxTranscodes",
		},
		{
			name: "TranscodeStart <= 0",
			mutate: func(c *AppConfig) {
				c.Timeouts.TranscodeStart = 0
			},
			wantError: "Timeouts.TranscodeStart",
		},
		{
			name: "TranscodeNoProgress <= TranscodeStart",
			mutate: func(c *AppConfig) {
				c.Timeouts.TranscodeStart = 10 * time.Second
				c.Timeouts.TranscodeNoProgress = 10 * time.Second
			},
			wantError: "Timeouts.TranscodeNoProgress",
		},
		{
			name: "KillGrace <= 0",
			mutate: func(c *AppConfig) {
				c.Timeouts.KillGrace = 0
			},
			wantError: "Timeouts.KillGrace",
		},
		{
			name: "KillGrace >= TranscodeNoProgress",
			mutate: func(c *AppConfig) {
				c.Timeouts.KillGrace = 20 * time.Second
				c.Timeouts.TranscodeNoProgress = 10 * time.Second
			},
			wantError: "Timeouts.KillGrace",
		},
		{
			name: "Breaker.Window <= 0",
			mutate: func(c *AppConfig) {
				c.Breaker.Window = 0
			},
			wantError: "Breaker.Window",
		},
		{
			name: "Breaker.MinAttempts < 1",
			mutate: func(c *AppConfig) {
				c.Breaker.MinAttempts = 0
			},
			wantError: "Breaker.MinAttempts",
		},
		{
			name: "Breaker.FailuresThreshold < 1",
			mutate: func(c *AppConfig) {
				c.Breaker.FailuresThreshold = 0
			},
			wantError: "Breaker.FailuresThreshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// satisfy strict validation
			t.Setenv("XG2G_OWI_BASE", "http://mock")
			t.Setenv("XG2G_STORE_PATH", "/tmp/store")
			t.Setenv("XG2G_HLS_ROOT", "/tmp/hls")

			// Start with valid config
			l := NewLoader("", "v3")
			cfg, err := l.Load()
			require.NoError(t, err)

			// Mutate to invalid
			tt.mutate(&cfg)

			// Validate directly
			err = Validate(cfg)
			if tt.wantError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
