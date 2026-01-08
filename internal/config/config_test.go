// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Set OWIBase to keep defaults deterministic for this test.
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
	if cfg.Enigma2.StreamPort != 8001 {
		t.Errorf("expected Enigma2.StreamPort=8001, got %d", cfg.Enigma2.StreamPort)
	}
	if cfg.Enigma2.Timeout != 10*time.Second {
		t.Errorf("expected Enigma2.Timeout=10s, got %v", cfg.Enigma2.Timeout)
	}
	if cfg.Enigma2.Retries != 2 { // Default is 2 now
		t.Errorf("expected Enigma2.Retries=2, got %d", cfg.Enigma2.Retries)
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
  tokenScopes:
    - v3:read
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
	if cfg.Enigma2.BaseURL != "http://custom.local" {
		t.Errorf("expected Enigma2.BaseURL=http://custom.local, got %s", cfg.Enigma2.BaseURL)
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
	if cfg.Enigma2.BaseURL != "http://env.local" {
		t.Errorf("expected ENV to override file: Enigma2.BaseURL=http://env.local, got %s", cfg.Enigma2.BaseURL)
	}
	if cfg.Enigma2.StreamPort != 7001 {
		t.Errorf("expected ENV to override file: Enigma2.StreamPort=7001, got %d", cfg.Enigma2.StreamPort)
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

	if cfg.Enigma2.StreamPort != 9001 {
		t.Errorf("expected Enigma2.StreamPort from file: 9001, got %d", cfg.Enigma2.StreamPort)
	}

	if cfg.EPGDays != 5 {
		t.Errorf("expected EPGDays from ENV: 5, got %d", cfg.EPGDays)
	}

	if cfg.Enigma2.BaseURL != "http://example.com" {
		t.Errorf("expected Enigma2.BaseURL from file: http://example.com, got %s", cfg.Enigma2.BaseURL)
	}
}

func TestWorkerEnvMergePreservesConfigValues(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Enabled = true
	cfg.Engine.Mode = "virtual"
	cfg.Engine.IdleTimeout = 45 * time.Second
	cfg.Engine.TunerSlots = []int{1, 3}

	cfg.Store.Backend = "bolt"
	cfg.Store.Path = "/tmp/xg2g-store"

	cfg.HLS.Root = "/tmp/xg2g-hls"
	cfg.HLS.DVRWindow = 1234 * time.Second

	cfg.Enigma2.BaseURL = "http://file-e2.local"
	cfg.Enigma2.Timeout = 12 * time.Second
	cfg.Enigma2.ResponseHeaderTimeout = 7 * time.Second
	cfg.Enigma2.Retries = 5
	cfg.Enigma2.RateLimit = 9
	cfg.Enigma2.RateBurst = 11
	cfg.Enigma2.UserAgent = "xg2g-test"

	unsetEnv(t, "XG2G_ENGINE_ENABLED")
	unsetEnv(t, "XG2G_ENGINE_MODE")
	unsetEnv(t, "XG2G_STORE_BACKEND")
	unsetEnv(t, "XG2G_STORE_PATH")
	unsetEnv(t, "XG2G_HLS_ROOT")
	unsetEnv(t, "XG2G_DVR_WINDOW")
	unsetEnv(t, "XG2G_ENGINE_IDLE_TIMEOUT")
	unsetEnv(t, "XG2G_E2_HOST")
	unsetEnv(t, "XG2G_E2_TIMEOUT")
	unsetEnv(t, "XG2G_E2_RESPONSE_HEADER_TIMEOUT")
	unsetEnv(t, "XG2G_E2_RETRIES")
	unsetEnv(t, "XG2G_E2_RATE_LIMIT")
	unsetEnv(t, "XG2G_E2_RATE_BURST")
	unsetEnv(t, "XG2G_E2_USER_AGENT")
	unsetEnv(t, "XG2G_TUNER_SLOTS")

	loader.mergeEnvConfig(&cfg)

	if cfg.Engine.Mode != "virtual" {
		t.Errorf("expected WorkerMode to remain %q, got %q", "virtual", cfg.Engine.Mode)
	}
	if cfg.Store.Backend != "bolt" {
		t.Errorf("expected StoreBackend to remain %q, got %q", "bolt", cfg.Store.Backend)
	}
	if cfg.Store.Path != "/tmp/xg2g-store" {
		t.Errorf("expected StorePath to remain %q, got %q", "/tmp/xg2g-store", cfg.Store.Path)
	}
	if cfg.HLS.Root != "/tmp/xg2g-hls" {
		t.Errorf("expected HLSRoot to remain %q, got %q", "/tmp/xg2g-hls", cfg.HLS.Root)
	}
	if cfg.HLS.DVRWindow != 1234*time.Second {
		t.Errorf("expected DVRWindow to remain %v, got %v", 1234*time.Second, cfg.HLS.DVRWindow)
	}
	if cfg.Engine.IdleTimeout != 45*time.Second {
		t.Errorf("expected IdleTimeout to remain %v, got %v", 45*time.Second, cfg.Engine.IdleTimeout)
	}
	if cfg.Enigma2.BaseURL != "http://file-e2.local" {
		t.Errorf("expected E2Host to remain %q, got %q", "http://file-e2.local", cfg.Enigma2.BaseURL)
	}
	if cfg.Enigma2.Timeout != 12*time.Second {
		t.Errorf("expected E2Timeout to remain %v, got %v", 12*time.Second, cfg.Enigma2.Timeout)
	}
	if cfg.Enigma2.ResponseHeaderTimeout != 7*time.Second {
		t.Errorf("expected E2RespTimeout to remain %v, got %v", 7*time.Second, cfg.Enigma2.ResponseHeaderTimeout)
	}
	if cfg.Enigma2.Retries != 5 {
		t.Errorf("expected E2Retries to remain %d, got %d", 5, cfg.Enigma2.Retries)
	}
	if cfg.Enigma2.RateLimit != 9 {
		t.Errorf("expected E2RateLimit to remain %d, got %d", 9, cfg.Enigma2.RateLimit)
	}
	if cfg.Enigma2.RateBurst != 11 {
		t.Errorf("expected E2RateBurst to remain %d, got %d", 11, cfg.Enigma2.RateBurst)
	}
	if cfg.Enigma2.UserAgent != "xg2g-test" {
		t.Errorf("expected E2UserAgent to remain %q, got %q", "xg2g-test", cfg.Enigma2.UserAgent)
	}
	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{1, 3}) {
		t.Errorf("expected TunerSlots to remain [1 3], got %v", cfg.Engine.TunerSlots)
	}
}

func TestInvalidTunerSlotsEnvPreservesConfig(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Mode = "standard"
	cfg.Engine.TunerSlots = []int{2, 4}

	t.Setenv("XG2G_TUNER_SLOTS", "bad-slots")

	loader.mergeEnvConfig(&cfg)

	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{2, 4}) {
		t.Errorf("expected TunerSlots to remain [2 4], got %v", cfg.Engine.TunerSlots)
	}
}

