// SPDX-License-Identifier: MIT
package proxy

import (
	"os"
	"testing"
)

// TestEnvConfig_TableDriven tests environment variable configuration with comprehensive scenarios
func TestEnvConfig_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantEnabled bool
		wantAddr    string
		wantTarget  string
	}{
		{
			name: "all_enabled_custom_values",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "true",
				"XG2G_PROXY_PORT":          "19000",
				"XG2G_PROXY_TARGET":        "http://10.10.10.10:17999",
			},
			wantEnabled: true,
			wantAddr:    ":19000",
			wantTarget:  "http://10.10.10.10:17999",
		},
		{
			name: "enabled_with_defaults",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "true",
				"XG2G_PROXY_TARGET":        "http://example.com:17999",
			},
			wantEnabled: true,
			wantAddr:    ":18000", // Default
			wantTarget:  "http://example.com:17999",
		},
		{
			name: "disabled",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "false",
				"XG2G_PROXY_PORT":          "19000",
				"XG2G_PROXY_TARGET":        "http://example.com:17999",
			},
			wantEnabled: false,
			wantAddr:    ":19000",
			wantTarget:  "http://example.com:17999",
		},
		{
			name:        "all_defaults",
			envVars:     map[string]string{},
			wantEnabled: false,
			wantAddr:    ":18000",
			wantTarget:  "",
		},
		{
			name: "enabled_1_string",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "1",
				"XG2G_PROXY_PORT":          "20000",
				"XG2G_PROXY_TARGET":        "http://localhost:17999",
			},
			wantEnabled: true,
			wantAddr:    ":20000",
			wantTarget:  "http://localhost:17999",
		},
		{
			name: "enabled_TRUE_uppercase",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "TRUE",
				"XG2G_PROXY_TARGET":        "http://192.168.1.100:17999",
			},
			wantEnabled: true,
			wantAddr:    ":18000",
			wantTarget:  "http://192.168.1.100:17999",
		},
		{
			name: "disabled_0_string",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "0",
			},
			wantEnabled: false,
			wantAddr:    ":18000",
			wantTarget:  "",
		},
		{
			name: "invalid_enabled_value",
			envVars: map[string]string{
				"XG2G_ENABLE_STREAM_PROXY": "maybe",
			},
			wantEnabled: false, // Invalid values default to false
			wantAddr:    ":18000",
			wantTarget:  "",
		},
		{
			name: "custom_port_only",
			envVars: map[string]string{
				"XG2G_PROXY_PORT": "21000",
			},
			wantEnabled: false,
			wantAddr:    ":21000",
			wantTarget:  "",
		},
		{
			name: "target_only",
			envVars: map[string]string{
				"XG2G_PROXY_TARGET": "http://stream.example.com:8080",
			},
			wantEnabled: false,
			wantAddr:    ":18000",
			wantTarget:  "http://stream.example.com:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all proxy-related env vars first
			_ = os.Unsetenv("XG2G_ENABLE_STREAM_PROXY")
			_ = os.Unsetenv("XG2G_PROXY_PORT")
			_ = os.Unsetenv("XG2G_PROXY_TARGET")

			// Set test env vars
			for k, v := range tt.envVars {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("Failed to set %s: %v", k, err)
				}
			}

			// Clean up after test
			defer func() {
				for k := range tt.envVars {
					_ = os.Unsetenv(k)
				}
			}()

			// Test IsEnabled
			if got := IsEnabled(); got != tt.wantEnabled {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.wantEnabled)
			}

			// Test GetListenAddr
			if got := GetListenAddr(); got != tt.wantAddr {
				t.Errorf("GetListenAddr() = %q, want %q", got, tt.wantAddr)
			}

			// Test GetTargetURL
			if got := GetTargetURL(); got != tt.wantTarget {
				t.Errorf("GetTargetURL() = %q, want %q", got, tt.wantTarget)
			}
		})
	}
}

