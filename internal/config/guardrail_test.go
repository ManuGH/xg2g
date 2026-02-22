package config

import (
	"strings"
	"testing"
)

func TestCheckLegacyEnvWithEnviron(t *testing.T) {
	t.Run("no legacy keys", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"PATH=/usr/bin", "XG2G_STORE_PATH=/tmp/store"})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("legacy v3 prefix key", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_V3_STORE_PATH=/tmp/legacy"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_V3_STORE_PATH") {
			t.Fatalf("expected error to mention key, got %v", err)
		}
	})

	t.Run("legacy split key", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_FFMPEG_PATH=/usr/bin/ffmpeg"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_FFMPEG_PATH") {
			t.Fatalf("expected error to mention split key, got %v", err)
		}
	})
}
