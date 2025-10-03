// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"strings"
	"testing"
)

func TestResolveOWISettingsValidation(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantErr      bool
		wantContains []string
	}{
		{
			name: "negative_timeout",
			env: map[string]string{
				"XG2G_OWI_TIMEOUT_MS": "-1000",
			},
			wantErr:      true,
			wantContains: []string{"timeout", "out of range"},
		},
		{
			name: "negative_retries",
			env: map[string]string{
				"XG2G_OWI_RETRIES": "-1",
			},
			wantErr:      true,
			wantContains: []string{"retries", "out of range"},
		},
		{
			name: "negative_backoff",
			env: map[string]string{
				"XG2G_OWI_BACKOFF_MS": "-500",
			},
			wantErr:      true,
			wantContains: []string{"backoff", "out of range"},
		},
		{
			name: "excessive_timeout",
			env: map[string]string{
				"XG2G_OWI_TIMEOUT_MS": "120000", // 2 minutes, over 60s max
			},
			wantErr:      true,
			wantContains: []string{"timeout", "out of range"},
		},
		{
			name: "backoff_exceeds_max",
			env: map[string]string{
				"XG2G_OWI_BACKOFF_MS": "40000", // 40s > 30s max
			},
			wantErr:      true,
			wantContains: []string{"backoff", "out of range"},
		},
		{
			name: "invalid_number_format",
			env: map[string]string{
				"XG2G_OWI_TIMEOUT_MS": "not-a-number",
			},
			wantErr:      true,
			wantContains: []string{"invalid timeout", "invalid syntax"},
		},
		{
			name:    "defaults_are_valid",
			env:     map[string]string{},
			wantErr: false,
		},
		{
			name: "valid_custom_values",
			env: map[string]string{
				"XG2G_OWI_TIMEOUT_MS":     "15000",
				"XG2G_OWI_RETRIES":        "5",
				"XG2G_OWI_BACKOFF_MS":     "300",
				"XG2G_OWI_MAX_BACKOFF_MS": "3000",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment first
			clearOWIEnv()

			// Set test environment
			for key, value := range tt.env {
				if err := os.Setenv(key, value); err != nil {
					t.Fatalf("failed to set env %s: %v", key, err)
				}
			}

			// Clean up after test
			defer clearOWIEnv()

			_, _, _, _, err := resolveOWISettings()

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %v, got nil", tt.wantContains)
				}
				lowerErr := strings.ToLower(err.Error())
				for _, sub := range tt.wantContains {
					if !strings.Contains(lowerErr, strings.ToLower(sub)) {
						t.Fatalf("expected error to contain %q, got %v", sub, err)
					}
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func clearOWIEnv() {
	_ = os.Unsetenv("XG2G_OWI_TIMEOUT_MS")
	_ = os.Unsetenv("XG2G_OWI_RETRIES")
	_ = os.Unsetenv("XG2G_OWI_BACKOFF_MS")
	_ = os.Unsetenv("XG2G_OWI_MAX_BACKOFF_MS")
}
