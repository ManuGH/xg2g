// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/validate"
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

func TestCodegenMergeFile_Characterization(t *testing.T) {
	loader := NewLoader("", "")

	trueVal := true
	daysVal := 7

	fileCfg := &FileConfig{
		LogLevel: "debug",
		DataDir:  "/tmp/test-data",
		EPG: EPGConfig{
			Enabled: &trueVal,
			Days:    &daysVal,
		},
	}

	cfgDst := &AppConfig{}
	if err := loader.mergeFileConfig(cfgDst, fileCfg); err != nil {
		t.Fatalf("mergeFileConfig failed: %v", err)
	}

	if cfgDst.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfgDst.LogLevel)
	}
	if cfgDst.DataDir != "/tmp/test-data" {
		t.Errorf("DataDir = %q, want /tmp/test-data", cfgDst.DataDir)
	}
	if !cfgDst.EPGEnabled {
		t.Errorf("EPGEnabled = false, want true")
	}
	if cfgDst.EPGDays != 7 {
		t.Errorf("EPGDays = %d, want 7", cfgDst.EPGDays)
	}
}

func TestCodegenValidation_Characterization(t *testing.T) {
	v := validate.New()
	cfg := AppConfig{
		EPGEnabled: true,
		EPGDays:    99, // Out of range (1-14)
	}

	validateConfigGenerated(v, cfg)
	if v.IsValid() {
		t.Error("expected validation failure for out-of-range EPGDays, got valid")
	}
}
