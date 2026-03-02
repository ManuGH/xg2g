package read

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
)

// ConfigInfo captures a sanitized and structured view of the application configuration.
type ConfigInfo struct {
	Version           string
	DataDir           string
	LogLevel          string
	Enigma2BaseURL    string
	Enigma2StreamPort int
	Enigma2Username   string
	Bouquets          []string
	EPGEnabled        bool
	EPGDays           int
	EPGSource         string
	PiconBase         string
	DeliveryPolicy    string
}

// GetConfigInfo assembles configuration information from the provided AppConfig.
func GetConfigInfo(cfg config.AppConfig) ConfigInfo {
	bouquets := make([]string, 0)
	for _, name := range strings.Split(cfg.Bouquet, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			bouquets = append(bouquets, trimmed)
		}
	}

	return ConfigInfo{
		Version:           cfg.Version,
		DataDir:           cfg.DataDir,
		LogLevel:          cfg.LogLevel,
		Enigma2BaseURL:    cfg.Enigma2.BaseURL,
		Enigma2StreamPort: cfg.Enigma2.StreamPort,
		Enigma2Username:   cfg.Enigma2.Username,
		Bouquets:          bouquets,
		EPGEnabled:        cfg.EPGEnabled,
		EPGDays:           cfg.EPGDays,
		EPGSource:         cfg.EPGSource,
		PiconBase:         cfg.PiconBase,
		DeliveryPolicy:    cfg.Streaming.DeliveryPolicy,
	}
}