// TestEnvConfig_PortFormatting tests various port format scenarios
func TestEnvConfig_PortFormatting(t *testing.T) {
	tests := []struct {
		name     string
		portVal  string
		wantAddr string
	}{
		{
			name:     "numeric_only",
			portVal:  "9000",
			wantAddr: ":9000",
		},
		{
			name:     "with_colon_prefix",
			portVal:  ":9001", // Should still work
			wantAddr: "::9001",
		},
		{
			name:     "empty_string",
			portVal:  "",
			wantAddr: ":18000", // Default
		},
		{
			name:     "zero_port",
			portVal:  "0",
			wantAddr: ":0", // Random port
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Unsetenv("XG2G_PROXY_PORT")

			if tt.portVal != "" {
				if err := os.Setenv("XG2G_PROXY_PORT", tt.portVal); err != nil {
					t.Fatalf("Setenv failed: %v", err)
				}
				defer func() { _ = os.Unsetenv("XG2G_PROXY_PORT") }()
			}

			if got := GetListenAddr(); got != tt.wantAddr {
				t.Errorf("GetListenAddr() = %q, want %q", got, tt.wantAddr)
			}
		})
	}
}

// TestEnvConfig_TargetURLVariations tests various target URL formats
func TestEnvConfig_TargetURLVariations(t *testing.T) {
	tests := []struct {
		name       string
		targetVal  string
		wantTarget string
	}{
		{
			name:       "http_with_port",
			targetVal:  "http://example.com:17999",
			wantTarget: "http://example.com:17999",
		},
		{
			name:       "http_without_port",
			targetVal:  "http://example.com",
			wantTarget: "http://example.com",
		},
		{
			name:       "https",
			targetVal:  "https://secure.example.com:443",
			wantTarget: "https://secure.example.com:443",
		},
		{
			name:       "ip_address",
			targetVal:  "http://192.168.1.1:8080",
			wantTarget: "http://192.168.1.1:8080",
		},
		{
			name:       "localhost",
			targetVal:  "http://localhost:17999",
			wantTarget: "http://localhost:17999",
		},
		{
			name:       "with_path",
			targetVal:  "http://example.com:17999/path",
			wantTarget: "http://example.com:17999/path",
		},
		{
			name:       "empty",
			targetVal:  "",
			wantTarget: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Unsetenv("XG2G_PROXY_TARGET")

			if tt.targetVal != "" {
				if err := os.Setenv("XG2G_PROXY_TARGET", tt.targetVal); err != nil {
					t.Fatalf("Setenv failed: %v", err)
				}
				defer func() { _ = os.Unsetenv("XG2G_PROXY_TARGET") }()
			}

			if got := GetTargetURL(); got != tt.wantTarget {
				t.Errorf("GetTargetURL() = %q, want %q", got, tt.wantTarget)
			}
		})
	}
}

// TestEnvConfig_BooleanParsing tests various boolean value formats for IsEnabled
func TestEnvConfig_BooleanParsing(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantVal bool
	}{
		{"true_lowercase", "true", true},
		{"TRUE_uppercase", "TRUE", true},
		{"True_mixedcase", "True", true},
		{"1_numeric", "1", true},
		{"false_lowercase", "false", false},
		{"FALSE_uppercase", "FALSE", false},
		{"0_numeric", "0", false},
		{"empty_string", "", false},
		{"invalid_yes", "yes", false},
		{"invalid_no", "no", false},
		{"invalid_on", "on", false},
		{"invalid_off", "off", false},
		{"invalid_random", "random", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Unsetenv("XG2G_ENABLE_STREAM_PROXY")

			if tt.value != "" {
				if err := os.Setenv("XG2G_ENABLE_STREAM_PROXY", tt.value); err != nil {
					t.Fatalf("Setenv failed: %v", err)
				}
				defer func() { _ = os.Unsetenv("XG2G_ENABLE_STREAM_PROXY") }()
			}

			if got := IsEnabled(); got != tt.wantVal {
				t.Errorf("IsEnabled() with value %q = %v, want %v", tt.value, got, tt.wantVal)
			}
		})
	}
}
