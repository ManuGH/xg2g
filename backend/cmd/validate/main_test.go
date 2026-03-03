// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setCISafeEnv sets environment variables on cmd to avoid permission issues in CI.
// It sets XG2G_STORE_PATH and XG2G_DATA to the provided temp directory.
func setCISafeEnv(cmd *exec.Cmd, tmpDir string) {
	cmd.Env = append(os.Environ(),
		"XG2G_STORE_PATH="+tmpDir,
		"XG2G_DATA="+tmpDir,
	)
}

// TestValidateCLI tests the validate binary with various config files
func TestValidateCLI(t *testing.T) {
	// Build the validate binary for testing
	binaryPath := filepath.Join(t.TempDir(), "validate-test")
	// #nosec G204 -- Test code: building test binary with controlled arguments
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build validate binary: %v\n%s", err, out)
	}

	tests := []struct {
		name       string
		configFile string // relative to ../../internal/config/testdata/
		wantExit   int
		wantStdout string // substring expected in stdout
		wantStderr string // substring expected in stderr
	}{
		{
			name:       "valid minimal config",
			configFile: "../../internal/config/testdata/valid-minimal.yaml",
			wantExit:   0,
			wantStdout: "is valid",
		},
		{
			name:       "invalid unknown key",
			configFile: "../../internal/config/testdata/invalid-unknown-key.yaml",
			wantExit:   1,
			wantStderr: "Configuration error",
		},
		{
			name:       "invalid type mismatch",
			configFile: "../../internal/config/testdata/invalid-type.yaml",
			wantExit:   1,
			wantStderr: "Configuration error",
		},
		{
			name:       "no file flag provided",
			configFile: "",
			wantExit:   2,
			wantStderr: "--file is required",
		},
		{
			name:       "non-existent file",
			configFile: "does-not-exist.yaml",
			wantExit:   1,
			wantStderr: "Configuration error",
		},
	}

	tmpDir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd *exec.Cmd
			if tt.configFile == "" {
				// Test without -f flag
				// #nosec G204 -- Test code: running test binary with controlled path
				cmd = exec.Command(binaryPath)
			} else {
				// #nosec G204 -- Test code: running test binary with controlled arguments
				cmd = exec.Command(binaryPath, "-f", tt.configFile)
			}
			setCISafeEnv(cmd, tmpDir)

			output, err := cmd.CombinedOutput()
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("unexpected error running validate: %v", err)
				}
			}

			// Check exit code
			if exitCode != tt.wantExit {
				t.Errorf("exit code = %d, want %d\nOutput:\n%s", exitCode, tt.wantExit, output)
			}

			// Check stdout/stderr content
			outputStr := string(output)
			if tt.wantStdout != "" && !strings.Contains(outputStr, tt.wantStdout) {
				t.Errorf("output does not contain %q\nGot:\n%s", tt.wantStdout, outputStr)
			}
			if tt.wantStderr != "" && !strings.Contains(outputStr, tt.wantStderr) {
				t.Errorf("output does not contain %q\nGot:\n%s", tt.wantStderr, outputStr)
			}
		})
	}
}

// TestValidateCLI_Version tests the -version flag
func TestValidateCLI_Version(t *testing.T) {
	// Build the validate binary for testing
	binaryPath := filepath.Join(t.TempDir(), "validate-test")
	// #nosec G204 -- Test code: building test binary with controlled arguments
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build validate binary: %v\n%s", err, out)
	}

	// #nosec G204 -- Test code: running test binary with controlled arguments
	cmd := exec.Command(binaryPath, "-version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unexpected error running validate -version: %v", err)
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		t.Error("version output is empty")
	}
	// Version should be "dev" in test builds
	if !strings.Contains(outputStr, "dev") {
		t.Logf("Version output: %s", outputStr)
	}
}

// TestValidateCLI_CuratedSurface tests the curated operator surface (config.example.yaml)
func TestValidateCLI_CuratedSurface(t *testing.T) {
	cfg := "../../config.example.yaml"
	if _, err := os.Stat(cfg); os.IsNotExist(err) {
		t.Skipf("%s not found, skipping", cfg)
	}

	// Build the validate binary for testing
	binaryPath := filepath.Join(t.TempDir(), "validate-test")
	// #nosec G204
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build validate binary: %v\n%s", err, out)
	}

	// #nosec G204
	cmd := exec.Command(binaryPath, "-f", cfg)
	setCISafeEnv(cmd, t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("validate failed for curated surface %s: %v\nOutput:\n%s", cfg, err, output)
	}
	if !strings.Contains(string(output), "is valid") {
		t.Errorf("expected success message, got:\n%s", string(output))
	}
}

// TestValidateCLI_RegistryParity tests the full registry surface (config.generated.example.yaml).
// Warning: This test is expected to fail until VODConfig convergence (ADR-014).
func TestValidateCLI_RegistryParity(t *testing.T) {
	cfg := "../../config.generated.example.yaml"
	if _, err := os.Stat(cfg); os.IsNotExist(err) {
		t.Skipf("%s not found, skipping", cfg)
	}

	// Build the validate binary for testing
	binaryPath := filepath.Join(t.TempDir(), "validate-test")
	// #nosec G204
	buildCmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build validate binary: %v\n%s", err, out)
	}

	// #nosec G204
	cmd := exec.Command(binaryPath, "-f", cfg)
	setCISafeEnv(cmd, t.TempDir())
	output, err := cmd.CombinedOutput()

	// ADR-014: High-Governance Exemption
	// We expect this to pass if FileConfig covers the generated example.
	if err != nil {
		t.Fatalf("validate failed for registry example %s: %v\nOutput:\n%s", cfg, err, output)
	}
	if !strings.Contains(string(output), "is valid") {
		t.Errorf("expected success message, got:\n%s", string(output))
	}
}
