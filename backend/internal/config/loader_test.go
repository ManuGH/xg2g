// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Set receiver base URL to keep defaults deterministic for this test.
	_ = os.Setenv("XG2G_E2_HOST", "http://example.com")
	_ = os.Setenv("XG2G_STORE_PATH", t.TempDir())
	defer func() {
		_ = os.Unsetenv("XG2G_E2_HOST")
		_ = os.Unsetenv("XG2G_STORE_PATH")
	}()

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
	if cfg.Playback.Operator.ForceIntent != "" {
		t.Errorf("expected Playback.Operator.ForceIntent default empty, got %q", cfg.Playback.Operator.ForceIntent)
	}
	if cfg.Playback.Operator.MaxQualityRung != "" {
		t.Errorf("expected Playback.Operator.MaxQualityRung default empty, got %q", cfg.Playback.Operator.MaxQualityRung)
	}
	if cfg.Playback.Operator.DisableClientFallback {
		t.Error("expected Playback.Operator.DisableClientFallback default false")
	}
	if len(cfg.Playback.Operator.SourceRules) != 0 {
		t.Errorf("expected Playback.Operator.SourceRules default empty, got %d entries", len(cfg.Playback.Operator.SourceRules))
	}
}

func TestLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")
	customDataDir := filepath.Join(tmpDir, "custom-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
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

func TestLoadFromYAMLHLSReadySegments(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
enigma2:
  baseUrl: http://custom.local
hls:
  readySegments: 5
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.HLS.ReadySegments != 5 {
		t.Fatalf("expected HLS.ReadySegments=5, got %d", cfg.HLS.ReadySegments)
	}
}

func TestLoadPlaybackOperatorFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
enigma2:
  baseUrl: http://custom.local
playback:
  operator:
    force_intent: quality
    max_quality_rung: quality_audio_aac_320_stereo
    disable_client_fallback: true
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Playback.Operator.ForceIntent != "quality" {
		t.Fatalf("expected Playback.Operator.ForceIntent=quality, got %q", cfg.Playback.Operator.ForceIntent)
	}
	if cfg.Playback.Operator.MaxQualityRung != "quality_audio_aac_320_stereo" {
		t.Fatalf("expected Playback.Operator.MaxQualityRung=quality_audio_aac_320_stereo, got %q", cfg.Playback.Operator.MaxQualityRung)
	}
	if !cfg.Playback.Operator.DisableClientFallback {
		t.Fatal("expected Playback.Operator.DisableClientFallback=true")
	}
}

func TestENVOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")
	envDataDir := filepath.Join(tmpDir, "env-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
  baseUrl: http://file.local
  streamPort: 9001
`, fileDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_DATA", envDataDir)
	t.Setenv("XG2G_E2_HOST", "http://env.local")
	t.Setenv("XG2G_E2_STREAM_PORT", "7001")

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

func TestEnvRecordingPlaybackOverrides(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_RECORDING_PLAYBACK_POLICY", "local_only")
	t.Setenv("XG2G_RECORDING_STABLE_WINDOW", "250ms")
	t.Setenv("XG2G_RECORDINGS_MAP", "/media/nfs-recordings=/Volumes/enigma2-recordings")

	loader := NewLoader("", "test-version")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.RecordingPlaybackPolicy != PlaybackPolicyLocalOnly {
		t.Fatalf("expected RecordingPlaybackPolicy=%q, got %q", PlaybackPolicyLocalOnly, cfg.RecordingPlaybackPolicy)
	}
	if cfg.RecordingStableWindow != 250*time.Millisecond {
		t.Fatalf("expected RecordingStableWindow=250ms, got %v", cfg.RecordingStableWindow)
	}
	if len(cfg.RecordingPathMappings) != 1 {
		t.Fatalf("expected 1 recording path mapping, got %d", len(cfg.RecordingPathMappings))
	}
	if cfg.RecordingPathMappings[0].ReceiverRoot != "/media/nfs-recordings" {
		t.Fatalf("expected receiver root /media/nfs-recordings, got %q", cfg.RecordingPathMappings[0].ReceiverRoot)
	}
	if cfg.RecordingPathMappings[0].LocalRoot != "/Volumes/enigma2-recordings" {
		t.Fatalf("expected local root /Volumes/enigma2-recordings, got %q", cfg.RecordingPathMappings[0].LocalRoot)
	}
}

func TestENVCanonicalStreamPortUsedWhenSet(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_E2_STREAM_PORT", "7101")

	loader := NewLoader("", "test-version")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Enigma2.StreamPort != 7101 {
		t.Errorf("expected canonical stream port: 7101, got %d", cfg.Enigma2.StreamPort)
	}
}

func TestENVOverridesPlaybackOperatorFileConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
enigma2:
  baseUrl: http://file.local
playback:
  operator:
    force_intent: compatible
    max_quality_rung: compatible_audio_aac_256_stereo
    disable_client_fallback: false
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_PLAYBACK_FORCE_INTENT", "repair")
	t.Setenv("XG2G_PLAYBACK_MAX_QUALITY_RUNG", "repair_audio_aac_192_stereo")
	t.Setenv("XG2G_PLAYBACK_DISABLE_CLIENT_FALLBACK", "true")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Playback.Operator.ForceIntent != "repair" {
		t.Fatalf("expected ENV override Playback.Operator.ForceIntent=repair, got %q", cfg.Playback.Operator.ForceIntent)
	}
	if cfg.Playback.Operator.MaxQualityRung != "repair_audio_aac_192_stereo" {
		t.Fatalf("expected ENV override Playback.Operator.MaxQualityRung=repair_audio_aac_192_stereo, got %q", cfg.Playback.Operator.MaxQualityRung)
	}
	if !cfg.Playback.Operator.DisableClientFallback {
		t.Fatal("expected ENV override Playback.Operator.DisableClientFallback=true")
	}
	if len(cfg.Playback.Operator.SourceRules) != 0 {
		t.Fatalf("expected source rules to remain unset, got %d entries", len(cfg.Playback.Operator.SourceRules))
	}
}

func TestLoadPlaybackOperatorSourceRulesFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
enigma2:
  baseUrl: http://file.local
playback:
  operator:
    force_intent: compatible
    source_rules:
      - name: live-monk
        mode: live
        service_ref: "1:0:1:ABC"
        force_intent: repair
      - name: rec-prefix
        mode: recording
        service_ref_prefix: "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/"
        max_quality_rung: compatible_audio_aac_256_stereo
        disable_client_fallback: true
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if len(cfg.Playback.Operator.SourceRules) != 2 {
		t.Fatalf("expected 2 source rules, got %d", len(cfg.Playback.Operator.SourceRules))
	}
	if cfg.Playback.Operator.SourceRules[0].Name != "live-monk" {
		t.Fatalf("unexpected first rule name %q", cfg.Playback.Operator.SourceRules[0].Name)
	}
	if cfg.Playback.Operator.SourceRules[0].Mode != "live" {
		t.Fatalf("unexpected first rule mode %q", cfg.Playback.Operator.SourceRules[0].Mode)
	}
	if cfg.Playback.Operator.SourceRules[0].ServiceRef != "1:0:1:ABC" {
		t.Fatalf("unexpected first rule service ref %q", cfg.Playback.Operator.SourceRules[0].ServiceRef)
	}
	if cfg.Playback.Operator.SourceRules[0].ForceIntent != "repair" {
		t.Fatalf("unexpected first rule force intent %q", cfg.Playback.Operator.SourceRules[0].ForceIntent)
	}
	if cfg.Playback.Operator.SourceRules[1].ServiceRefPrefix != "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/" {
		t.Fatalf("unexpected second rule prefix %q", cfg.Playback.Operator.SourceRules[1].ServiceRefPrefix)
	}
	if cfg.Playback.Operator.SourceRules[1].MaxQualityRung != "compatible_audio_aac_256_stereo" {
		t.Fatalf("unexpected second rule max rung %q", cfg.Playback.Operator.SourceRules[1].MaxQualityRung)
	}
	if cfg.Playback.Operator.SourceRules[1].DisableClientFallback == nil || !*cfg.Playback.Operator.SourceRules[1].DisableClientFallback {
		t.Fatal("expected second rule disable_client_fallback=true")
	}
}

func TestENVLegacyStreamPortFailsStart(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_STREAM_PORT", "7001")

	loader := NewLoader("", "test-version")
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected legacy env error, got nil")
	}
	if !strings.Contains(err.Error(), "XG2G_STREAM_PORT") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "XG2G_E2_STREAM_PORT=7001") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrecedenceOrder(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
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

func TestValidateEPGBounds(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	baseCfg := func() AppConfig {
		return AppConfig{
			DataDir: tmpDir,
			Enigma2: Enigma2Settings{
				BaseURL:    "http://test.local",
				StreamPort: 8001,
			},
			Limits: LimitsConfig{
				MaxSessions:   10,
				MaxTranscodes: 5,
			},
			Timeouts: TimeoutsConfig{
				TranscodeStart:      10 * time.Second,
				TranscodeNoProgress: 30 * time.Second,
				KillGrace:           5 * time.Second,
			},
			Breaker: BreakerConfig{
				Window:            60 * time.Second,
				MinAttempts:       10,
				FailuresThreshold: 5,
			},
			Streaming: StreamingConfig{
				DeliveryPolicy: "universal",
			},
			Bouquet:            "test",
			EPGEnabled:         true,
			EPGDays:            7,
			EPGMaxConcurrency:  5,
			EPGTimeoutMS:       5000,
			EPGRetries:         2,
			FuzzyMax:           2,
			VODCacheMaxEntries: 256,
		}
	}

	tests := []struct {
		name      string
		cfg       func() AppConfig
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid EPG config",
			cfg: func() AppConfig {
				return baseCfg()
			},
			shouldErr: false,
		},
		{
			name: "EPGTimeoutMS too low",
			cfg: func() AppConfig {
				cfg := baseCfg()
				cfg.EPGTimeoutMS = 50
				return cfg
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGTimeoutMS too high",
			cfg: func() AppConfig {
				cfg := baseCfg()
				cfg.EPGTimeoutMS = 100000
				return cfg
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGRetries too high",
			cfg: func() AppConfig {
				cfg := baseCfg()
				cfg.EPGRetries = 10
				return cfg
			},
			shouldErr: true,
			errMsg:    "EPGRetries",
		},
		{
			name: "FuzzyMax too high",
			cfg: func() AppConfig {
				cfg := baseCfg()
				cfg.FuzzyMax = 50
				return cfg
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

func TestE2MaxBackoffDefaultAndCanonicalEnv(t *testing.T) {
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
			envValue:        "2s",
			expectedBackoff: 2 * time.Second,
			description:     "Custom 2s maxBackoff from ENV",
		},
		{
			name:            "custom_value_5s",
			envValue:        "5s",
			expectedBackoff: 5 * time.Second,
			description:     "Custom 5s maxBackoff from ENV",
		},
		{
			name:            "custom_value_10s",
			envValue:        "10s",
			expectedBackoff: 10 * time.Second,
			description:     "Custom 10s maxBackoff from ENV",
		},
		{
			name:            "custom_value_30s",
			envValue:        "30s",
			expectedBackoff: 30 * time.Second,
			description:     "Maximum 30s maxBackoff from ENV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Unsetenv("XG2G_E2_MAX_BACKOFF")
			t.Setenv("XG2G_E2_HOST", "http://example.com")
			t.Setenv("XG2G_STORE_PATH", t.TempDir())

			if tt.envValue != "" {
				t.Setenv("XG2G_E2_MAX_BACKOFF", tt.envValue)
			}

			loader := NewLoader("", "test-version")
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			if cfg.Enigma2.MaxBackoff != tt.expectedBackoff {
				t.Errorf("%s: expected Enigma2.MaxBackoff=%v, got %v",
					tt.description, tt.expectedBackoff, cfg.Enigma2.MaxBackoff)
			}
		})
	}
}

func TestE2MaxBackoffFromENV(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_E2_MAX_BACKOFF", "9s")

	loader := NewLoader("", "test-version")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Enigma2.MaxBackoff != 9*time.Second {
		t.Errorf("expected Enigma2.MaxBackoff=9s from XG2G_E2_MAX_BACKOFF, got %v", cfg.Enigma2.MaxBackoff)
	}
}

// TestE2MaxBackoffFromFile tests that maxBackoff from canonical YAML config is read correctly.
func TestE2MaxBackoffFromFile(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
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
enigma2:
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

			if cfg.Enigma2.MaxBackoff != tt.expectedBackoff {
				t.Errorf("%s: expected Enigma2.MaxBackoff=%v, got %v",
					tt.description, tt.expectedBackoff, cfg.Enigma2.MaxBackoff)
			}
		})
	}
}

// TestE2MaxBackoffENVOverridesFile tests precedence: ENV > File > Default.
func TestE2MaxBackoffENVOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	// File config: 5s maxBackoff
	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
  baseUrl: http://test.local
  maxBackoff: 5s
  backoff: 500ms
`, tmpDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// ENV config: 10s maxBackoff (should override file)
	t.Setenv("XG2G_E2_MAX_BACKOFF", "10s")

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

// TestE2BackoffConfigConsistency tests that canonical Enigma2 timing config works together.
func TestE2BackoffConfigConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
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

func TestE2MaxBackoffInvalidValues(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	tests := []struct {
		name          string
		envValue      string
		expectDefault bool
		expectErr     bool
		description   string
	}{
		{
			name:          "invalid_string",
			envValue:      "not-a-duration",
			expectDefault: true,
			description:   "Invalid string should use default",
		},
		{
			name:          "negative_value",
			envValue:      "-1s",
			expectDefault: false,
			expectErr:     true,
			description:   "Negative value should fail validation",
		},
		{
			name:          "zero_value",
			envValue:      "0s",
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
			_ = os.Unsetenv("XG2G_E2_MAX_BACKOFF")
			t.Setenv("XG2G_E2_HOST", "http://example.com")
			t.Setenv("XG2G_STORE_PATH", t.TempDir())

			if tt.envValue != "" {
				t.Setenv("XG2G_E2_MAX_BACKOFF", tt.envValue)
			}

			loader := NewLoader("", "test-version")
			cfg, err := loader.Load()
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected Load() to fail")
				}
				if !containsString(err.Error(), "Enigma2.MaxBackoff") {
					t.Fatalf("expected Enigma2.MaxBackoff validation error, got %v", err)
				}
				return
			}
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
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(tmpDir, "config.yaml")
	dataDir := filepath.Join(tmpDir, "data")

	// YAML with EPG explicitly disabled and retries set to 0
	yamlContent := fmt.Sprintf(`
dataDir: %s
enigma2:
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

	// Note: enigma2.retries is not a pointer in Enigma2Config, so it still has the old behavior.
	// This is intentional; this regression test only covers the pointer-based EPG fields.
}

func TestParseScopedTokensFromEnv(t *testing.T) {
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

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
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	loader := NewLoader("", "test")
	_, err := loader.Load()
	if err == nil || !containsString(err.Error(), "APITokenScopes") {
		t.Fatalf("expected APITokenScopes error, got %v", err)
	}
}

func TestYAMLRejectsMissingTokenScopes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
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
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
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
