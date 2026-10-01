package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	iptvSecret32 = "echo-" + strings.Repeat("q", 27) // exactly 32 characters
	iptvSecret31 = "echo-" + strings.Repeat("q", 26) // exactly 31 characters
)

func TestIPTVSourceSecret_TestFixtureLengths(t *testing.T) {
	if len(iptvSecret32) != 32 || len(iptvSecret31) != 31 {
		t.Fatalf("fixture lengths wrong: %d / %d", len(iptvSecret32), len(iptvSecret31))
	}
}

func TestIPTVSourceSecret_Validation(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"unset is valid (feature disabled, fail closed)", "", false},
		{"whitespace only is treated as unset", "   \t ", false},
		{"32 characters is valid", iptvSecret32, false},
		{"31 characters is rejected", iptvSecret31, true},
		{"padded 31 characters is rejected after trim", "  " + iptvSecret31 + "  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseValidationConfig()
			cfg.IPTVSourceSecret = tt.secret
			err := Validate(cfg)
			hasField := err != nil && strings.Contains(err.Error(), "IPTVSourceSecret")
			if tt.wantErr && !hasField {
				t.Fatalf("expected IPTVSourceSecret validation error, got %v", err)
			}
			if !tt.wantErr && hasField {
				t.Fatalf("unexpected IPTVSourceSecret validation error: %v", err)
			}
			if err != nil && strings.TrimSpace(tt.secret) != "" && strings.Contains(err.Error(), strings.TrimSpace(tt.secret)) {
				t.Fatalf("validation error echoes the secret value: %v", err)
			}
		})
	}
}

func TestIPTVSourceSecret_LoadFromEnv(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_IPTV_SOURCE_SECRET", iptvSecret32)

	cfg, err := NewLoader("", "test").Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.IPTVSourceSecret != iptvSecret32 {
		t.Fatalf("IPTVSourceSecret not loaded from env")
	}
}

func TestIPTVSourceSecret_LoadRejectsShortEnvWithoutEchoing(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_IPTV_SOURCE_SECRET", iptvSecret31)

	_, err := NewLoader("", "test").Load()
	if err == nil {
		t.Fatal("expected Load() to reject a 31 character IPTV source secret")
	}
	if !strings.Contains(err.Error(), "IPTVSourceSecret") {
		t.Fatalf("error should name the field: %v", err)
	}
	if strings.Contains(err.Error(), iptvSecret31) {
		t.Fatalf("error echoes the secret value: %v", err)
	}
}

func TestIPTVSourceSecret_FileConfigAndEnvPrecedence(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "enigma2:\n  baseUrl: http://file.local\niptv:\n  source_secret: " + iptvSecret32 + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Run("file value is used", func(t *testing.T) {
		cfg, err := NewLoader(configPath, "test").Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}
		if cfg.IPTVSourceSecret != iptvSecret32 {
			t.Fatalf("IPTVSourceSecret not loaded from file")
		}
	})

	t.Run("env overrides file", func(t *testing.T) {
		override := "env-override-secret-0123456789abcdefg"
		t.Setenv("XG2G_IPTV_SOURCE_SECRET", override)
		cfg, err := NewLoader(configPath, "test").Load()
		if err != nil {
			t.Fatalf("Load() failed: %v", err)
		}
		if cfg.IPTVSourceSecret != override {
			t.Fatalf("env value should win over file value")
		}
	})
}

// The IPTV secret must never be derived from or fall back to the playback
// decision secret (different purpose, independent rotation).
func TestIPTVSourceSecret_NoFallbackToDecisionSecret(t *testing.T) {
	SetRequiredTestSecrets(t)
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_E2_HOST", "http://example.com")
	t.Setenv("XG2G_DECISION_SECRET", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")

	cfg, err := NewLoader("", "test").Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.IPTVSourceSecret != "" {
		t.Fatalf("IPTVSourceSecret must stay empty without its own configuration, got a value")
	}
}

func TestIPTVSourceSecret_IsMaskedInDumps(t *testing.T) {
	t.Run("AppConfig", func(t *testing.T) {
		cfg := baseValidationConfig()
		cfg.IPTVSourceSecret = iptvSecret32
		m, ok := MaskSecrets(cfg).(map[string]any)
		if !ok {
			t.Fatal("expected map result")
		}
		if got := m["IPTVSourceSecret"]; got != "***" {
			t.Fatalf("IPTVSourceSecret not masked in AppConfig dump: %v", got)
		}
	})
	t.Run("FileConfig", func(t *testing.T) {
		secret := iptvSecret32
		fc := &FileConfig{IPTV: &IPTVFileConfig{SourceSecret: &secret}}
		out, ok := MaskSecrets(fc).(map[string]any)
		if !ok {
			t.Fatal("expected map result")
		}
		iptv, ok := out["IPTV"].(map[string]any)
		if !ok {
			t.Fatalf("expected IPTV section map, got %T", out["IPTV"])
		}
		if got := iptv["SourceSecret"]; got != "***" {
			t.Fatalf("SourceSecret not masked in FileConfig dump: %v", got)
		}
	})
}

func TestIPTVSourceSecret_RegistryEntry(t *testing.T) {
	reg, err := GetRegistry()
	if err != nil {
		t.Fatalf("GetRegistry() failed: %v", err)
	}
	e, ok := reg.ByEnv["XG2G_IPTV_SOURCE_SECRET"]
	if !ok {
		t.Fatal("registry entry for XG2G_IPTV_SOURCE_SECRET not found")
	}
	if e.Path != "iptv.source_secret" || e.FieldPath != "IPTVSourceSecret" {
		t.Fatalf("unexpected registry mapping: %+v", e)
	}
	if !e.Secret {
		t.Fatal("registry entry must be marked Secret")
	}
	if d, _ := e.Default.(string); d != "" {
		t.Fatal("default must be empty (feature off)")
	}
}
