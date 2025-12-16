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
	fileCfg := FileConfig{
		Version:  cfg.Version,
		DataDir:  cfg.DataDir,
		LogLevel: cfg.LogLevel,
		OpenWebIF: OpenWebIFConfig{
			BaseURL:    cfg.OWIBase,
			Username:   cfg.OWIUsername,
			Password:   cfg.OWIPassword,
			StreamPort: cfg.StreamPort,
			UseWebIF:   boolPtr(cfg.UseWebIFStreams),
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
