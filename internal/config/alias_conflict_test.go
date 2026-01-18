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

func TestAliasConflictOpenWebIFEnigma2(t *testing.T) {
	cases := []struct {
		name       string
		yaml       string
		wantErr    bool
		errMessage string
	}{
		{
			name: "only openWebIF set",
			yaml: `
openWebIF:
  baseUrl: "http://example.com"
`,
		},
		{
			name: "only enigma2 set",
			yaml: `
enigma2:
  baseUrl: "http://example.com"
`,
		},
		{
			name: "both set same",
			yaml: `
openWebIF:
  baseUrl: "http://example.com"
  timeout: "10s"
enigma2:
  baseUrl: "http://example.com"
  timeout: "10s"
`,
		},
		{
			name: "both set differ",
			yaml: `
openWebIF:
  baseUrl: "http://example.com"
  timeout: "10s"
enigma2:
  baseUrl: "http://example.com"
  timeout: "5s"
`,
			wantErr:    true,
			errMessage: "openWebIF.timeout conflicts with enigma2.timeout",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(strings.TrimSpace(tc.yaml)), 0644); err != nil {
				t.Fatalf("write temp config: %v", err)
			}

			loader := NewLoader(path, "dev")
			_, err := loader.Load()

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errMessage != "" && !strings.Contains(err.Error(), tc.errMessage) {
					t.Fatalf("expected error to contain %q, got %q", tc.errMessage, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAliasConflictEnvToEnv(t *testing.T) {
	t.Run("env mismatch fails", func(t *testing.T) {
		t.Setenv("XG2G_OWI_TIMEOUT_MS", "10000")
		t.Setenv("XG2G_E2_TIMEOUT", "5s")
		t.Setenv("XG2G_E2_HOST", "http://example.com")

		loader := NewLoader("", "dev")
		_, err := loader.Load()
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "openWebIF.timeout conflicts with enigma2.timeout") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("env match passes", func(t *testing.T) {
		t.Setenv("XG2G_OWI_TIMEOUT_MS", "10000")
		t.Setenv("XG2G_E2_TIMEOUT", "10s")
		t.Setenv("XG2G_E2_HOST", "http://example.com")

		loader := NewLoader("", "dev")
		_, err := loader.Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestAliasConflictYamlVsEnv(t *testing.T) {
	t.Run("yaml openWebIF vs env enigma2 mismatch", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), "openWebIF.timeout conflicts with enigma2.timeout") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("yaml enigma2 vs env openWebIF mismatch", func(t *testing.T) {
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
		if !strings.Contains(err.Error(), "openWebIF.timeout conflicts with enigma2.timeout") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
