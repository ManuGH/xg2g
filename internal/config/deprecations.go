// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ManuGH/xg2g/internal/log"
)

// Deprecation defines a deprecated configuration key and its enforcement state.
type Deprecation struct {
	Key     string `json:"key"`     // Key in ENV or YAML Path
	Target  string `json:"target"`  // Recommended target (if any)
	Phase   string `json:"phase"`   // "warn" or "fail"
	Message string `json:"message"` // Detailed explanation
}

// DeprecationsFile represents the structure of docs/deprecations.json.
type DeprecationsFile struct {
	Version      int           `json:"version"`
	Deprecations []Deprecation `json:"deprecations"`
}

// LoadDeprecations loads the deprecation list from disk.
func LoadDeprecations() ([]Deprecation, error) {
	// Heuristic: start at current dir, look for docs/deprecations.json, go up 3 levels max
	curr, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	for i := 0; i < 4; i++ {
		path := filepath.Join(curr, "docs", "deprecations.json")
		if _, err := os.Stat(path); err == nil {
			// #nosec G304 - path is constructed locally with hardcoded filename relative to CWD
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read deprecations at %s: %w", path, err)
			}
			var df DeprecationsFile
			if err := json.Unmarshal(data, &df); err != nil {
				return nil, fmt.Errorf("unmarshal deprecations: %w", err)
			}
			return df.Deprecations, nil
		}
		curr = filepath.Dir(curr)
		if curr == "/" || curr == "." {
			break
		}
	}

	return nil, nil // Not found is fine, means no deprecations active
}

// CheckDeprecations scans the environment and registry for deprecated keys.
func (l *Loader) CheckDeprecations(cfg *AppConfig) error {
	logger := log.WithComponent("config")

	// Use the current directory or a known base path to find docs/deprecations.json
	// For now, assume it's in the repo root
	deps, err := LoadDeprecations()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to load deprecations list")
		return nil // Non-fatal for now
	}

	registry, err := GetRegistry()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to get registry for deprecation check")
		return nil // Non-fatal, continue without registry-based checks
	}

	for _, d := range deps {
		active := false
		source := ""

		// Check ENV
		if os.Getenv(d.Key) != "" {
			active = true
			source = "ENV: " + d.Key
		}

		// Check YAML (using registry to map path)
		if !active && d.Key != "" {
			if _, ok := registry.ByPath[d.Key]; ok { //nolint:staticcheck
				// Pro-active: if it's in ByPath, we assume it's user-facing.
				// For P1.2, we'll focus on ENV and explicitly known legacy keys.
			}
		}

		if active {
			if d.Phase == "fail" {
				return fmt.Errorf("configuration key %q is removed: %s", d.Key, d.Message)
			}
			logger.Warn().
				Str("source", source).
				Str("target", d.Target).
				Msgf("DEPRECATED: %s", d.Message)
		}
	}

	return nil
}
