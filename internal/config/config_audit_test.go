// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigAudit_FieldCoverage(t *testing.T) {
	registry, err := GetRegistry()
	require.NoError(t, err)
	cfg := AppConfig{}

	err = registry.ValidateFieldCoverage(cfg)
	if err != nil {
		t.Errorf("Mechanical coverage check failed: %v. Every exported field in AppConfig must be in registry.go", err)
	}
}

func TestConfigAudit_RegistryIntegrity(t *testing.T) {
	registry, err := GetRegistry()
	require.NoError(t, err)

	// 1. All entries must have a profile and status
	for path, entry := range registry.ByPath {
		assert.NotEmpty(t, entry.Profile, "Profile missing for path: %s", path)
		assert.NotEmpty(t, entry.Status, "Status missing for path: %s", path)
	}

	// 2. Field uniqueness check (already panics on buildRegistry, but we can double check logic)
	assert.True(t, len(registry.ByField) > 0)
}

func TestConfigAudit_StrictLoadingFailsOnUnknownKey(t *testing.T) {
	// 1. Create a temporary config file with an unknown key
	tmpDir, err := os.MkdirTemp("", "xg2g-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	configPath := filepath.Join(tmpDir, "invalid.yaml")
	content := `
version: v3
dataDir: /tmp
unknownKeyAtRoot: "should-fail"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	// 2. Use the productive Loader path
	loader := NewLoader(configPath, "test-version")
	_, err = loader.Load()

	// 3. Assert failure
	assert.Error(t, err, "Loader must fail when an unknown YAML key is present")
	assert.Contains(t, strings.ToLower(err.Error()), "strict", "Error should mention strict parsing")
}

func TestConfigAudit_StrictLoadingFailsOnNestedUnknownKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xg2g-audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	configPath := filepath.Join(tmpDir, "invalid-nested.yaml")
	content := `
version: v3
api:
  listenAddr: ":8088"
  shadowKey: "not-allowed"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	loader := NewLoader(configPath, "test-version")
	_, err = loader.Load()

	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "strict")
}

