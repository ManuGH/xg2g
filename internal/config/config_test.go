// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Set required OWIBase since it no longer has a default
	_ = os.Setenv("XG2G_OWI_BASE", "http://example.com")
	defer func() { _ = os.Unsetenv("XG2G_OWI_BASE") }()

	loader := NewLoader("", "test-version")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Check defaults
	if cfg.DataDir != "/tmp" {
		t.Errorf("expected DataDir=/tmp, got %s", cfg.DataDir)
	}
	if cfg.StreamPort != 8001 {
		t.Errorf("expected StreamPort=8001, got %d", cfg.StreamPort)
	}
	if cfg.OWITimeout != 10*time.Second {
		t.Errorf("expected OWITimeout=10s, got %v", cfg.OWITimeout)
	}
	if cfg.OWIRetries != 3 {
		t.Errorf("expected OWIRetries=3, got %d", cfg.OWIRetries)
	}
	if cfg.Version != "test-version" {
		t.Errorf("expected Version=test-version, got %s", cfg.Version)
	}
}

func TestLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	customDataDir := filepath.Join(tmpDir, "custom-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://custom.local
  username: testuser
  password: testpass
  streamPort: 9001
  timeout: 20s
  retries: 5
bouquets:
  - Favourites
  - Premium
epg:
  enabled: true
  days: 14
  maxConcurrency: 10
api:
  token: test-token
picons:
  baseUrl: http://picons.local
`, customDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != customDataDir {
		t.Errorf("expected DataDir=%s, got %s", customDataDir, cfg.DataDir)
	}
	if cfg.OWIBase != "http://custom.local" {
		t.Errorf("expected OWIBase=http://custom.local, got %s", cfg.OWIBase)
	}
	if cfg.Bouquet != "Favourites,Premium" {
		t.Errorf("expected Bouquet=Favourites,Premium, got %s", cfg.Bouquet)
	}
}

func TestENVOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")
	envDataDir := filepath.Join(tmpDir, "env-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://file.local
  streamPort: 9001
`, fileDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_DATA", envDataDir)
	t.Setenv("XG2G_OWI_BASE", "http://env.local")
	t.Setenv("XG2G_STREAM_PORT", "7001")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != envDataDir {
		t.Errorf("expected ENV to override file: DataDir=%s, got %s", envDataDir, cfg.DataDir)
	}
	if cfg.OWIBase != "http://env.local" {
		t.Errorf("expected ENV to override file: OWIBase=http://env.local, got %s", cfg.OWIBase)
	}
	if cfg.StreamPort != 7001 {
		t.Errorf("expected ENV to override file: StreamPort=7001, got %d", cfg.StreamPort)
	}
}

