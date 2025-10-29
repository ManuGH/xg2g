// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"testing"
	"time"
)

func TestParseString(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		envSet       bool
		want         string
	}{
		{
			name:         "environment variable set",
			key:          "TEST_STRING",
			defaultValue: "default",
			envValue:     "from-env",
			envSet:       true,
			want:         "from-env",
		},
		{
			name:         "environment variable not set",
			key:          "TEST_STRING_UNSET",
			defaultValue: "default",
			envSet:       false,
			want:         "default",
		},
		{
			name:         "environment variable empty string",
			key:          "TEST_STRING_EMPTY",
			defaultValue: "default",
			envValue:     "",
			envSet:       true,
			want:         "default",
		},
		{
			name:         "sensitive variable (password)",
			key:          "TEST_PASSWORD",
			defaultValue: "default",
			envValue:     "secret123",
			envSet:       true,
			want:         "secret123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.envSet {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			// Test
			got := ParseString(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ParseString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		envSet       bool
		want         int
	}{
		{
			name:         "valid integer",
			key:          "TEST_INT",
			defaultValue: 42,
			envValue:     "100",
			envSet:       true,
			want:         100,
		},
		{
			name:         "invalid integer",
			key:          "TEST_INT_INVALID",
			defaultValue: 42,
			envValue:     "not-a-number",
			envSet:       true,
			want:         42, // falls back to default
		},
		{
			name:         "empty string",
			key:          "TEST_INT_EMPTY",
			defaultValue: 42,
			envValue:     "",
			envSet:       true,
			want:         42,
		},
		{
			name:         "not set",
			key:          "TEST_INT_UNSET",
			defaultValue: 42,
			envSet:       false,
			want:         42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.envSet {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			// Test
			got := ParseInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ParseInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue time.Duration
		envValue     string
		envSet       bool
		want         time.Duration
	}{
		{
			name:         "valid duration",
			key:          "TEST_DURATION",
			defaultValue: 5 * time.Second,
			envValue:     "10s",
			envSet:       true,
			want:         10 * time.Second,
		},
		{
			name:         "complex duration",
			key:          "TEST_DURATION_COMPLEX",
			defaultValue: 5 * time.Second,
			envValue:     "1h30m45s",
			envSet:       true,
			want:         1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:         "invalid duration",
			key:          "TEST_DURATION_INVALID",
			defaultValue: 5 * time.Second,
			envValue:     "not-a-duration",
			envSet:       true,
			want:         5 * time.Second, // falls back to default
		},
		{
			name:         "empty string",
			key:          "TEST_DURATION_EMPTY",
			defaultValue: 5 * time.Second,
			envValue:     "",
			envSet:       true,
			want:         5 * time.Second,
		},
		{
			name:         "not set",
			key:          "TEST_DURATION_UNSET",
			defaultValue: 5 * time.Second,
			envSet:       false,
			want:         5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.envSet {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			// Test
			got := ParseDuration(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ParseDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		envSet       bool
		want         bool
	}{
		{
			name:         "true string",
			key:          "TEST_BOOL_TRUE",
			defaultValue: false,
			envValue:     "true",
			envSet:       true,
			want:         true,
		},
		{
			name:         "TRUE uppercase",
			key:          "TEST_BOOL_TRUE_UPPER",
			defaultValue: false,
			envValue:     "TRUE",
			envSet:       true,
			want:         true,
		},
		{
			name:         "1 as true",
			key:          "TEST_BOOL_1",
			defaultValue: false,
			envValue:     "1",
			envSet:       true,
			want:         true,
		},
		{
			name:         "yes as true",
			key:          "TEST_BOOL_YES",
			defaultValue: false,
			envValue:     "yes",
			envSet:       true,
			want:         true,
		},
		{
			name:         "false string",
			key:          "TEST_BOOL_FALSE",
			defaultValue: true,
			envValue:     "false",
			envSet:       true,
			want:         false,
		},
		{
			name:         "0 as false",
			key:          "TEST_BOOL_0",
			defaultValue: true,
			envValue:     "0",
			envSet:       true,
			want:         false,
		},
		{
			name:         "no as false",
			key:          "TEST_BOOL_NO",
			defaultValue: true,
			envValue:     "no",
			envSet:       true,
			want:         false,
		},
		{
			name:         "invalid boolean",
			key:          "TEST_BOOL_INVALID",
			defaultValue: true,
			envValue:     "maybe",
			envSet:       true,
			want:         true, // falls back to default
		},
		{
			name:         "empty string",
			key:          "TEST_BOOL_EMPTY",
			defaultValue: true,
			envValue:     "",
			envSet:       true,
			want:         true,
		},
		{
			name:         "not set",
			key:          "TEST_BOOL_UNSET",
			defaultValue: false,
			envSet:       false,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.envSet {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			// Test
			got := ParseBool(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("ParseBool() = %v, want %v", got, tt.want)
			}
		})
	}
}
