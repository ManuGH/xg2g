// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build schemacheck

// SPDX-License-Identifier: MIT

package config

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestJSONSchemaValidation tests that check-jsonschema correctly validates
// all YAML fixtures against docs/guides/config.schema.json
func TestJSONSchemaValidation(t *testing.T) {
	// Skip if check-jsonschema is not installed
	if _, err := exec.LookPath("check-jsonschema"); err != nil {
		t.Skip("check-jsonschema not installed, skipping schema validation tests")
	}

	schemaPath := filepath.Join("..", "..", "docs", "guides", "config.schema.json")

	tests := []struct {
		name      string
		yamlFile  string
		wantValid bool
	}{
		{
			name:      "valid minimal config",
			yamlFile:  filepath.Join("testdata", "valid-minimal.yaml"),
			wantValid: true,
		},
		{
			name:      "config.example.yaml",
			yamlFile:  filepath.Join("..", "..", "config.example.yaml"),
			wantValid: true,
		},
		{
			name:      "invalid unknown key",
			yamlFile:  filepath.Join("testdata", "invalid-unknown-key.yaml"),
			wantValid: false,
		},
		{
			name:      "invalid type mismatch",
			yamlFile:  filepath.Join("testdata", "invalid-type.yaml"),
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// #nosec G204 -- test code with static schema path and controlled test inputs
			cmd := exec.Command("check-jsonschema", "--schemafile", schemaPath, tt.yamlFile)
			err := cmd.Run()

			if tt.wantValid && err != nil {
				t.Errorf("expected %s to be valid, but check-jsonschema failed: %v", tt.yamlFile, err)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("expected %s to be invalid, but check-jsonschema succeeded", tt.yamlFile)
			}
		})
	}
}

// TestJSONSchemaAgainstAllValidFixtures discovers all valid-*.yaml files
// in testdata and validates them against the schema
func TestJSONSchemaAgainstAllValidFixtures(t *testing.T) {
	// Skip if check-jsonschema is not installed
	if _, err := exec.LookPath("check-jsonschema"); err != nil {
		t.Skip("check-jsonschema not installed, skipping schema validation tests")
	}

	schemaPath := filepath.Join("..", "..", "docs", "guides", "config.schema.json")
	pattern := filepath.Join("testdata", "valid-*.yaml")

	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("failed to glob %s: %v", pattern, err)
	}

	if len(matches) == 0 {
		t.Fatalf("no valid-*.yaml files found in testdata")
	}

	for _, yamlFile := range matches {
		t.Run(filepath.Base(yamlFile), func(t *testing.T) {
			// #nosec G204 -- test code with static schema path and controlled test inputs
			cmd := exec.Command("check-jsonschema", "--schemafile", schemaPath, yamlFile)
			if err := cmd.Run(); err != nil {
				t.Errorf("schema validation failed for %s: %v", yamlFile, err)
			}
		})
	}
}

// TestJSONSchemaRejectsInvalidFixtures discovers all invalid-*.yaml files
// in testdata and ensures they are rejected by the schema
func TestJSONSchemaRejectsInvalidFixtures(t *testing.T) {
	// Skip if check-jsonschema is not installed
	if _, err := exec.LookPath("check-jsonschema"); err != nil {
		t.Skip("check-jsonschema not installed, skipping schema validation tests")
	}

	schemaPath := filepath.Join("..", "..", "docs", "guides", "config.schema.json")
	pattern := filepath.Join("testdata", "invalid-*.yaml")

	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("failed to glob %s: %v", pattern, err)
	}

	if len(matches) == 0 {
		t.Fatalf("no invalid-*.yaml files found in testdata")
	}

	for _, yamlFile := range matches {
		t.Run(filepath.Base(yamlFile), func(t *testing.T) {
			// #nosec G204 -- test code with static schema path and controlled test inputs
			cmd := exec.Command("check-jsonschema", "--schemafile", schemaPath, yamlFile)
			err := cmd.Run()
			if err == nil {
				t.Errorf("expected schema validation to fail for %s, but it succeeded", yamlFile)
			}
		})
	}
}