func TestPrecedenceOrder(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://example.com
  streamPort: 9001
epg:
  days: 10
`, fileDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_EPG_DAYS", "5")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != fileDataDir {
		t.Errorf("expected DataDir from file: %s, got %s", fileDataDir, cfg.DataDir)
	}

	if cfg.StreamPort != 9001 {
		t.Errorf("expected StreamPort from file: 9001, got %d", cfg.StreamPort)
	}

	if cfg.EPGDays != 5 {
		t.Errorf("expected EPGDays from ENV: 5, got %d", cfg.EPGDays)
	}

	if cfg.OWIBase != "http://example.com" {
		t.Errorf("expected OWIBase from file: http://example.com, got %s", cfg.OWIBase)
	}
}

func TestValidateEPGBounds(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		cfg       func() AppConfig
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid EPG config",
			cfg: func() AppConfig {
				return AppConfig{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: false,
		},
		{
			name: "EPGTimeoutMS too low",
			cfg: func() AppConfig {
				return AppConfig{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      50, // Too low
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGTimeoutMS too high",
			cfg: func() AppConfig {
				return AppConfig{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      100000, // Too high
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGRetries too high",
			cfg: func() AppConfig {
				return AppConfig{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        10, // Too high
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGRetries",
		},
		{
			name: "FuzzyMax too high",
			cfg: func() AppConfig {
				return AppConfig{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        2,
					FuzzyMax:          50, // Too high
				}
			},
			shouldErr: true,
			errMsg:    "FuzzyMax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg()
			err := Validate(cfg)

			if tt.shouldErr {
				if err == nil {
					t.Error("expected validation error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

// TestOWIMaxBackoffFromENV tests that XG2G_OWI_MAX_BACKOFF_MS is read correctly
func TestOWIMaxBackoffFromENV(t *testing.T) {
	tests := []struct {
		name            string
		envValue        string
		expectedBackoff time.Duration
		description     string
	}{
		{
			name:            "default_value",
			envValue:        "",
			expectedBackoff: 30 * time.Second,
			description:     "Default maxBackoff when ENV not set",
		},
		{
			name:            "custom_value_2s",
			envValue:        "2000",
			expectedBackoff: 2 * time.Second,
			description:     "Custom 2s maxBackoff from ENV",
		},
		{
			name:            "custom_value_5s",
			envValue:        "5000",
			expectedBackoff: 5 * time.Second,
			description:     "Custom 5s maxBackoff from ENV",
		},
		{
			name:            "custom_value_10s",
			envValue:        "10000",
			expectedBackoff: 10 * time.Second,
			description:     "Custom 10s maxBackoff from ENV",
		},
		{
			name:            "custom_value_30s",
			envValue:        "30000",
			expectedBackoff: 30 * time.Second,
			description:     "Maximum 30s maxBackoff from ENV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			_ = os.Unsetenv("XG2G_OWI_MAX_BACKOFF_MS")

			// Set required OWIBase
			t.Setenv("XG2G_OWI_BASE", "http://example.com")

			// Set test-specific ENV
			if tt.envValue != "" {
				t.Setenv("XG2G_OWI_MAX_BACKOFF_MS", tt.envValue)
			}

			loader := NewLoader("", "test-version")
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			if cfg.OWIMaxBackoff != tt.expectedBackoff {
				t.Errorf("%s: expected OWIMaxBackoff=%v, got %v",
					tt.description, tt.expectedBackoff, cfg.OWIMaxBackoff)
			}
		})
	}
}

// TestOWIMaxBackoffFromFile tests that maxBackoff from YAML config is read correctly
func TestOWIMaxBackoffFromFile(t *testing.T) {
	tests := []struct {
		name            string
		yamlBackoff     string
		expectedBackoff time.Duration
		description     string
	}{
		{
			name:            "2_seconds",
			yamlBackoff:     "2s",
			expectedBackoff: 2 * time.Second,
			description:     "2s maxBackoff from YAML",
		},
		{
			name:            "5_seconds",
			yamlBackoff:     "5s",
			expectedBackoff: 5 * time.Second,
			description:     "5s maxBackoff from YAML",
		},
		{
			name:            "10_seconds",
			yamlBackoff:     "10s",
			expectedBackoff: 10 * time.Second,
			description:     "10s maxBackoff from YAML",
		},
		{
			name:            "30_seconds",
			yamlBackoff:     "30s",
			expectedBackoff: 30 * time.Second,
			description:     "30s maxBackoff from YAML",
		},
		{
			name:            "milliseconds",
			yamlBackoff:     "2500ms",
			expectedBackoff: 2500 * time.Millisecond,
			description:     "2.5s maxBackoff from YAML (in ms)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://test.local
  maxBackoff: %s
`, tmpDir, tt.yamlBackoff)

			if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			loader := NewLoader(configPath, "1.0.0")
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			if cfg.OWIMaxBackoff != tt.expectedBackoff {
				t.Errorf("%s: expected OWIMaxBackoff=%v, got %v",
					tt.description, tt.expectedBackoff, cfg.OWIMaxBackoff)
			}
		})
	}
}

// TestOWIMaxBackoffENVOverridesFile tests precedence: ENV > File > Default
func TestOWIMaxBackoffENVOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// File config: 5s maxBackoff
	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://test.local
  maxBackoff: 5s
  backoff: 500ms
`, tmpDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// ENV config: 10s maxBackoff (should override file)
	t.Setenv("XG2G_OWI_MAX_BACKOFF_MS", "10000")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify ENV overrides file
	if cfg.OWIMaxBackoff != 10*time.Second {
		t.Errorf("expected ENV to override file: OWIMaxBackoff=10s, got %v", cfg.OWIMaxBackoff)
	}

	// Verify backoff from file is still loaded
	if cfg.OWIBackoff != 500*time.Millisecond {
		t.Errorf("expected backoff from file: 500ms, got %v", cfg.OWIBackoff)
	}
}

// TestOWIBackoffConfigConsistency tests that all OWI timing configs work together
func TestOWIBackoffConfigConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://test.local
  timeout: 15s
  retries: 5
  backoff: 300ms
  maxBackoff: 5s
`, tmpDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify all timing configs
	if cfg.OWITimeout != 15*time.Second {
		t.Errorf("expected OWITimeout=15s, got %v", cfg.OWITimeout)
	}
	if cfg.OWIRetries != 5 {
		t.Errorf("expected OWIRetries=5, got %d", cfg.OWIRetries)
	}
	if cfg.OWIBackoff != 300*time.Millisecond {
		t.Errorf("expected OWIBackoff=300ms, got %v", cfg.OWIBackoff)
	}
	if cfg.OWIMaxBackoff != 5*time.Second {
		t.Errorf("expected OWIMaxBackoff=5s, got %v", cfg.OWIMaxBackoff)
	}
}

