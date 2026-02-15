// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"time"
)

// setDefaults sets default values for configuration.
func (l *Loader) setDefaults(cfg *AppConfig) error {
	// P1.4 Mechanical Truth: Apply defaults from Registry
	registry, err := GetRegistry()
	if err != nil {
		return fmt.Errorf("get registry: %w", err)
	}
	if err := registry.ApplyDefaults(cfg); err != nil {
		return fmt.Errorf("apply defaults: %w", err)
	}

	// Fields not yet in Registry (internal state)
	cfg.ConfigVersion = V3ConfigVersion

	// Default Verification Policies (Homelab Friendly)
	cfg.Verification.Enabled = true
	cfg.Verification.Interval = 60 * time.Second

	return nil
}
