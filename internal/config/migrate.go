// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"errors"
	"fmt"
	"strings"
)

var ErrMigrationNotImplemented = errors.New("migration not implemented")

// EffectiveFileConfigVersion resolves config version from a FileConfig.
func EffectiveFileConfigVersion(cfg FileConfig) string {
	if strings.TrimSpace(cfg.ConfigVersion) != "" {
		return cfg.ConfigVersion
	}
	if strings.TrimSpace(cfg.Version) != "" {
		return cfg.Version
	}
	return ""
}

// MigrateFileConfig applies known migrations to reach the target version.
// It returns the updated config and a list of change descriptions.
func MigrateFileConfig(cfg FileConfig, targetVersion string) (FileConfig, []string, error) {
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		return cfg, nil, fmt.Errorf("target version is required")
	}

	current := EffectiveFileConfigVersion(cfg)
	if current == targetVersion {
		return cfg, nil, nil
	}

	if current == "" {
		cfg.ConfigVersion = targetVersion
		return cfg, []string{fmt.Sprintf("set configVersion to %s", targetVersion)}, nil
	}

	return cfg, nil, fmt.Errorf("%w: %s -> %s", ErrMigrationNotImplemented, current, targetVersion)
}
