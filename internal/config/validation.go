// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package config provides configuration management for xg2g.
package config

import (
	"net"
	"strings"

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

	// Rate limit whitelist entries must be valid IPs or CIDRs
	for _, entry := range cfg.RateLimitWhitelist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if net.ParseIP(entry) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(entry); err == nil {
			continue
		}
		v.AddError("RateLimitWhitelist", "must be a valid IP or CIDR", entry)
	}

	// Validate V3 Worker paths if enabled (Fail Fast)
	if cfg.WorkerEnabled {
		v.WritableDirectory("StorePath", cfg.StorePath, false)
		v.WritableDirectory("HLSRoot", cfg.HLSRoot, false)

		if cfg.ConfigStrict {
			if strings.TrimSpace(cfg.ConfigVersion) == "" {
				v.AddError("ConfigVersion", "required when XG2G_V3_CONFIG_STRICT=true", cfg.ConfigVersion)
			} else if cfg.ConfigVersion != V3ConfigVersion {
				v.AddError("ConfigVersion", "unsupported v3 config version", cfg.ConfigVersion)
			}

			switch cfg.WorkerMode {
			case "standard", "virtual":
			default:
				v.AddError("WorkerMode", "must be standard or virtual", cfg.WorkerMode)
			}

			switch cfg.StoreBackend {
			case "memory", "bolt":
			default:
				v.AddError("StoreBackend", "must be memory or bolt", cfg.StoreBackend)
			}

			v.URL("E2Host", cfg.E2Host, []string{"http", "https"})
		}
	}

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}
