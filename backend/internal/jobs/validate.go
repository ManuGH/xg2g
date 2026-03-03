// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/validate"
)

// validateConfig validates the configuration for refresh operations
func validateConfig(cfg config.AppConfig) error {
	// Use centralized validation package
	v := validate.New()

	v.URL("Enigma2.BaseURL", cfg.Enigma2.BaseURL, []string{"http", "https"})
	v.Port("Enigma2.StreamPort", cfg.Enigma2.StreamPort)
	v.Directory("DataDir", cfg.DataDir, false)

	if cfg.PiconBase != "" {
		v.URL("PiconBase", cfg.PiconBase, []string{"http", "https"})
	}

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}

// clampConcurrency ensures concurrency is within sane bounds [1, maxVal]
func clampConcurrency(value, defaultValue, maxVal int) int {
	if value < 1 {
		if defaultValue < 1 {
			return 1
		}
		return defaultValue
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
