// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"testing"
)

// TestE2AuthMode_Inherit tests "inherit" mode behavior
func TestE2AuthMode_Inherit(t *testing.T) {
	tests := []struct {
		name         string
		owiUser      string
		owiPass      string
		e2User       string
		e2Pass       string
		expectE2User string
		expectE2Pass string
		shouldFail   bool
	}{
		{
			name:         "inherit: OWI set, E2 empty => E2 becomes OWI",
			owiUser:      "owiuser",
			owiPass:      "owipass",
			e2User:       "",
			e2Pass:       "",
			expectE2User: "owiuser",
			expectE2Pass: "owipass",
			shouldFail:   false,
		},
		{
			name:         "inherit: OWI set, E2 set => E2 unchanged",
			owiUser:      "owiuser",
			owiPass:      "owipass",
			e2User:       "e2user",
			e2Pass:       "e2pass",
			expectE2User: "e2user",
			expectE2Pass: "e2pass",
			shouldFail:   false,
		},
		{
			name:       "inherit: OWI partial => fail",
			owiUser:    "owiuser",
			owiPass:    "",
			e2User:     "",
			e2Pass:     "",
			shouldFail: true,
		},
		{
			name:       "inherit: E2 partial => fail",
			owiUser:    "owiuser",
			owiPass:    "owipass",
			e2User:     "e2user",
			e2Pass:     "",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				OWIUsername: tt.owiUser,
				OWIPassword: tt.owiPass,
				Enigma2: Enigma2Settings{
					AuthMode: "inherit",
					Username: tt.e2User,
					Password: tt.e2Pass,
				},
				DataDir:    "/tmp",
				StreamPort: 8001,
			}

			// Validate inputs
			err := validateE2AuthModeInputs(&cfg)
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
				return
			}

			// Resolve
			resolveE2AuthMode(&cfg)

			// Check results
			if cfg.Enigma2.Username != tt.expectE2User {
				t.Errorf("expected E2 username %q, got %q", tt.expectE2User, cfg.Enigma2.Username)
			}
			if cfg.Enigma2.Password != tt.expectE2Pass {
				t.Errorf("expected E2 password %q, got %q", tt.expectE2Pass, cfg.Enigma2.Password)
			}
		})
	}
}

// TestE2AuthMode_None tests "none" mode behavior
func TestE2AuthMode_None(t *testing.T) {
	tests := []struct {
		name       string
		owiUser    string
		owiPass    string
		e2User     string
		e2Pass     string
		shouldFail bool
	}{
		{
			name:       "none: OWI set, E2 empty => E2 stays empty",
			owiUser:    "owiuser",
			owiPass:    "owipass",
			e2User:     "",
			e2Pass:     "",
			shouldFail: false,
		},
		{
			name:       "none: E2 set => fail",
			owiUser:    "owiuser",
			owiPass:    "owipass",
			e2User:     "e2user",
			e2Pass:     "e2pass",
			shouldFail: true,
		},
		{
			name:       "none: E2 partial => fail",
			owiUser:    "",
			owiPass:    "",
			e2User:     "e2user",
			e2Pass:     "",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				OWIUsername: tt.owiUser,
				OWIPassword: tt.owiPass,
				Enigma2: Enigma2Settings{
					AuthMode: "none",
					Username: tt.e2User,
					Password: tt.e2Pass,
				},
				DataDir:    "/tmp",
				StreamPort: 8001,
			}

			// Validate inputs
			err := validateE2AuthModeInputs(&cfg)
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
				return
			}

			// Resolve
			resolveE2AuthMode(&cfg)

			// Check results: E2 should always be empty
			if cfg.Enigma2.Username != "" {
				t.Errorf("expected E2 username to be empty, got %q", cfg.Enigma2.Username)
			}
			if cfg.Enigma2.Password != "" {
				t.Errorf("expected E2 password to be empty, got %q", cfg.Enigma2.Password)
			}
		})
	}
}

// TestE2AuthMode_Explicit tests "explicit" mode behavior
func TestE2AuthMode_Explicit(t *testing.T) {
	tests := []struct {
		name       string
		e2User     string
		e2Pass     string
		shouldFail bool
	}{
		{
			name:       "explicit: empty => ok",
			e2User:     "",
			e2Pass:     "",
			shouldFail: false,
		},
		{
			name:       "explicit: both set => ok",
			e2User:     "e2user",
			e2Pass:     "e2pass",
			shouldFail: false,
		},
		{
			name:       "explicit: partial => fail",
			e2User:     "e2user",
			e2Pass:     "",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				Enigma2: Enigma2Settings{
					AuthMode: "explicit",
					Username: tt.e2User,
					Password: tt.e2Pass,
				},
				DataDir:    "/tmp",
				StreamPort: 8001,
			}

			// Validate inputs
			err := validateE2AuthModeInputs(&cfg)
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
				return
			}

			// Resolve
			resolveE2AuthMode(&cfg)

			// Check results: E2 should remain unchanged
			if cfg.Enigma2.Username != tt.e2User {
				t.Errorf("expected E2 username %q, got %q", tt.e2User, cfg.Enigma2.Username)
			}
			if cfg.Enigma2.Password != tt.e2Pass {
				t.Errorf("expected E2 password %q, got %q", tt.e2Pass, cfg.Enigma2.Password)
			}
		})
	}
}

// TestE2AuthMode_InvalidEnum tests invalid enum values
func TestE2AuthMode_InvalidEnum(t *testing.T) {
	cfg := AppConfig{
		Enigma2: Enigma2Settings{
			AuthMode: "invalid",
		},
		DataDir:    "/tmp",
		StreamPort: 8001,
	}

	err := validateE2AuthModeInputs(&cfg)
	if err == nil {
		t.Errorf("expected validation error for invalid enum, got nil")
	}
}

// TestE2AuthMode_Normalization tests case/whitespace normalization
func TestE2AuthMode_Normalization(t *testing.T) {
	tests := []struct {
		name       string
		authMode   string
		expectMode string
	}{
		{"uppercase", "INHERIT", "inherit"},
		{"mixed case", "InHerIt", "inherit"},
		{"whitespace", " none ", "none"},
		{"tabs", "\texplicit\t", "explicit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := AppConfig{
				Enigma2: Enigma2Settings{
					AuthMode: tt.authMode,
				},
				DataDir:    "/tmp",
				StreamPort: 8001,
			}

			err := validateE2AuthModeInputs(&cfg)
			if err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}

			if cfg.Enigma2.AuthMode != tt.expectMode {
				t.Errorf("expected normalized mode %q, got %q", tt.expectMode, cfg.Enigma2.AuthMode)
			}
		})
	}
}