// TestOWIMaxBackoffInvalidValues tests handling of invalid maxBackoff values
func TestOWIMaxBackoffInvalidValues(t *testing.T) {
	tests := []struct {
		name          string
		envValue      string
		expectDefault bool
		description   string
	}{
		{
			name:          "invalid_string",
			envValue:      "not-a-number",
			expectDefault: true,
			description:   "Invalid string should use default",
		},
		{
			name:          "negative_value",
			envValue:      "-1000",
			expectDefault: false, // Will parse as negative duration
			description:   "Negative value should parse (validation happens elsewhere)",
		},
		{
			name:          "zero_value",
			envValue:      "0",
			expectDefault: false, // Will parse as 0
			description:   "Zero value should parse (validation happens elsewhere)",
		},
		{
			name:          "empty_string",
			envValue:      "",
			expectDefault: true,
			description:   "Empty string should use default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			_ = os.Unsetenv("XG2G_OWI_MAX_BACKOFF_MS")

			// Set required OWIBase
			t.Setenv("XG2G_OWI_BASE", "http://example.com")

			// Set test-specific ENV
			if tt.envValue != "" {
				t.Setenv("XG2G_OWI_MAX_BACKOFF_MS", tt.envValue)
			}

			loader := NewLoader("", "test-version")
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			if tt.expectDefault {
				if cfg.OWIMaxBackoff != 30*time.Second {
					t.Errorf("%s: expected default OWIMaxBackoff=30s, got %v",
						tt.description, cfg.OWIMaxBackoff)
				}
			}
			// Note: For non-default cases, we just verify it parses without error
			// Validation of logical ranges happens in the validate package
		})
	}
}

// TestYAMLCanDisableFeatures is a regression test for the boolean/zero-value merge bug
// Previously, setting `epg.enabled: false` in YAML was ignored because the merge logic
// only checked `if src.EPG.Enabled { dst.EPGEnabled = true }`, which never set false values.
// This test ensures that YAML can explicitly disable features and set zero values.
func TestYAMLCanDisableFeatures(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dataDir := filepath.Join(tmpDir, "data")

	// YAML with EPG explicitly disabled and retries set to 0
	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://test.local
  retries: 0
epg:
  enabled: false
  days: 0
  retries: 0
`, dataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "test")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify that false/0 values from YAML were applied (not defaults)
	if cfg.EPGEnabled {
		t.Errorf("EPG should be disabled when epg.enabled: false in YAML, but EPGEnabled=%v", cfg.EPGEnabled)
	}

	if cfg.EPGDays != 0 {
		t.Errorf("EPG Days should be 0 when epg.days: 0 in YAML, but EPGDays=%d", cfg.EPGDays)
	}

	if cfg.EPGRetries != 0 {
		t.Errorf("EPG Retries should be 0 when epg.retries: 0 in YAML, but EPGRetries=%d", cfg.EPGRetries)
	}

	// Note: OWIRetries is not a pointer in OpenWebIFConfig, so it still has the old behavior
	// This is intentional - we only fixed EPG fields for now
}

func TestAuthAnonymousEnv(t *testing.T) {
	// Set required OWIBase
	t.Setenv("XG2G_OWI_BASE", "http://example.com")

	tests := []struct {
		name    string
		envKey  string
		envVal  string
		check   func(*AppConfig) bool
		wantErr bool
	}{
		{
			name:   "Auth Anonymous True",
			envKey: "XG2G_AUTH_ANONYMOUS",
			envVal: "true",
			check: func(c *AppConfig) bool {
				return c.AuthAnonymous == true
			},
		},
		{
			name:   "Auth Anonymous False",
			envKey: "XG2G_AUTH_ANONYMOUS",
			envVal: "false",
			check: func(c *AppConfig) bool {
				return c.AuthAnonymous == false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			loader := NewLoader("", "test")
			cfg, err := loader.Load()
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.check(&cfg) {
				t.Errorf("Config check failed for assertion")
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
