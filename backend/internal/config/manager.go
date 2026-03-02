// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manager handles configuration persistence.
type Manager struct {
	configPath string
}

// NewManager creates a new configuration manager.
func NewManager(configPath string) *Manager {
	return &Manager{
		configPath: configPath,
	}
}

// Save writes the configuration to disk.
func (m *Manager) Save(cfg *AppConfig) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(m.configPath), 0750); err != nil {
		return fmt.Errorf("mkdir config dir: %w", err)
	}

	// Map AppConfig back to FileConfig for serialization
	fileCfg := ToFileConfig(cfg)

	// Encode to YAML (atomic write: temp file + rename)
	dir := filepath.Dir(m.configPath)
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	enc := yaml.NewEncoder(tmp)
	enc.SetIndent(2)
	if err := enc.Encode(fileCfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close encoder: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config file: %w", err)
	}

	if err := os.Rename(tmp.Name(), m.configPath); err != nil {
		return fmt.Errorf("rename config file: %w", err)
	}

	return nil
}

// Helper functions for mapping

func ToFileConfig(cfg *AppConfig) FileConfig {
	return FileConfig{
		Version:       EffectiveConfigVersion(*cfg),
		ConfigVersion: V3ConfigVersion,
		DataDir:       cfg.DataDir,
		LogLevel:      cfg.LogLevel,
		OpenWebIF: OpenWebIFConfig{
			BaseURL:    cfg.Enigma2.BaseURL,
			Username:   cfg.Enigma2.Username,
			Password:   cfg.Enigma2.Password,
			StreamPort: cfg.Enigma2.StreamPort,
			UseWebIF:   boolPtr(cfg.Enigma2.UseWebIFStreams),
			Timeout:    cfg.Enigma2.Timeout.String(),
			Retries:    cfg.Enigma2.Retries,
			Backoff:    cfg.Enigma2.Backoff.String(),
			MaxBackoff: cfg.Enigma2.MaxBackoff.String(),
		},
		Enigma2: Enigma2Config{
			BaseURL:               cfg.Enigma2.BaseURL, // Now always the same as OpenWebIF
			Username:              cfg.Enigma2.Username,
			Password:              cfg.Enigma2.Password,
			Timeout:               cfg.Enigma2.Timeout.String(),
			ResponseHeaderTimeout: cfg.Enigma2.ResponseHeaderTimeout.String(),
			Retries:               cfg.Enigma2.Retries,
			Backoff:               cfg.Enigma2.Backoff.String(),
			MaxBackoff:            cfg.Enigma2.MaxBackoff.String(),
			RateLimit:             cfg.Enigma2.RateLimit,
			RateBurst:             cfg.Enigma2.RateBurst,
			UserAgent:             cfg.Enigma2.UserAgent,
			FallbackTo8001:        boolPtr(cfg.Enigma2.FallbackTo8001),
		},
		Bouquets: splitCSV(cfg.Bouquet),
		EPG: EPGConfig{
			Enabled:        boolPtr(cfg.EPGEnabled),
			Days:           intPtr(cfg.EPGDays),
			Source:         cfg.EPGSource,
			MaxConcurrency: intPtr(cfg.EPGMaxConcurrency),
			TimeoutMS:      intPtr(cfg.EPGTimeoutMS),
			XMLTVPath:      cfg.XMLTVPath,
		},
		Picons: PiconsConfig{
			BaseURL: cfg.PiconBase,
		},
		Limits: &LimitsConfig{
			MaxSessions:   cfg.Limits.MaxSessions,
			MaxTranscodes: cfg.Limits.MaxTranscodes,
		},
		Timeouts: &TimeoutsConfig{
			TranscodeStart:      cfg.Timeouts.TranscodeStart,
			TranscodeNoProgress: cfg.Timeouts.TranscodeNoProgress,
			KillGrace:           cfg.Timeouts.KillGrace,
		},
		Breaker: &BreakerConfig{
			Window:               cfg.Breaker.Window,
			MinAttempts:          cfg.Breaker.MinAttempts,
			FailuresThreshold:    cfg.Breaker.FailuresThreshold,
			ConsecutiveThreshold: cfg.Breaker.ConsecutiveThreshold,
		},
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }
