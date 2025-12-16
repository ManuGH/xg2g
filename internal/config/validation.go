// SPDX-License-Identifier: MIT

// Package config provides configuration management for xg2g.
package config

import (
	"github.com/ManuGH/xg2g/internal/validate"
)

// Validate validates a AppConfig using the centralized validation package
func Validate(cfg AppConfig) error {
	v := validate.New()

	// OpenWebIF URL
	v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})

	// Stream port
	v.Port("StreamPort", cfg.StreamPort)

	// Data directory
	v.Directory("DataDir", cfg.DataDir, false)

	// Bouquet (at least not empty)
	v.NotEmpty("Bouquet", cfg.Bouquet)

	// EPG settings (if enabled)
	if cfg.EPGEnabled {
		v.Range("EPGDays", cfg.EPGDays, 1, 14)
		v.Range("EPGMaxConcurrency", cfg.EPGMaxConcurrency, 1, 10)
		// EPGTimeoutMS must be between 100ms and 60s for safety
		v.Range("EPGTimeoutMS", cfg.EPGTimeoutMS, 100, 60000)
		// EPGRetries should be reasonable (0-5)
		v.Range("EPGRetries", cfg.EPGRetries, 0, 5)
		// FuzzyMax for fuzzy matching (0-10 is reasonable)
		v.Range("FuzzyMax", cfg.FuzzyMax, 0, 10)
	}

	// OWI retries
	v.Range("OWIRetries", cfg.OWIRetries, 0, 10)

	// Validate file paths for security
	v.Path("XMLTVPath", cfg.XMLTVPath)

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}