func TestEmptyTunerSlotsEnvPreservesConfig(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Mode = "standard"
	cfg.Engine.TunerSlots = []int{5}

	t.Setenv("XG2G_TUNER_SLOTS", "")

	loader.mergeEnvConfig(&cfg)

	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{5}) {
		t.Errorf("expected TunerSlots to remain [5], got %v", cfg.Engine.TunerSlots)
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
					DataDir: tmpDir,
					Enigma2: Enigma2Settings{
						BaseURL:    "http://test.local",
						StreamPort: 8001,
					},
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
					DataDir: tmpDir,
					Enigma2: Enigma2Settings{
						BaseURL:    "http://test.local",
						StreamPort: 8001,
					},
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
					DataDir: tmpDir,
					Enigma2: Enigma2Settings{
						BaseURL:    "http://test.local",
						StreamPort: 8001,
					},
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

			// Set OWIBase for test clarity
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
	if cfg.Enigma2.MaxBackoff != 10*time.Second {
		t.Errorf("expected ENV to override file: Enigma2.MaxBackoff=10s, got %v", cfg.Enigma2.MaxBackoff)
	}

	// Verify backoff from file is still loaded
	if cfg.Enigma2.Backoff != 500*time.Millisecond {
		t.Errorf("expected backoff from file: 500ms, got %v", cfg.Enigma2.Backoff)
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
	if cfg.Enigma2.Timeout != 15*time.Second {
		t.Errorf("expected Enigma2.Timeout=15s, got %v", cfg.Enigma2.Timeout)
	}
	if cfg.Enigma2.Retries != 5 {
		t.Errorf("expected Enigma2.Retries=5, got %d", cfg.Enigma2.Retries)
	}
	if cfg.Enigma2.Backoff != 300*time.Millisecond {
		t.Errorf("expected Enigma2.Backoff=300ms, got %v", cfg.Enigma2.Backoff)
	}
	if cfg.Enigma2.MaxBackoff != 5*time.Second {
		t.Errorf("expected Enigma2.MaxBackoff=5s, got %v", cfg.Enigma2.MaxBackoff)
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

			// Set OWIBase for test clarity
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
				if cfg.Enigma2.MaxBackoff != 30*time.Second {
					t.Errorf("%s: expected default Enigma2.MaxBackoff=30s, got %v",
						tt.description, cfg.Enigma2.MaxBackoff)
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

func TestParseScopedTokensFromEnv(t *testing.T) {
	// Set valid OWIBase to satisfy validation (Enigma2 inherits this)
	t.Setenv("XG2G_OWI_BASE", "http://example.com")

	t.Run("json_format", func(t *testing.T) {
		t.Setenv("XG2G_API_TOKENS", `[{"token":"read","scopes":["v3:read"]},{"token":"ops","scopes":["v3:read","v3:write"]}]`)

		loader := NewLoader("", "test")
		cfg, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}
		if len(cfg.APITokens) != 2 {
			t.Fatalf("expected 2 scoped tokens, got %d", len(cfg.APITokens))
		}
		if cfg.APITokens[0].Token != "read" {
			t.Fatalf("expected first token 'read', got %q", cfg.APITokens[0].Token)
		}
	})

	t.Run("legacy_format", func(t *testing.T) {
		t.Setenv("XG2G_API_TOKENS", "read=v3:read;ops=v3:read,v3:write")

		loader := NewLoader("", "test")
		cfg, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}
		if len(cfg.APITokens) != 2 {
			t.Fatalf("expected 2 scoped tokens, got %d", len(cfg.APITokens))
		}
	})

	t.Run("invalid_json_errors", func(t *testing.T) {
		t.Setenv("XG2G_API_TOKENS", `[{"token":]`)

		loader := NewLoader("", "test")
		_, err := loader.Load()
		if err == nil || !containsString(err.Error(), "XG2G_API_TOKENS") {
			t.Fatalf("expected XG2G_API_TOKENS error, got %v", err)
		}
	})

	t.Run("legacy_missing_scopes_errors", func(t *testing.T) {
		t.Setenv("XG2G_API_TOKENS", "read=")

		loader := NewLoader("", "test")
		_, err := loader.Load()
		if err == nil || !containsString(err.Error(), "XG2G_API_TOKENS") {
			t.Fatalf("expected XG2G_API_TOKENS error, got %v", err)
		}
	})

	t.Run("unknown_scope_errors", func(t *testing.T) {
		t.Setenv("XG2G_API_TOKENS", `[{"token":"read","scopes":["v3:unknown"]}]`)

		loader := NewLoader("", "test")
		_, err := loader.Load()
		if err == nil || !containsString(err.Error(), "unknown scope") {
			t.Fatalf("expected unknown scope error, got %v", err)
		}
	})
}

func TestAPITokenRequiresScopes(t *testing.T) {
	t.Setenv("XG2G_API_TOKEN", "token-only")

	loader := NewLoader("", "test")
	_, err := loader.Load()
	if err == nil || !containsString(err.Error(), "APITokenScopes") {
		t.Fatalf("expected APITokenScopes error, got %v", err)
	}
}

func TestYAMLRejectsMissingTokenScopes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
api:
  token: test-token
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "test")
	_, err := loader.Load()
	if err == nil || !containsString(err.Error(), "APITokenScopes") {
		t.Fatalf("expected APITokenScopes error, got %v", err)
	}
}

func TestYAMLRejectsTokenWithoutScopes(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
api:
  tokens:
    - token: extra-token
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "test")
	_, err := loader.Load()
	if err == nil || !containsString(err.Error(), "APITokens") {
		t.Fatalf("expected APITokens error, got %v", err)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	if val, ok := os.LookupEnv(key); ok {
		_ = os.Unsetenv(key)
		t.Cleanup(func() { _ = os.Setenv(key, val) })
		return
	}
	t.Cleanup(func() { _ = os.Unsetenv(key) })
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
