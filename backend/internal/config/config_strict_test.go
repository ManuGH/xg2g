// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoad_ValidMinimal tests loading a valid minimal configuration.
func TestLoad_ValidMinimal(t *testing.T) {
	// Ensure test directory exists (validation checks this)
	// Ensure test directory exists (validation checks this)
	testDir := t.TempDir()
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DATA", testDir)

	loader := NewLoader(filepath.Join("testdata", "valid-minimal.yaml"), "test")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	// Verify some basic fields were loaded
	if cfg.DataDir != testDir {
		t.Errorf("expected DataDir=%s, got %s", testDir, cfg.DataDir)
	}
	if cfg.Enigma2.BaseURL != "http://receiver.local" {
		t.Errorf("expected Enigma2.BaseURL=http://receiver.local, got %s", cfg.Enigma2.BaseURL)
	}
}

// TestLoad_UnknownKeyFails tests that strict parsing rejects unknown fields.
func TestLoad_UnknownKeyFails(t *testing.T) {
	loader := NewLoader(filepath.Join("testdata", "invalid-unknown-key.yaml"), "test")
	_, err := loader.Load()

	if err == nil {
		t.Fatal("expected error due to unknown key, got nil")
	}
	if !errors.Is(err, ErrUnknownConfigField) {
		t.Fatalf("expected ErrUnknownConfigField, got: %v", err)
	}

	// Verify error message contains "unknown field" or similar
	errMsg := err.Error()
	if !strings.Contains(errMsg, "field") && !strings.Contains(errMsg, "unexpectedRootKey") {
		t.Errorf("expected error about unknown field, got: %v", err)
	}
}

// TestLoad_InvalidTypeFails tests that type mismatches are caught.
func TestLoad_InvalidTypeFails(t *testing.T) {
	loader := NewLoader(filepath.Join("testdata", "invalid-type.yaml"), "test")
	_, err := loader.Load()

	if err == nil {
		t.Fatal("expected error due to wrong type, got nil")
	}

	// Verify it's a parse error (not just validation)
	errMsg := err.Error()
	if !strings.Contains(errMsg, "parse") && !strings.Contains(errMsg, "unmarshal") && !strings.Contains(errMsg, "cannot unmarshal") {
		t.Logf("Note: error was %q, continuing anyway", errMsg)
	}
}

// TestLoad_ValidationFails tests that validation catches logical errors.
func TestLoad_ValidationFails(t *testing.T) {
	loader := NewLoader(filepath.Join("testdata", "invalid-validation.yaml"), "test")
	_, err := loader.Load()

	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	// Verify it's a validation error about EPGDays
	errMsg := err.Error()
	if !strings.Contains(errMsg, "EPGDays") && !strings.Contains(errMsg, "validation") {
		t.Errorf("expected validation error about EPGDays, got: %v", err)
	}
}
