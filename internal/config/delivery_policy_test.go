package config_test

import (
	"os"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

// setupEnv sets up the minimum required environment variables for validation to pass
func setupEnv(t *testing.T) func() {
	_ = os.Setenv("XG2G_OWI_BASE", "http://test-enigma2-host")
	_ = os.Setenv("XG2G_ENGINE_ENABLED", "false")
	return func() {
		_ = os.Unsetenv("XG2G_OWI_BASE")
		_ = os.Unsetenv("XG2G_ENGINE_ENABLED")
	}
}

// TestDeliveryPolicyDefaults verifies that the default delivery_policy is "universal"
func TestDeliveryPolicyDefaults(t *testing.T) {
	cleanup := setupEnv(t)
	defer cleanup()

	loader := config.NewLoader("", "test-version")

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Streaming.DeliveryPolicy != "universal" {
		t.Errorf("Expected delivery_policy='universal', got '%s'", cfg.Streaming.DeliveryPolicy)
	}
}

// TestDeliveryPolicyValidation_Universal verifies that "universal" is accepted
func TestDeliveryPolicyValidation_Universal(t *testing.T) {
	cleanup := setupEnv(t)
	defer cleanup()

	loader := config.NewLoader("", "test-version")

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Explicitly set to universal (should pass validation)
	cfg.Streaming.DeliveryPolicy = "universal"

	if err := config.Validate(cfg); err != nil {
		t.Errorf("Validate() failed for delivery_policy='universal': %v", err)
	}
}

// TestDeliveryPolicyValidation_RejectsInvalid verifies that non-universal values are rejected
func TestDeliveryPolicyValidation_RejectsInvalid(t *testing.T) {
	cleanup := setupEnv(t)
	defer cleanup()

	testCases := []struct {
		name   string
		policy string
	}{
		{"auto", "auto"},
		{"safari", "safari"},
		{"safari_hevc_hw", "safari_hevc_hw"},
		{"empty", ""},
		{"invalid", "invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			loader := config.NewLoader("", "test-version")

			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			// Override with invalid policy
			cfg.Streaming.DeliveryPolicy = tc.policy

			err = config.Validate(cfg)
			if err == nil {
				t.Errorf("Validate() should have failed for delivery_policy='%s'", tc.policy)
			}
		})
	}
}

// TestDeprecatedEnvVarFailStart verifies that XG2G_STREAM_PROFILE causes fail-start
func TestDeprecatedEnvVarFailStart(t *testing.T) {
	// Set the deprecated env var
	_ = os.Setenv("XG2G_STREAM_PROFILE", "auto")
	defer func() { _ = os.Unsetenv("XG2G_STREAM_PROFILE") }()

	loader := config.NewLoader("", "test-version")

	_, err := loader.Load()
	if err == nil {
		t.Fatal("Load() should have failed when XG2G_STREAM_PROFILE is set")
	}

	expectedMsg := "XG2G_STREAM_PROFILE removed. Use XG2G_STREAMING_POLICY=universal (ADR-00X)"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

// TestNewEnvVarWorks verifies that XG2G_STREAMING_POLICY works correctly
func TestNewEnvVarWorks(t *testing.T) {
	cleanup := setupEnv(t)
	defer cleanup()

	_ = os.Setenv("XG2G_STREAMING_POLICY", "universal")
	defer func() { _ = os.Unsetenv("XG2G_STREAMING_POLICY") }()

	loader := config.NewLoader("", "test-version")

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.Streaming.DeliveryPolicy != "universal" {
		t.Errorf("Expected delivery_policy='universal', got '%s'", cfg.Streaming.DeliveryPolicy)
	}
}
