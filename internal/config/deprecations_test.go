// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

func TestDeprecationRegistry(t *testing.T) {
	// Save original registry
	originalRegistry := make(map[string]Deprecation)
	for k, v := range deprecationRegistry {
		originalRegistry[k] = v
	}
	defer func() {
		deprecationRegistry = originalRegistry
	}()

	// Clear registry for testing
	ClearDeprecations()

	// Test empty registry
	if len(deprecationRegistry) != 0 {
		t.Errorf("expected empty registry after ClearDeprecations, got %d entries", len(deprecationRegistry))
	}

	// Test adding a deprecation
	dep := Deprecation{
		OldField:        "timeout_ms",
		NewField:        "timeoutMs",
		DeprecatedSince: "1.8.0",
		RemovalVersion:  "2.0.0",
	}
	AddDeprecation(dep)

	// Test retrieval
	retrieved, found := GetDeprecation("timeout_ms")
	if !found {
		t.Error("expected to find deprecation for 'timeout_ms'")
	}
	if retrieved.OldField != dep.OldField {
		t.Errorf("expected OldField=%s, got %s", dep.OldField, retrieved.OldField)
	}
	if retrieved.NewField != dep.NewField {
		t.Errorf("expected NewField=%s, got %s", dep.NewField, retrieved.NewField)
	}

	// Test not found
	_, found = GetDeprecation("nonexistent_field")
	if found {
		t.Error("expected not to find deprecation for 'nonexistent_field'")
	}
}

func TestDeprecationSummary(t *testing.T) {
	// Save original registry
	originalRegistry := make(map[string]Deprecation)
	for k, v := range deprecationRegistry {
		originalRegistry[k] = v
	}
	defer func() {
		deprecationRegistry = originalRegistry
	}()

	// Test empty summary
	ClearDeprecations()
	summary := DeprecationSummary()
	if summary != "No deprecated configuration fields" {
		t.Errorf("expected empty summary message, got: %s", summary)
	}

	// Add a deprecation
	AddDeprecation(Deprecation{
		OldField:        "old_field",
		NewField:        "newField",
		DeprecatedSince: "1.5.0",
		RemovalVersion:  "2.0.0",
	})

	summary = DeprecationSummary()
	if !strings.Contains(summary, "old_field") {
		t.Error("expected summary to contain 'old_field'")
	}
	if !strings.Contains(summary, "newField") {
		t.Error("expected summary to contain 'newField'")
	}
	if !strings.Contains(summary, "1.5.0") {
		t.Error("expected summary to contain deprecation version")
	}
	if !strings.Contains(summary, "2.0.0") {
		t.Error("expected summary to contain removal version")
	}
}

func TestValidateDeprecations(t *testing.T) {
	cfg := &FileConfig{
		Version: "1",
		DataDir: "/tmp/test",
	}

	// Currently a no-op, but should not error
	err := ValidateDeprecations(cfg)
	if err != nil {
		t.Errorf("ValidateDeprecations should not error, got: %v", err)
	}
}

func TestCheckDeprecations(t *testing.T) {
	// Save original registry
	originalRegistry := make(map[string]Deprecation)
	for k, v := range deprecationRegistry {
		originalRegistry[k] = v
	}
	defer func() {
		deprecationRegistry = originalRegistry
	}()

	// This test verifies that checkDeprecations doesn't panic
	// In the future when we implement actual detection, this test
	// should verify warning logs

	yamlData := []byte(`
version: "1"
dataDir: /tmp/test
openWebIF:
  baseUrl: http://test.local
bouquets:
  - userbouquet.test.tv
epg:
  enabled: true
  days: 7
`)

	// Should not panic
	checkDeprecations(yamlData)
}
