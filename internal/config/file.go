// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

// LoadFileConfig loads a YAML config file without applying defaults or env overrides.
func LoadFileConfig(path string) (*FileConfig, error) {
	loader := NewLoader(path, "")
	return loader.loadFile(path)
}
