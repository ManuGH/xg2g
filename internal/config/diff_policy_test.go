package config

import "testing"

func TestDiff_RestartPolicyFromRegistry(t *testing.T) {
	base := AppConfig{
		LogLevel:      "info",
		APIListenAddr: ":8088",
	}

	t.Run("LogLevelIsHotReloadable", func(t *testing.T) {
		next := base
		next.LogLevel = "debug"

		diff, err := Diff(base, next)
		if err != nil {
			t.Fatalf("Diff() failed: %v", err)
		}
		if diff.RestartRequired {
			t.Fatalf("expected no restart for LogLevel change, got restart required")
		}
	})

	t.Run("ListenAddressRequiresRestart", func(t *testing.T) {
		next := base
		next.APIListenAddr = ":9090"

		diff, err := Diff(base, next)
		if err != nil {
			t.Fatalf("Diff() failed: %v", err)
		}
		if !diff.RestartRequired {
			t.Fatalf("expected restart for APIListenAddr change, got hot-reload")
		}
	})
}
