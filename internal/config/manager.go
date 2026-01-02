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
	// We only write fields that are user-configurable via the UI
	//
	// IMPORTANT: For Enigma2.BaseURL, only write it if it differs from OWIBase.
	// This preserves the automatic fallback behavior (enigma2 inherits from openWebIF).
	e2BaseURL := ""
	if cfg.E2Host != "" && cfg.E2Host != cfg.OWIBase {
		e2BaseURL = cfg.E2Host
	}

	fileCfg := FileConfig{
		Version:       EffectiveConfigVersion(*cfg),
		ConfigVersion: V3ConfigVersion,
		DataDir:       cfg.DataDir,
		LogLevel:      cfg.LogLevel,
		OpenWebIF: OpenWebIFConfig{
			BaseURL:    cfg.OWIBase,
			Username:   cfg.OWIUsername,
			Password:   cfg.OWIPassword,
			StreamPort: cfg.StreamPort,
			UseWebIF:   boolPtr(cfg.UseWebIFStreams),
		},
		Enigma2: Enigma2Config{
			BaseURL:               e2BaseURL,
			Username:              cfg.E2Username,
			Password:              cfg.E2Password,
			Timeout:               cfg.E2Timeout.String(),
			ResponseHeaderTimeout: cfg.E2RespTimeout.String(),
			Retries:               cfg.E2Retries,
			Backoff:               cfg.E2Backoff.String(),
			MaxBackoff:            cfg.E2MaxBackoff.String(),
			RateLimit:             cfg.E2RateLimit,
			RateBurst:             cfg.E2RateBurst,
			UserAgent:             cfg.E2UserAgent,
		},
		Bouquets: splitCSV(cfg.Bouquet),
		EPG: EPGConfig{
			Enabled:        boolPtr(cfg.EPGEnabled),
			Days:           intPtr(cfg.EPGDays),
			Source:         cfg.EPGSource,
			MaxConcurrency: intPtr(cfg.EPGMaxConcurrency),
			TimeoutMS:      intPtr(cfg.EPGTimeoutMS),
		},
		Picons: PiconsConfig{
			BaseURL: cfg.PiconBase,
		},
	}

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

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }
