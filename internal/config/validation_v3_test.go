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
		OWIBase:       "http://example.com",
		DataDir:       tmp,
		Bouquet:       "Premium",
		StreamPort:    8001,
		WorkerEnabled: true,
		WorkerMode:    "standard",
		StoreBackend:  "memory",
		StorePath:     filepath.Join(tmp, "v3-store"),
		HLSRoot:       filepath.Join(tmp, "v3-hls"),
		E2Host:        "http://example.com",
	}
}

func TestValidate_V3StrictAllowsEmptyConfigVersion(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.ConfigStrict = true
	cfg.ConfigVersion = ""

	err := Validate(cfg)
	require.NoError(t, err)
}

func TestValidate_V3StrictRejectsInvalidWorkerMode(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.ConfigStrict = true
	cfg.WorkerMode = "weird"

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WorkerMode")
}
