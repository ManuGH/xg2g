// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStrictConfig_FailsOnUnknownFields verifies that v3.0.0 strict mode
// correctly rejects configuration files with unknown fields.
func TestStrictConfig_FailsOnUnknownFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Config with a typo/unknown field "unknownField"
	yamlContent := `
dataDir: /tmp
unknownField: should_fail
openWebIF:
  baseUrl: http://test.local
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Load with default settings (should imply strict mode for v3.0.0)
	loader := NewLoader(configPath, "")
	_, err := loader.Load()

	if err == nil {
		t.Fatal("expected error due to unknown field in strict mode, got nil")
	}
	if !errors.Is(err, ErrUnknownConfigField) {
		t.Fatalf("expected ErrUnknownConfigField, got: %v", err)
	}

	// Verify error message mentions the unknown field
	// The standard yaml strict error usually looks like "line X: field unknownField not found in type config.AppConfig"
	if !strings.Contains(err.Error(), "field unknownField not found") && !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected error to mention unknown field, got: %v", err)
	}
}
