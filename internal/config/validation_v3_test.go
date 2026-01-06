// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func baseV3Config(t *testing.T) AppConfig {
	t.Helper()
	tmp := t.TempDir()
	return AppConfig{
		OWIBase:    "http://example.com",
		DataDir:    tmp,
		Bouquet:    "Premium",
		StreamPort: 8001,
		Engine: EngineConfig{
			Enabled: true,
			Mode:    "standard",
		},
		Store: StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(tmp, "v3-store"),
		},
		HLS: HLSConfig{
			Root: filepath.Join(tmp, "v3-hls"),
		},
		Enigma2: Enigma2Settings{
			BaseURL: "http://example.com",
		},
	}
}

func TestValidate_V3StrictAllowsEmptyConfigVersion(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.ConfigStrict = true
	cfg.ConfigVersion = ""

	err := Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_AllowsEmptyBouquet(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.Bouquet = ""

	err := Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_AllowsEmptyOWIBase(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.OWIBase = ""

	err := Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_V3StrictRejectsInvalidWorkerMode(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.ConfigStrict = true
	cfg.Engine.Mode = "weird"

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Engine.Mode")
}
