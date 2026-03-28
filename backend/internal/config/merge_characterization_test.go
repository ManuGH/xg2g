// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
	"time"
)

func TestMergeFileConfig_RejectsLegacyOpenWebIF(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	useWebIFOpen := true
	src := &FileConfig{
		OpenWebIF: OpenWebIFConfig{
			BaseURL:  "http://legacy.local",
			UseWebIF: &useWebIFOpen,
		},
	}

	err := loader.mergeFileConfig(&cfg, src)
	if err == nil {
		t.Fatal("expected legacy openWebIF config to be rejected")
	}
	if got := err.Error(); got == "" || !containsString(got, "openWebIF.baseUrl -> enigma2.baseUrl") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeFileConfig_AppliesCanonicalEnigma2(t *testing.T) {
	loader := NewLoader("", "test")
	cfg := AppConfig{}
	if err := loader.setDefaults(&cfg); err != nil {
		t.Fatalf("setDefaults() failed: %v", err)
	}

	useWebIF := false
	port := 9200
	src := &FileConfig{
		Enigma2: Enigma2Config{
			BaseURL:    "http://canonical.local",
			UseWebIF:   &useWebIF,
			StreamPort: &port,
		},
	}
	if err := loader.mergeFileConfig(&cfg, src); err != nil {
		t.Fatalf("mergeFileConfig() failed: %v", err)
	}
	if cfg.Enigma2.BaseURL != "http://canonical.local" {
		t.Fatalf("expected canonical base URL, got %q", cfg.Enigma2.BaseURL)
	}
	if cfg.Enigma2.StreamPort != 9200 {
		t.Fatalf("expected canonical stream port, got %d", cfg.Enigma2.StreamPort)
	}
	if cfg.Enigma2.UseWebIFStreams {
		t.Fatalf("expected canonical useWebIFStreams=false, got true")
	}
}

func TestMergeEnvConfig_AppliesCanonicalEnigma2Env(t *testing.T) {
	unsetEnv(t, "XG2G_E2_HOST")
	unsetEnv(t, "XG2G_E2_USER")
	unsetEnv(t, "XG2G_E2_PASS")
	unsetEnv(t, "XG2G_E2_TIMEOUT")
	unsetEnv(t, "XG2G_E2_RETRIES")
	unsetEnv(t, "XG2G_E2_BACKOFF")
	unsetEnv(t, "XG2G_E2_STREAM_PORT")

	t.Setenv("XG2G_E2_HOST", "http://canonical.local")
	t.Setenv("XG2G_E2_USER", "canonical-user")
	t.Setenv("XG2G_E2_PASS", "canonical-pass")
	t.Setenv("XG2G_E2_TIMEOUT", "3s")
	t.Setenv("XG2G_E2_RETRIES", "9")
	t.Setenv("XG2G_E2_BACKOFF", "700ms")
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

func TestMergeEnvConfig_AppliesCanonicalMaxBackoffEnv(t *testing.T) {
	unsetEnv(t, "XG2G_E2_MAX_BACKOFF")

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
