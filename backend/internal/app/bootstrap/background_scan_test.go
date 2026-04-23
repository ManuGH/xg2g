package bootstrap

import "testing"

func TestBackgroundScanEnabled(t *testing.T) {
	t.Run("enabled by default", func(t *testing.T) {
		t.Setenv("XG2G_BACKGROUND_SCAN_ENABLED", "")
		if !backgroundScanEnabled() {
			t.Fatal("expected background scan to be enabled by default")
		}
	})

	t.Run("can be disabled explicitly", func(t *testing.T) {
		t.Setenv("XG2G_BACKGROUND_SCAN_ENABLED", "false")
		if backgroundScanEnabled() {
			t.Fatal("expected background scan to be disabled")
		}
	})
}
