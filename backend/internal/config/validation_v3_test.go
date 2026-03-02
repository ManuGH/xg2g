// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func baseV3Config(t *testing.T) AppConfig {
	t.Helper()
	tmp := t.TempDir()
	return AppConfig{
		Enigma2: Enigma2Settings{
			BaseURL:    "http://example.com",
			StreamPort: 8001,
		},
		DataDir: tmp,
		Bouquet: "Premium",
		Engine: EngineConfig{
			Enabled: false,
			Mode:    "standard",
		},
		Store: StoreConfig{
			Backend: "memory",
			Path:    filepath.Join(tmp, "v3-store"),
		},
		HLS: HLSConfig{
			Root: filepath.Join(tmp, "v3-hls"),
		},
		Limits: LimitsConfig{
			MaxSessions:   10,
			MaxTranscodes: 5,
		},
		Timeouts: TimeoutsConfig{
			TranscodeStart:      10 * time.Second,
			TranscodeNoProgress: 30 * time.Second,
			KillGrace:           5 * time.Second,
		},
		Breaker: BreakerConfig{
			Window:            60 * time.Second,
			MinAttempts:       10,
			FailuresThreshold: 5,
		},
		Streaming: StreamingConfig{
			DeliveryPolicy: "universal",
		},
		VODCacheMaxEntries: 256,
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

func TestValidate_AllowsEmptyEnigma2BaseURL(t *testing.T) {
	cfg := baseV3Config(t)
	cfg.Enigma2.BaseURL = ""

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
