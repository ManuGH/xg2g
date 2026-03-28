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

func TestLegacyOpenWebIFYAMLRejected(t *testing.T) {
	cases := []struct {
		name       string
		yaml       string
		errMessage string
	}{
		{
			name: "only openWebIF set",
			yaml: `
openWebIF:
  baseUrl: "http://example.com"
`,
			errMessage: "openWebIF.baseUrl -> enigma2.baseUrl",
		},
		{
			name: "mixed openWebIF and enigma2 keys still fail on legacy key",
			yaml: `
openWebIF:
  baseUrl: "http://example.com"
enigma2:
  password: "secret"
`,
			errMessage: "openWebIF.baseUrl -> enigma2.baseUrl",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XG2G_STORE_PATH", t.TempDir())
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(strings.TrimSpace(tc.yaml)), 0644); err != nil {
				t.Fatalf("write temp config: %v", err)
			}

			loader := NewLoader(path, "dev")
			_, err := loader.Load()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if tc.errMessage != "" && !strings.Contains(err.Error(), tc.errMessage) {
				t.Fatalf("expected error to contain %q, got %q", tc.errMessage, err.Error())
			}
			if !strings.Contains(err.Error(), enigma2MigrationGuide) {
				t.Fatalf("expected migration guide in error, got %q", err.Error())
			}
		})
	}
}

func TestCanonicalEnigma2YAMLPasses(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := `
enigma2:
  baseUrl: "http://example.com"
`
	if err := os.WriteFile(path, []byte(strings.TrimSpace(cfg)), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	loader := NewLoader(path, "dev")
	if _, err := loader.Load(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAliasConflictYamlVsEnv(t *testing.T) {
	t.Run("yaml openWebIF fails before env conflict resolution", func(t *testing.T) {
		t.Setenv("XG2G_STORE_PATH", t.TempDir())
		t.Setenv("XG2G_E2_TIMEOUT", "5s")
		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg := `
openWebIF:
  baseUrl: "http://example.com"
  timeout: "10s"
`
		if err := os.WriteFile(path, []byte(strings.TrimSpace(cfg)), 0644); err != nil {
			t.Fatalf("write temp config: %v", err)
		}
		loader := NewLoader(path, "dev")
		_, err := loader.Load()
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "openWebIF.timeout -> enigma2.timeout") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), enigma2MigrationGuide) {
			t.Fatalf("expected migration guide in error, got %v", err)
		}
	})

	t.Run("yaml enigma2 vs env openWebIF mismatch", func(t *testing.T) {
		t.Setenv("XG2G_STORE_PATH", t.TempDir())
		t.Setenv("XG2G_OWI_TIMEOUT_MS", "5000")
		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg := `
enigma2:
  baseUrl: "http://example.com"
  timeout: "10s"
`
		if err := os.WriteFile(path, []byte(strings.TrimSpace(cfg)), 0644); err != nil {
			t.Fatalf("write temp config: %v", err)
		}
		loader := NewLoader(path, "dev")
		_, err := loader.Load()
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "XG2G_OWI_TIMEOUT_MS") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "XG2G_E2_TIMEOUT=5s") {
			t.Fatalf("expected migration hint, got %v", err)
		}
	})
}

func TestLegacyEnvRejectedBeforeEnvAliasResolution(t *testing.T) {
	t.Run("env mismatch fails with migration hint", func(t *testing.T) {
		t.Setenv("XG2G_STORE_PATH", t.TempDir())
		t.Setenv("XG2G_OWI_TIMEOUT_MS", "10000")
		t.Setenv("XG2G_E2_TIMEOUT", "5s")
		t.Setenv("XG2G_E2_HOST", "http://example.com")

		loader := NewLoader("", "dev")
		_, err := loader.Load()
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "XG2G_OWI_TIMEOUT_MS") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "XG2G_E2_TIMEOUT=10s") {
			t.Fatalf("expected migration hint, got %v", err)
		}
	})

	t.Run("env match still fails because legacy env is forbidden", func(t *testing.T) {
		t.Setenv("XG2G_STORE_PATH", t.TempDir())
		t.Setenv("XG2G_OWI_TIMEOUT_MS", "10000")
		t.Setenv("XG2G_E2_TIMEOUT", "10s")
		t.Setenv("XG2G_E2_HOST", "http://example.com")

		loader := NewLoader("", "dev")
		_, err := loader.Load()
		if err == nil {
			t.Fatal("expected legacy env error, got nil")
		}
		if !strings.Contains(err.Error(), "XG2G_OWI_TIMEOUT_MS") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
