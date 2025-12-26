// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strings"
)

// EffectiveFileConfigVersion resolves config version from a FileConfig.
func EffectiveFileConfigVersion(cfg FileConfig) string {
	_ = cfg
	return V3ConfigVersion
}

// MigrateFileConfig applies known migrations to reach the target version.
// It returns the updated config and a list of change descriptions.
func MigrateFileConfig(cfg FileConfig, targetVersion string) (FileConfig, []string, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		return cfg, nil, fmt.Errorf("target version is required")
	}
	if targetVersion != V3ConfigVersion {
		return cfg, nil, fmt.Errorf("config version is fixed to %s", V3ConfigVersion)
	}

	var changes []string
	if strings.TrimSpace(cfg.ConfigVersion) != V3ConfigVersion {
		cfg.ConfigVersion = V3ConfigVersion
		changes = append(changes, fmt.Sprintf("set configVersion to %s", V3ConfigVersion))
	}
	if strings.TrimSpace(cfg.Version) != "" && strings.TrimSpace(cfg.Version) != V3ConfigVersion {
		cfg.Version = V3ConfigVersion
		changes = append(changes, fmt.Sprintf("set version to %s", V3ConfigVersion))
	}

	return cfg, changes, nil
}
