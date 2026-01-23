// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"reflect"
	"testing"
	"time"
)

func TestWorkerEnvMergePreservesConfigValues(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Enabled = true
	cfg.Engine.Mode = "virtual"
	cfg.Engine.IdleTimeout = 45 * time.Second
	cfg.Engine.TunerSlots = []int{1, 3}

	cfg.Store.Backend = "bolt"
	cfg.Store.Path = "/tmp/xg2g-store"

	cfg.HLS.Root = "/tmp/xg2g-hls"
	cfg.HLS.DVRWindow = 1234 * time.Second

	cfg.Enigma2.BaseURL = "http://file-e2.local"
	cfg.Enigma2.Timeout = 12 * time.Second
	cfg.Enigma2.ResponseHeaderTimeout = 7 * time.Second
	cfg.Enigma2.Retries = 5
	cfg.Enigma2.RateLimit = 9
	cfg.Enigma2.RateBurst = 11
	cfg.Enigma2.UserAgent = "xg2g-test"

	unsetEnv(t, "XG2G_ENGINE_ENABLED")
	unsetEnv(t, "XG2G_ENGINE_MODE")
	unsetEnv(t, "XG2G_STORE_BACKEND")
	unsetEnv(t, "XG2G_STORE_PATH")
	unsetEnv(t, "XG2G_HLS_ROOT")
	unsetEnv(t, "XG2G_DVR_WINDOW")
	unsetEnv(t, "XG2G_ENGINE_IDLE_TIMEOUT")
	unsetEnv(t, "XG2G_E2_HOST")
	unsetEnv(t, "XG2G_E2_TIMEOUT")
	unsetEnv(t, "XG2G_E2_RESPONSE_HEADER_TIMEOUT")
	unsetEnv(t, "XG2G_E2_RETRIES")
	unsetEnv(t, "XG2G_E2_RATE_LIMIT")
	unsetEnv(t, "XG2G_E2_RATE_BURST")
	unsetEnv(t, "XG2G_E2_USER_AGENT")
	unsetEnv(t, "XG2G_TUNER_SLOTS")

	loader.mergeEnvConfig(&cfg)

	if cfg.Engine.Mode != "virtual" {
		t.Errorf("expected WorkerMode to remain %q, got %q", "virtual", cfg.Engine.Mode)
	}
	if cfg.Store.Backend != "bolt" {
		t.Errorf("expected StoreBackend to remain %q, got %q", "bolt", cfg.Store.Backend)
	}
	if cfg.Store.Path != "/tmp/xg2g-store" {
		t.Errorf("expected StorePath to remain %q, got %q", "/tmp/xg2g-store", cfg.Store.Path)
	}
	if cfg.HLS.Root != "/tmp/xg2g-hls" {
		t.Errorf("expected HLSRoot to remain %q, got %q", "/tmp/xg2g-hls", cfg.HLS.Root)
	}
	if cfg.HLS.DVRWindow != 1234*time.Second {
		t.Errorf("expected DVRWindow to remain %v, got %v", 1234*time.Second, cfg.HLS.DVRWindow)
	}
	if cfg.Engine.IdleTimeout != 45*time.Second {
		t.Errorf("expected IdleTimeout to remain %v, got %v", 45*time.Second, cfg.Engine.IdleTimeout)
	}
	if cfg.Enigma2.BaseURL != "http://file-e2.local" {
		t.Errorf("expected E2Host to remain %q, got %q", "http://file-e2.local", cfg.Enigma2.BaseURL)
	}
	if cfg.Enigma2.Timeout != 12*time.Second {
		t.Errorf("expected E2Timeout to remain %v, got %v", 12*time.Second, cfg.Enigma2.Timeout)
	}
	if cfg.Enigma2.ResponseHeaderTimeout != 7*time.Second {
		t.Errorf("expected E2RespTimeout to remain %v, got %v", 7*time.Second, cfg.Enigma2.ResponseHeaderTimeout)
	}
	if cfg.Enigma2.Retries != 5 {
		t.Errorf("expected E2Retries to remain %d, got %d", 5, cfg.Enigma2.Retries)
	}
	if cfg.Enigma2.RateLimit != 9 {
		t.Errorf("expected E2RateLimit to remain %d, got %d", 9, cfg.Enigma2.RateLimit)
	}
	if cfg.Enigma2.RateBurst != 11 {
		t.Errorf("expected E2RateBurst to remain %d, got %d", 11, cfg.Enigma2.RateBurst)
	}
	if cfg.Enigma2.UserAgent != "xg2g-test" {
		t.Errorf("expected E2UserAgent to remain %q, got %q", "xg2g-test", cfg.Enigma2.UserAgent)
	}
	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{1, 3}) {
		t.Errorf("expected TunerSlots to remain [1 3], got %v", cfg.Engine.TunerSlots)
	}
}

func TestInvalidTunerSlotsEnvPreservesConfig(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Mode = "standard"
	cfg.Engine.TunerSlots = []int{2, 4}

	t.Setenv("XG2G_TUNER_SLOTS", "bad-slots")

	loader.mergeEnvConfig(&cfg)

	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{2, 4}) {
		t.Errorf("expected TunerSlots to remain [2 4], got %v", cfg.Engine.TunerSlots)
	}
}

func TestEmptyTunerSlotsEnvPreservesConfig(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	loader.setDefaults(&cfg)

	cfg.Engine.Mode = "standard"
	cfg.Engine.TunerSlots = []int{5}

	t.Setenv("XG2G_TUNER_SLOTS", "")

	loader.mergeEnvConfig(&cfg)

	if !reflect.DeepEqual(cfg.Engine.TunerSlots, []int{5}) {
		t.Errorf("expected TunerSlots to remain [5], got %v", cfg.Engine.TunerSlots)
	}
}
