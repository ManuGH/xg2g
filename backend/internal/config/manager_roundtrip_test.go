// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveLoadRoundTrip guards against the config-brick regression: Manager.Save
// must never emit a legacy openWebIF.* section, because the loader hard-rejects
// any file containing that key. Before the fix, ToFileConfig always wrote
// openWebIF, so a single config save through the API made the file unloadable —
// bricking hot-reload and the next daemon restart.
func TestSaveLoadRoundTrip(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	SetRequiredTestSecrets(t)

	// 1. Start from a valid canonical (enigma2.*) config with non-default
	// receiver settings that must survive a Save -> Load round-trip.
	srcPath := filepath.Join(t.TempDir(), "config.yaml")
	src := strings.TrimSpace(`
enigma2:
  baseUrl: "http://receiver.example.com"
  retries: 3
  streamPort: 17999
  useWebIFStreams: true
`)
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write source config: %v", err)
	}
	cfg, err := NewLoader(srcPath, "dev").Load()
	if err != nil {
		t.Fatalf("load source config: %v", err)
	}

	// 2. Save it back out (simulates PUT /api/v3/system/config).
	savedPath := filepath.Join(t.TempDir(), "saved.yaml")
	if err := NewManager(savedPath).Save(&cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// 3. The serialized file must NOT contain the legacy openWebIF key.
	saved, err := os.ReadFile(savedPath) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(saved), "openWebIF") {
		t.Fatalf("saved config contains legacy openWebIF section (would brick reload):\n%s", saved)
	}

	// 4. The saved file must reload cleanly — this is what reload/restart does.
	reloaded, err := NewLoader(savedPath, "dev").Load()
	if err != nil {
		t.Fatalf("re-loading saved config failed (config-brick regression): %v", err)
	}

	// 5. Receiver settings must survive the round-trip (no silent downgrade).
	if reloaded.Enigma2.StreamPort != 17999 {
		t.Errorf("streamPort not preserved across Save->Load: got %d, want 17999 (silent StreamRelay downgrade)", reloaded.Enigma2.StreamPort)
	}
	if !reloaded.Enigma2.UseWebIFStreams {
		t.Errorf("useWebIFStreams not preserved across Save->Load: got false, want true")
	}
}
