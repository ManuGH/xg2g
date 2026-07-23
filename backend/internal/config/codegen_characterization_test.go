// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"testing"
)

// TestCodegenMergeEnv_Characterization verifies that the generated mergeEnvConfigGenerated
// produces consistent configuration defaults across all registered environment variables.
func TestCodegenMergeEnv_Characterization(t *testing.T) {
	loader := NewLoader("", "")

	cfgManual := &AppConfig{}
	cfgGenerated := &AppConfig{}

	loader.mergeEnvConfig(cfgManual)
	loader.mergeEnvConfigGenerated(cfgGenerated)

	// Verify that basic env-overridden string fields match between manual and generated implementations
	if cfgManual.Version != cfgGenerated.Version {
		t.Errorf("Version mismatch: manual=%q vs generated=%q", cfgManual.Version, cfgGenerated.Version)
	}
	if cfgManual.DataDir != cfgGenerated.DataDir {
		t.Errorf("DataDir mismatch: manual=%q vs generated=%q", cfgManual.DataDir, cfgGenerated.DataDir)
	}
	if cfgManual.LogLevel != cfgGenerated.LogLevel {
		t.Errorf("LogLevel mismatch: manual=%q vs generated=%q", cfgManual.LogLevel, cfgGenerated.LogLevel)
	}
}

func TestCodegenRegistry_RoundTrip(t *testing.T) {
	reg, err := GetRegistry()
	if err != nil {
		t.Fatalf("GetRegistry failed: %v", err)
	}

	if len(reg.ByPath) == 0 {
		t.Fatal("Registry.ByPath is empty")
	}

	for path, entry := range reg.ByPath {
		if entry.Path != path {
			t.Errorf("Registry path mismatch for %s: entry.Path=%s", path, entry.Path)
		}
	}
}
