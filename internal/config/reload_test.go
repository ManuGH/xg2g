// SPDX-License-Identifier: MIT

package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/jobs"
)

// Test helper: create a minimal valid config file
func writeValidConfig(t *testing.T, path string, bouquet string) {
	t.Helper()
	content := `dataDir: /tmp/test
openWebIF:
  baseUrl: http://test.example.com
  username: testuser
  password: testpass
bouquets:
  - ` + bouquet + `
epg:
  enabled: true
  days: 7
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

// TestNewConfigHolder tests the ConfigHolder constructor.
func TestNewConfigHolder(t *testing.T) {
	initial := jobs.Config{
		Bouquet:     "test-bouquet",
		DataDir:     "/tmp/test",
		EPGDays:     7,
		EPGEnabled:  true,
		OWIBase:     "http://test.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}

	loader := NewLoader("", "test-version")
	holder := NewConfigHolder(initial, loader, "/path/to/config.yaml")

	if holder == nil {
		t.Fatal("expected ConfigHolder, got nil")
	}

	got := holder.Get()
	if got.Bouquet != initial.Bouquet {
		t.Errorf("expected Bouquet %q, got %q", initial.Bouquet, got.Bouquet)
	}
	if got.DataDir != initial.DataDir {
		t.Errorf("expected DataDir %q, got %q", initial.DataDir, got.DataDir)
	}
}

// TestConfigHolder_Get tests thread-safe config read.
func TestConfigHolder_Get(t *testing.T) {
	cfg := jobs.Config{
		Bouquet:     "initial",
		EPGDays:     5,
		OWIBase:     "http://test.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}

	loader := NewLoader("", "test")
	holder := NewConfigHolder(cfg, loader, "")

	// Test Get returns correct config
	got := holder.Get()
	if got.Bouquet != "initial" {
		t.Errorf("expected Bouquet %q, got %q", "initial", got.Bouquet)
	}

	// Test Get is thread-safe (returns copy, not reference)
	got.Bouquet = "modified"
	if holder.Get().Bouquet != "initial" {
		t.Error("Get() should return a copy, not a reference")
	}
}

// TestConfigHolder_Reload_Success tests successful config reload.
func TestConfigHolder_Reload_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write initial config
	writeValidConfig(t, configPath, "old-bouquet")

	// Load initial config
	loader := NewLoader(configPath, "test")
	initial, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}

	holder := NewConfigHolder(initial, loader, configPath)

	// Update config file
	writeValidConfig(t, configPath, "new-bouquet")

	// Reload
	ctx := context.Background()
	err = holder.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	// Verify config was updated
	got := holder.Get()
	if got.Bouquet != "new-bouquet" {
		t.Errorf("expected Bouquet %q after reload, got %q", "new-bouquet", got.Bouquet)
	}
}

// TestConfigHolder_Reload_ValidationFailure tests reload with invalid config.
func TestConfigHolder_Reload_ValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write valid initial config
	writeValidConfig(t, configPath, "stable-bouquet")

	loader := NewLoader(configPath, "test")
	initial, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}

	holder := NewConfigHolder(initial, loader, configPath)

	// Write invalid config (missing required openWebIF section)
	invalidContent := `
bouquets:
  - new-bouquet
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0644); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	// Reload should fail
	ctx := context.Background()
	err = holder.Reload(ctx)
	if err == nil {
		t.Fatal("expected Reload() to fail with validation error, got nil")
	}

	// Verify old config is unchanged
	got := holder.Get()
	if got.Bouquet != "stable-bouquet" {
		t.Errorf("expected old config to be preserved, got Bouquet %q", got.Bouquet)
	}
}

// TestConfigHolder_RegisterListener tests listener registration.
func TestConfigHolder_RegisterListener(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	writeValidConfig(t, configPath, "old")

	loader := NewLoader(configPath, "test")
	initial, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	holder := NewConfigHolder(initial, loader, configPath)

	// Register listener
	ch := make(chan jobs.Config, 1)
	holder.RegisterListener(ch)

	// Update config and reload
	writeValidConfig(t, configPath, "new")

	ctx := context.Background()
	err = holder.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	// Verify listener received new config
	select {
	case received := <-ch:
		if received.Bouquet != "new" {
			t.Errorf("expected listener to receive Bouquet %q, got %q", "new", received.Bouquet)
		}
	default:
		t.Error("listener did not receive config update")
	}
}

// TestConfigHolder_NotifyListeners_NonBlocking tests non-blocking notification.
func TestConfigHolder_NotifyListeners_NonBlocking(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	writeValidConfig(t, configPath, "old")

	loader := NewLoader(configPath, "test")
	initial, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	holder := NewConfigHolder(initial, loader, configPath)

	// Register listener with no buffer (should not block)
	ch := make(chan jobs.Config)
	holder.RegisterListener(ch)

	// Update and reload
	writeValidConfig(t, configPath, "new")

	ctx := context.Background()
	err = holder.Reload(ctx)
	if err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	// Test passes if Reload() didn't block
}

// TestMaskURL tests URL masking for logging.
func TestMaskURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty_url",
			input: "",
			want:  "",
		},
		{
			name:  "http_url",
			input: "http://example.com/path",
			want:  "***redacted***",
		},
		{
			name:  "https_url_with_creds",
			input: "https://user:pass@example.com:8080/path?query=1",
			want:  "***redacted***",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := maskURL(tc.input)
			if got != tc.want {
				t.Errorf("maskURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestConfigHolder_LogChanges tests config change logging.
func TestConfigHolder_LogChanges(t *testing.T) {
	old := jobs.Config{
		Bouquet:     "old-bouquet",
		EPGEnabled:  false,
		EPGDays:     3,
		StreamPort:  8080,
		OWIBase:     "http://old.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}

	newCfg := jobs.Config{
		Bouquet:     "new-bouquet",
		EPGEnabled:  true,
		EPGDays:     7,
		StreamPort:  9090,
		OWIBase:     "http://new.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}

	loader := NewLoader("", "test")
	holder := NewConfigHolder(old, loader, "")

	// Call logChanges (should not panic)
	holder.logChanges(old, newCfg)

	// Test passes if no panic occurred
}

// TestConfigHolder_Stop tests Stop method.
func TestConfigHolder_Stop(t *testing.T) {
	cfg := jobs.Config{
		Bouquet:     "test",
		OWIBase:     "http://test.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}
	loader := NewLoader("", "test")
	holder := NewConfigHolder(cfg, loader, "")

	// Call Stop (should not panic even if watcher is nil)
	holder.Stop()

	// Test passes if no panic occurred
}

// TestConfigHolder_StartWatcher_EmptyPath tests watcher with empty path.
func TestConfigHolder_StartWatcher_EmptyPath(t *testing.T) {
	cfg := jobs.Config{
		Bouquet:     "test",
		OWIBase:     "http://test.example.com",
		OWIUsername: "user",
		OWIPassword: "pass",
	}
	loader := NewLoader("", "test")
	holder := NewConfigHolder(cfg, loader, "") // Empty config path

	ctx := context.Background()
	err := holder.StartWatcher(ctx)
	if err != nil {
		t.Errorf("StartWatcher with empty path should not error, got: %v", err)
	}

	// Clean up
	holder.Stop()
}