func TestConfigGovernance_ForbiddenCombination_ProxyAware(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "xg2g-gov-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	configPath := filepath.Join(tmpDir, "forbidden.yaml")
	// Case 1: ForceHTTPS=true, TLSEnabled=false, TrustedProxies=empty -> FAIL
	content := `
version: v3
dataDir: /tmp
enigma2:
  baseUrl: http://localhost
tls:
  forceHTTPS: true
  enabled: false
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	loader := NewLoader(configPath, "test-version")
	_, err = loader.Load()
	require.Error(t, err, "Should fail without TrustedProxies")
	assert.Contains(t, err.Error(), "HTTPS_WITHOUT_TLS")

	// Case 2: ForceHTTPS=true, TLSEnabled=false, TrustedProxies=SET -> PASS
	os.Setenv("XG2G_TRUSTED_PROXIES", "127.0.0.1/32")
	defer os.Unsetenv("XG2G_TRUSTED_PROXIES")

	_, err = loader.Load()
	assert.NoError(t, err, "Should pass with TrustedProxies (Proxy-Aware)")
}

func TestConfigAudit_RegistryDefaultsTypes(t *testing.T) {
	registry, err := GetRegistry()
	require.NoError(t, err)
	cfg := AppConfig{}
	cfgType := reflect.TypeOf(cfg)

	for _, entry := range registry.ByField {
		if entry.Default == nil {
			continue
		}

		// Resolve field type
		parts := strings.Split(entry.FieldPath, ".")
		curr := cfgType
		for _, p := range parts {
			f, ok := curr.FieldByName(p)
			require.True(t, ok, "Field %s not found in struct", p)
			curr = f.Type
		}

		// Handle Pointers
		if curr.Kind() == reflect.Ptr {
			curr = curr.Elem()
		}

		defaultVal := reflect.ValueOf(entry.Default)
		assert.Equal(t, curr.Kind(), defaultVal.Type().Kind(),
			"Default value type mismatch for field %s: expected %v, got %v",
			entry.FieldPath, curr.Kind(), defaultVal.Type().Kind())
	}
}

// Helper to reflect deep fields (e.g. Engine.Mode)
func getFieldValue(t *testing.T, v reflect.Value, path string) (reflect.Value, bool) {
	parts := strings.Split(path, ".")
	curr := v
	for _, p := range parts {
		if curr.Kind() == reflect.Ptr {
			curr = curr.Elem()
		}
		f := curr.FieldByName(p)
		if !f.IsValid() {
			return reflect.Value{}, false
		}
		curr = f
	}
	return curr, true
}

// Gate: Ensure Registry Defaults are the Single Source of Truth for runtime configuration
func TestConfigAudit_RegistryTruth_Defaults(t *testing.T) {
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	// 1. Load config only with defaults (no file, no env)
	loader := NewLoader("", "vTest")
	cfg, err := loader.Load()
	// Validation might fail because defaults alone might not be valid (e.g. missing required URLs)
	// But we want to audit that defaults were applied.
	if err != nil {
		t.Logf("Loader validation error ignored for defaults audit: %v", err)
	}

	registry, err := GetRegistry()
	require.NoError(t, err)
	cfgVal := reflect.ValueOf(cfg)

	for _, entry := range registry.ByField {
		if entry.Default == nil {
			continue
		}
		if entry.Status == StatusInternal {
			continue
		}

		// Read actual value from cfg
		val, found := getFieldValue(t, cfgVal, entry.FieldPath)
		require.True(t, found, "Field %s not found in AppConfig", entry.FieldPath)

		// Dereference pointer if default is value type but field is pointer
		if val.Kind() == reflect.Ptr && !val.IsNil() {
			val = val.Elem()
		}

		// Compare actual vs expected default
		// We use strict equality to verify mechanical application
		assert.EqualValues(t, entry.Default, val.Interface(),
			"Runtime default mismatch for %s (Registry says %v, Runtime has %v)",
			entry.FieldPath, entry.Default, val.Interface())
	}
}

// TestConfigAudit_RegistryTruth_EnvKeys mechanically proves that every ENV key defined in the Registry
// is actually read/consumed by the Loader.
// Implementation: It checks the Loader.ConsumedEnvKeys map, which is populated by l.env* wrappers.
// This meets Team-Red's requirement for a stable "PASS = code reads the key" gate.
func TestConfigAudit_RegistryTruth_EnvKeys(t *testing.T) {
	// 1. Setup minimal environment for successful load
	// We need to set these to avoid "no tuner slots" critical error or other blockers
	os.Setenv("XG2G_ENGINE_ENABLED", "false") // Disable engine to skip auto-discovery
	defer os.Unsetenv("XG2G_ENGINE_ENABLED")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	// 2. Load Config
	l := NewLoader("", "vTest")
	// We expect errors because we aren't providing a valid config, just checking consumption
	_, _ = l.Load()

	// 3. Verify Consumption via Tracker
	registry, err := GetRegistry()
	require.NoError(t, err)

	for _, entry := range registry.ByField {
		if entry.Env == "" {
			continue
		}

		// Skip Candidates/Zombies - only Active keys must be consumed
		if entry.Status != StatusActive {
			continue
		}

		// ASSERTION: The key MUST have been accessed by the Loader
		if _, consumed := l.ConsumedEnvKeys[entry.Env]; !consumed {
			t.Errorf("Registry Key %s (Field: %s) was NOT consumed by Loader. Missing l.env*(%q) call in mergeEnvConfig?",
				entry.Env, entry.FieldPath, entry.Env)
		}
	}
}

// TestConfigAudit_PointerDefaults_PreserveZeroValues verifies that ApplyDefaults
// respects existing values for pointer fields (e.g. explicitly set to false/0)
// and only applies defaults when nil.
func TestConfigAudit_PointerDefaults_PreserveZeroValues(t *testing.T) {
	// We need a field in AppConfig that is a pointer.
	// HDHR.Enabled is *bool.
	fieldPath := "HDHR.Enabled"

	// Inject a test default into Registry
	reg, err := GetRegistry()
	require.NoError(t, err)
	original, exists := reg.ByField[fieldPath]

	// Set default to TRUE
	reg.ByField[fieldPath] = ConfigEntry{
		FieldPath: fieldPath,
		Default:   true,
		Status:    StatusActive,
	}
	defer func() {
		if exists {
			reg.ByField[fieldPath] = original
		} else {
			delete(reg.ByField, fieldPath)
		}
	}()

	t.Run("ApplyDefaultToNil", func(t *testing.T) {
		cfg := AppConfig{}
		// HDHR.Enabled is nil initially
		assert.Nil(t, cfg.HDHR.Enabled)

		reg.ApplyDefaults(&cfg)

		require.NotNil(t, cfg.HDHR.Enabled)
		assert.True(t, *cfg.HDHR.Enabled, "Should set default true to nil pointer")
	})

	t.Run("PreserveExplicitFalse", func(t *testing.T) {
		cfg := AppConfig{}
		val := false
		cfg.HDHR.Enabled = &val // Explicitly set to false

		reg.ApplyDefaults(&cfg)

		require.NotNil(t, cfg.HDHR.Enabled)
		assert.False(t, *cfg.HDHR.Enabled, "Should preserved explicit false")
	})
}

func TestConfigGovernance_DeprecationFail(t *testing.T) {
	// Set an environment variable marked as "fail" in docs/deprecations.json
	os.Setenv("XG2G_HTTP_ENABLE_HTTP2", "true")
	defer os.Unsetenv("XG2G_HTTP_ENABLE_HTTP2")

	// Provide minimal valid config to avoid unrelated validation errors
	os.Setenv("XG2G_OWI_BASE", "http://localhost")
	defer os.Unsetenv("XG2G_OWI_BASE")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	loader := NewLoader("", "test-version")
	_, err := loader.Load()

	require.Error(t, err, "Should fail due to 'fail' phase deprecation")
	assert.Contains(t, err.Error(), "XG2G_HTTP_ENABLE_HTTP2")
	assert.Contains(t, err.Error(), "removed")
}

func TestConfigGovernance_DeprecationWarn(t *testing.T) {
	// Set an environment variable marked as "warn" in docs/deprecations.json
	os.Setenv("XG2G_STREAM_PORT", "8002")
	defer os.Unsetenv("XG2G_STREAM_PORT")

	// Provide minimal valid config
	os.Setenv("XG2G_OWI_BASE", "http://localhost")
	defer os.Unsetenv("XG2G_OWI_BASE")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())

	loader := NewLoader("", "test-version")
	_, err := loader.Load()

	// Loading should succeed (warn only)
	assert.NoError(t, err)
}
