// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/oasdiff/yaml"
)

// Test helper: create a minimal valid config file
func writeValidConfig(t *testing.T, path string, bouquet string) {
	t.Helper()
	// Use map/struct to marshal correct YAML to avoid indentation issues
	cfg := map[string]interface{}{
		"dataDir": "/tmp/test",
		"openWebIF": map[string]interface{}{
			"baseUrl":  "http://test.example.com",
			"username": "testuser",
			"password": "testpass",
		},
		"bouquets": []string{bouquet},
		"epg": map[string]interface{}{
			"enabled": true,
			"days":    7,
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

// TestNewConfigHolder tests the ConfigHolder constructor.
func TestNewConfigHolder(t *testing.T) {
	initial := AppConfig{
		Bouquet:    "test-bouquet",
		DataDir:    "/tmp/test",
		EPGDays:    7,
		EPGEnabled: true,
		Enigma2: Enigma2Settings{
			BaseURL:  "http://test.example.com",
			Username: "user",
			Password: "pass",
		},
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

func TestConfigHolder_Swap_AssignsMonotonicEpoch(t *testing.T) {
	initial := AppConfig{
		Bouquet:    "test-bouquet",
		DataDir:    "/tmp/test",
		EPGDays:    7,
		EPGEnabled: true,
		Enigma2: Enigma2Settings{
			BaseURL:  "http://test.example.com",
			Username: "user",
			Password: "pass",
		},
	}

	loader := NewLoader("", "test")
	holder := NewConfigHolder(initial, loader, "")

	first := holder.Current()
	if first == nil {
		t.Fatal("expected initial snapshot, got nil")
	}
	if first.Epoch == 0 {
		t.Fatal("expected initial snapshot epoch to be non-zero")
	}

	next := BuildSnapshot(initial, DefaultEnv())
	next.Epoch = 12345 // should be overwritten by Swap()
	nextPtr := &next

	prev := holder.Swap(nextPtr)
	if prev == nil {
		t.Fatal("expected previous snapshot, got nil")
	}
	if prev != first {
		t.Fatalf("expected Swap() to return previous snapshot pointer")
	}

	got := holder.Current()
	if got != nextPtr {
		t.Fatalf("expected Current() to return swapped snapshot pointer")
	}
	if got.Epoch != first.Epoch+1 {
		t.Fatalf("expected epoch to increment: got %d, want %d", got.Epoch, first.Epoch+1)
	}
}

// TestConfigHolder_Get tests thread-safe config read.
func TestConfigHolder_Get(t *testing.T) {
	cfg := AppConfig{
		Bouquet: "initial",
		EPGDays: 5,
		Enigma2: Enigma2Settings{
			BaseURL:  "http://test.example.com",
			Username: "user",
			Password: "pass",
		},
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

	// Write invalid config (EPG days out of range)
	invalidContent := `
openWebIF:
  baseUrl: http://test.example.com
epg:
  enabled: true
  days: 99
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0600); err != nil {
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
	ch := make(chan AppConfig, 1)
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
	ch := make(chan AppConfig)
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

func TestConfigHolder_ReloadDuringRequest_UsesSingleEpoch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	writeValidConfig(t, configPath, "old-bouquet")

	loader := NewLoader(configPath, "test")
	initial, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}

	holder := NewConfigHolder(initial, loader, configPath)

	firstEpochCh := make(chan uint64, 1)
	secondEpochCh := make(chan uint64, 1)
	proceed := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := holder.Current()
		if snap == nil {
			http.Error(w, "no snapshot", http.StatusInternalServerError)
			return
		}

		firstEpochCh <- snap.Epoch
		<-proceed
		secondEpochCh <- snap.Epoch

		_, _ = fmt.Fprintf(w, "%d,%d", snap.Epoch, snap.Epoch)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(rr, req)
	}()

	firstEpoch := <-firstEpochCh

	writeValidConfig(t, configPath, "new-bouquet")
	if err := holder.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() failed: %v", err)
	}

	current := holder.Current()
	if current == nil {
		t.Fatal("expected snapshot after reload, got nil")
	}
	if current.Epoch <= firstEpoch {
		t.Fatalf("expected epoch to increase after reload: got %d, want > %d", current.Epoch, firstEpoch)
	}
	if current.App.Bouquet != "new-bouquet" {
		t.Fatalf("expected Bouquet %q after reload, got %q", "new-bouquet", current.App.Bouquet)
	}

	close(proceed)
	<-done

	secondEpoch := <-secondEpochCh
	if secondEpoch != firstEpoch {
		t.Fatalf("expected request to use a single epoch: got %d then %d", firstEpoch, secondEpoch)
	}
}

// TestConfigHolder_LogChanges tests config change logging.
func TestConfigHolder_LogChanges(t *testing.T) {
	old := AppConfig{
		Bouquet:    "old-bouquet",
		EPGEnabled: false,
		EPGDays:    3,
		Enigma2: Enigma2Settings{
			StreamPort: 8080,
			BaseURL:    "http://old.example.com",
			Username:   "user",
			Password:   "pass",
		},
	}

	newCfg := AppConfig{
		Bouquet:    "new-bouquet",
		EPGEnabled: true,
		EPGDays:    7,
		Enigma2: Enigma2Settings{
			StreamPort: 9090,
			BaseURL:    "http://new.example.com",
			Username:   "user",
			Password:   "pass",
		},
	}

	loader := NewLoader("", "test")
	holder := NewConfigHolder(old, loader, "")

	// Call logChanges (should not panic)
	holder.logChanges(old, newCfg)

	// Test passes if no panic occurred
}

// TestConfigHolder_Stop tests Stop method.
func TestConfigHolder_Stop(t *testing.T) {
	cfg := AppConfig{
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
	cfg := AppConfig{
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

// TestConfigHolder_Reload_StrictParseFailure tests reload with YAML strict parsing errors.
// Verifies that invalid YAML (unknown fields) preserves the old config.
func TestConfigHolder_Reload_StrictParseFailure(t *testing.T) {
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

	// Write config with unknown field (strict parsing should reject)
	invalidContent := `
dataDir: /tmp/test
openWebIF:
  baseUrl: http://test.example.com
bouquets:
  - test-bouquet
epg:
  enabled: true
  days: 7
unknownField: this-should-be-rejected
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	// Reload should fail due to strict parsing
	ctx := context.Background()
	err = holder.Reload(ctx)
	if err == nil {
		t.Fatal("expected Reload() to fail with strict parsing error, got nil")
	}

	// Verify old config is unchanged
	got := holder.Get()
	if got.Bouquet != "stable-bouquet" {
		t.Errorf("expected old config to be preserved after parse error, got Bouquet %q", got.Bouquet)
	}
}

// TestConfigHolder_Reload_TypeMismatch tests reload with YAML type errors.
// Verifies that type mismatches preserve the old config.
func TestConfigHolder_Reload_TypeMismatch(t *testing.T) {
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

	// Write config with type mismatch (days should be int, not string)
	invalidContent := `
dataDir: /tmp/test
openWebIF:
  baseUrl: http://test.example.com
bouquets:
  - test-bouquet
epg:
  enabled: true
  days: "seven"
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	// Reload should fail due to type mismatch
	ctx := context.Background()
	err = holder.Reload(ctx)
	if err == nil {
		t.Fatal("expected Reload() to fail with type mismatch error, got nil")
	}

	// Verify old config is unchanged
	got := holder.Get()
	if got.Bouquet != "stable-bouquet" {
		t.Errorf("expected old config to be preserved after type error, got Bouquet %q", got.Bouquet)
	}
}

// TestConfigHolder_Reload_BusinessLogicFailure tests reload with business logic validation errors.
// Verifies that configs that pass parsing but fail business validation preserve the old config.
func TestConfigHolder_Reload_BusinessLogicFailure(t *testing.T) {
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

	// Write config that parses but fails validation (epg.days out of range)
	invalidContent := `
dataDir: /tmp/test
openWebIF:
  baseUrl: http://test.example.com
bouquets:
  - test-bouquet
epg:
  enabled: true
  days: 99
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	// Reload should fail due to validation (days out of range 1-14)
	ctx := context.Background()
	err = holder.Reload(ctx)
	if err == nil {
		t.Fatal("expected Reload() to fail with validation error, got nil")
	}

	// Verify old config is unchanged
	got := holder.Get()
	if got.Bouquet != "stable-bouquet" {
		t.Errorf("expected old config to be preserved after validation error, got Bouquet %q", got.Bouquet)
	}
	if got.EPGDays != 7 {
		t.Errorf("expected old EPGDays=7 to be preserved, got %d", got.EPGDays)
	}
}
