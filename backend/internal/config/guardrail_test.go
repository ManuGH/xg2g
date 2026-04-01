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

	t.Run("legacy owi host suggests canonical host line", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_OWI_BASE=http://receiver.local"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_E2_HOST=http://receiver.local") {
			t.Fatalf("expected canonical host migration line, got %v", err)
		}
	})

	t.Run("legacy owi host with embedded credentials does not leak password", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_OWI_BASE=http://root:pw-123@receiver.local"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_E2_HOST=http://receiver.local") {
			t.Fatalf("expected sanitized host migration line, got %v", err)
		}
		if !strings.Contains(err.Error(), "XG2G_E2_USER=root") {
			t.Fatalf("expected canonical user migration line, got %v", err)
		}
		if !strings.Contains(err.Error(), "XG2G_E2_PASS") {
			t.Fatalf("expected canonical password hint, got %v", err)
		}
		if strings.Contains(err.Error(), "pw-123") {
			t.Fatalf("legacy error leaked password: %v", err)
		}
	})

	t.Run("legacy timeout suggests canonical duration", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_OWI_TIMEOUT_MS=5000"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_E2_TIMEOUT=5s") {
			t.Fatalf("expected canonical duration migration line, got %v", err)
		}
	})

	t.Run("legacy stream port explains deprecated canonical override", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_STREAM_PORT=8001"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_E2_STREAM_PORT=8001") {
			t.Fatalf("expected canonical stream port migration line, got %v", err)
		}
		if !strings.Contains(err.Error(), "itself deprecated") {
			t.Fatalf("expected deprecation note, got %v", err)
		}
	})

	t.Run("legacy monetization unlock scope suggests required scopes", func(t *testing.T) {
		err := CheckLegacyEnvWithEnviron([]string{"XG2G_MONETIZATION_UNLOCK_SCOPE=xg2g:unlock"})
		if err == nil {
			t.Fatalf("expected legacy key error")
		}
		if !strings.Contains(err.Error(), "XG2G_MONETIZATION_REQUIRED_SCOPES=xg2g:unlock") {
			t.Fatalf("expected monetization migration line, got %v", err)
		}
	})
}
