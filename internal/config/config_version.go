// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "strings"

const (
	DefaultConfigVersion = "3.0.0"
	V3ConfigVersion      = "3.0.0"
)

// EffectiveConfigVersion returns a stable config version for serialization.
func EffectiveConfigVersion(cfg AppConfig) string {
	if strings.TrimSpace(cfg.ConfigVersion) != "" {
		return cfg.ConfigVersion
	}
	return DefaultConfigVersion
}
