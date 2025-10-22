// SPDX-License-Identifier: MIT
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
)

func TestLoadDefaults(t *testing.T) {
	loader := NewLoader("", "test-version")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Check defaults
	if cfg.DataDir != "/tmp" {
		t.Errorf("expected DataDir=/tmp, got %s", cfg.DataDir)
	}
	if cfg.StreamPort != 8001 {
		t.Errorf("expected StreamPort=8001, got %d", cfg.StreamPort)
	}
	if cfg.OWITimeout != 10*time.Second {
		t.Errorf("expected OWITimeout=10s, got %v", cfg.OWITimeout)
	}
	if cfg.OWIRetries != 3 {
		t.Errorf("expected OWIRetries=3, got %d", cfg.OWIRetries)
	}
	if cfg.Version != "test-version" {
		t.Errorf("expected Version=test-version, got %s", cfg.Version)
	}
}

func TestLoadFromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	customDataDir := filepath.Join(tmpDir, "custom-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://custom.local
  username: testuser
  password: testpass
  streamPort: 9001
  timeout: 20s
  retries: 5
bouquets:
  - Favourites
  - Premium
epg:
  enabled: true
  days: 14
  maxConcurrency: 10
api:
  token: test-token
picons:
  baseUrl: http://picons.local
`, customDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != customDataDir {
		t.Errorf("expected DataDir=%s, got %s", customDataDir, cfg.DataDir)
	}
	if cfg.OWIBase != "http://custom.local" {
		t.Errorf("expected OWIBase=http://custom.local, got %s", cfg.OWIBase)
	}
	if cfg.Bouquet != "Favourites,Premium" {
		t.Errorf("expected Bouquet=Favourites,Premium, got %s", cfg.Bouquet)
	}
}

func TestENVOverridesFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")
	envDataDir := filepath.Join(tmpDir, "env-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  baseUrl: http://file.local
  streamPort: 9001
`, fileDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_DATA", envDataDir)
	t.Setenv("XG2G_OWI_BASE", "http://env.local")
	t.Setenv("XG2G_STREAM_PORT", "7001")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != envDataDir {
		t.Errorf("expected ENV to override file: DataDir=%s, got %s", envDataDir, cfg.DataDir)
	}
	if cfg.OWIBase != "http://env.local" {
		t.Errorf("expected ENV to override file: OWIBase=http://env.local, got %s", cfg.OWIBase)
	}
	if cfg.StreamPort != 7001 {
		t.Errorf("expected ENV to override file: StreamPort=7001, got %d", cfg.StreamPort)
	}
}

func TestPrecedenceOrder(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	fileDataDir := filepath.Join(tmpDir, "file-data")

	yamlContent := fmt.Sprintf(`
dataDir: %s
openWebIF:
  streamPort: 9001
epg:
  days: 10
`, fileDataDir)

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Setenv("XG2G_EPG_DAYS", "5")

	loader := NewLoader(configPath, "1.0.0")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DataDir != fileDataDir {
		t.Errorf("expected DataDir from file: %s, got %s", fileDataDir, cfg.DataDir)
	}

	if cfg.StreamPort != 9001 {
		t.Errorf("expected StreamPort from file: 9001, got %d", cfg.StreamPort)
	}

	if cfg.EPGDays != 5 {
		t.Errorf("expected EPGDays from ENV: 5, got %d", cfg.EPGDays)
	}

	if cfg.OWIBase != "http://10.10.55.57" {
		t.Errorf("expected OWIBase from default: http://10.10.55.57, got %s", cfg.OWIBase)
	}
}

func TestValidateEPGBounds(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		cfg       func() jobs.Config
		shouldErr bool
		errMsg    string
	}{
		{
			name: "valid EPG config",
			cfg: func() jobs.Config {
				return jobs.Config{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: false,
		},
		{
			name: "EPGTimeoutMS too low",
			cfg: func() jobs.Config {
				return jobs.Config{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      50, // Too low
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGTimeoutMS too high",
			cfg: func() jobs.Config {
				return jobs.Config{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      100000, // Too high
					EPGRetries:        2,
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGTimeoutMS",
		},
		{
			name: "EPGRetries too high",
			cfg: func() jobs.Config {
				return jobs.Config{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        10, // Too high
					FuzzyMax:          2,
				}
			},
			shouldErr: true,
			errMsg:    "EPGRetries",
		},
		{
			name: "FuzzyMax too high",
			cfg: func() jobs.Config {
				return jobs.Config{
					DataDir:           tmpDir,
					OWIBase:           "http://test.local",
					StreamPort:        8001,
					Bouquet:           "test",
					EPGEnabled:        true,
					EPGDays:           7,
					EPGMaxConcurrency: 5,
					EPGTimeoutMS:      5000,
					EPGRetries:        2,
					FuzzyMax:          50, // Too high
				}
			},
			shouldErr: true,
			errMsg:    "FuzzyMax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg()
			err := Validate(cfg)

			if tt.shouldErr {
				if err == nil {
					t.Error("expected validation error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
