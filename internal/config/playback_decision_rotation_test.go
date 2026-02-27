package config

import (
	"strings"
	"testing"
	"time"
)

func TestPlaybackDecisionRotationEnvOverrides(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_OWI_BASE", "http://example.com")
	t.Setenv("XG2G_PLAYBACK_DECISION_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_PLAYBACK_DECISION_KID", "kid-env")
	t.Setenv("XG2G_PLAYBACK_DECISION_PREVIOUS_KEYS", "kid-old:abcdefghijklmnopqrstuvwxyz0123456789ABCDE2")
	t.Setenv("XG2G_PLAYBACK_DECISION_ROTATION_WINDOW", "7m")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.PlaybackDecisionSecret != "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1" {
		t.Fatalf("expected playback decision secret from env, got %q", cfg.PlaybackDecisionSecret)
	}
	if cfg.PlaybackDecisionKeyID != "kid-env" {
		t.Fatalf("expected playback decision key id from env, got %q", cfg.PlaybackDecisionKeyID)
	}
	if len(cfg.PlaybackDecisionPreviousKeys) != 1 {
		t.Fatalf("expected one previous key, got %d", len(cfg.PlaybackDecisionPreviousKeys))
	}
	if cfg.PlaybackDecisionPreviousKeys[0] != "kid-old:abcdefghijklmnopqrstuvwxyz0123456789ABCDE2" {
		t.Fatalf("unexpected previous key entry: %q", cfg.PlaybackDecisionPreviousKeys[0])
	}
	if cfg.PlaybackDecisionRotationWindow != 7*time.Minute {
		t.Fatalf("expected rotation window 7m, got %v", cfg.PlaybackDecisionRotationWindow)
	}
}

func TestPlaybackDecisionPreviousKeysRequirePositiveRotationWindow(t *testing.T) {
	cfg := mustLoadConfigForPlaybackDecisionValidation(t)
	cfg.PlaybackDecisionPreviousKeys = []string{"kid-old:abcdefghijklmnopqrstuvwxyz0123456789ABCDE2"}
	cfg.PlaybackDecisionRotationWindow = 0

	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "PlaybackDecisionRotationWindow") {
		t.Fatalf("expected PlaybackDecisionRotationWindow validation error, got %v", err)
	}
}

func TestPlaybackDecisionKeyIDValidation(t *testing.T) {
	cfg := mustLoadConfigForPlaybackDecisionValidation(t)
	cfg.PlaybackDecisionKeyID = "INVALID KID"
	cfg.PlaybackDecisionSecret = "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"

	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "PlaybackDecisionKeyID") {
		t.Fatalf("expected PlaybackDecisionKeyID validation error, got %v", err)
	}
}

func mustLoadConfigForPlaybackDecisionValidation(t *testing.T) AppConfig {
	t.Helper()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_OWI_BASE", "http://example.com")

	loader := NewLoader("", "test")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	return cfg
}
