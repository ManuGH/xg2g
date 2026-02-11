// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
	"time"
)

func TestMergeFileConfig_UseWebIFPrefersEnigma2Alias(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	useWebIFOpen := true
	useWebIFE2 := false
	src := &FileConfig{
		OpenWebIF: OpenWebIFConfig{
			UseWebIF: &useWebIFOpen,
		},
		Enigma2: Enigma2Config{
			UseWebIF: &useWebIFE2,
		},
	}

	if err := loader.mergeFileConfig(&cfg, src); err != nil {
		t.Fatalf("mergeFileConfig() failed: %v", err)
	}

	if cfg.Enigma2.UseWebIFStreams {
		t.Fatalf("expected enigma2.useWebIFStreams to override openWebIF.useWebIFStreams")
	}
}

func TestMergeFileConfig_StreamPortPrefersEnigma2Alias(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	t.Run("openWebIF sets stream port", func(t *testing.T) {
		src := &FileConfig{
			OpenWebIF: OpenWebIFConfig{
				StreamPort: 9100,
			},
		}
		if err := loader.mergeFileConfig(&cfg, src); err != nil {
			t.Fatalf("mergeFileConfig() failed: %v", err)
		}
		if cfg.Enigma2.StreamPort != 9100 {
			t.Fatalf("expected stream port from openWebIF, got %d", cfg.Enigma2.StreamPort)
		}
	})

	t.Run("enigma2 streamPort overrides openWebIF fallback", func(t *testing.T) {
		port := 9200
		src := &FileConfig{
			Enigma2: Enigma2Config{
				StreamPort: &port,
			},
		}
		if err := loader.mergeFileConfig(&cfg, src); err != nil {
			t.Fatalf("mergeFileConfig() failed: %v", err)
		}
		if cfg.Enigma2.StreamPort != 9200 {
			t.Fatalf("expected enigma2 stream port to override openWebIF fallback, got %d", cfg.Enigma2.StreamPort)
		}
	})
}

func TestMergeEnvConfig_CanonicalEnigma2OverridesLegacyOWI(t *testing.T) {
	unsetEnv(t, "XG2G_OWI_BASE")
	unsetEnv(t, "XG2G_E2_HOST")
	unsetEnv(t, "XG2G_OWI_USER")
	unsetEnv(t, "XG2G_E2_USER")
	unsetEnv(t, "XG2G_OWI_PASS")
	unsetEnv(t, "XG2G_E2_PASS")
	unsetEnv(t, "XG2G_OWI_TIMEOUT_MS")
	unsetEnv(t, "XG2G_E2_TIMEOUT")
	unsetEnv(t, "XG2G_OWI_RETRIES")
	unsetEnv(t, "XG2G_E2_RETRIES")
	unsetEnv(t, "XG2G_OWI_BACKOFF_MS")
	unsetEnv(t, "XG2G_E2_BACKOFF")
	unsetEnv(t, "XG2G_STREAM_PORT")
	unsetEnv(t, "XG2G_E2_STREAM_PORT")

	t.Setenv("XG2G_OWI_BASE", "http://legacy.local")
	t.Setenv("XG2G_E2_HOST", "http://canonical.local")
	t.Setenv("XG2G_OWI_USER", "legacy-user")
	t.Setenv("XG2G_E2_USER", "canonical-user")
	t.Setenv("XG2G_OWI_PASS", "legacy-pass")
	t.Setenv("XG2G_E2_PASS", "canonical-pass")
	t.Setenv("XG2G_OWI_TIMEOUT_MS", "12000")
	t.Setenv("XG2G_E2_TIMEOUT", "3s")
	t.Setenv("XG2G_OWI_RETRIES", "5")
	t.Setenv("XG2G_E2_RETRIES", "9")
	t.Setenv("XG2G_OWI_BACKOFF_MS", "250")
	t.Setenv("XG2G_E2_BACKOFF", "700ms")
	t.Setenv("XG2G_STREAM_PORT", "7001")
	t.Setenv("XG2G_E2_STREAM_PORT", "7101")

	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	loader.mergeEnvConfig(&cfg)

	if cfg.Enigma2.BaseURL != "http://canonical.local" {
		t.Fatalf("expected canonical base URL, got %q", cfg.Enigma2.BaseURL)
	}
	if cfg.Enigma2.Username != "canonical-user" {
		t.Fatalf("expected canonical username, got %q", cfg.Enigma2.Username)
	}
	if cfg.Enigma2.Password != "canonical-pass" {
		t.Fatalf("expected canonical password, got %q", cfg.Enigma2.Password)
	}
	if cfg.Enigma2.Timeout != 3*time.Second {
		t.Fatalf("expected canonical timeout 3s, got %v", cfg.Enigma2.Timeout)
	}
	if cfg.Enigma2.Retries != 9 {
		t.Fatalf("expected canonical retries 9, got %d", cfg.Enigma2.Retries)
	}
	if cfg.Enigma2.Backoff != 700*time.Millisecond {
		t.Fatalf("expected canonical backoff 700ms, got %v", cfg.Enigma2.Backoff)
	}
	if cfg.Enigma2.StreamPort != 7101 {
		t.Fatalf("expected canonical streamPort 7101, got %d", cfg.Enigma2.StreamPort)
	}
}

func TestMergeEnvConfig_MaxBackoffPrefersCanonicalEnv(t *testing.T) {
	unsetEnv(t, "XG2G_OWI_MAX_BACKOFF_MS")
	unsetEnv(t, "XG2G_E2_MAX_BACKOFF")

	t.Setenv("XG2G_OWI_MAX_BACKOFF_MS", "4000")
	t.Setenv("XG2G_E2_MAX_BACKOFF", "9s")

	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	loader.mergeEnvConfig(&cfg)

	if cfg.Enigma2.MaxBackoff != 9*time.Second {
		t.Fatalf("expected maxBackoff from canonical enigma2 env key, got %v", cfg.Enigma2.MaxBackoff)
	}
}
