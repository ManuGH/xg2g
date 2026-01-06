// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"fmt"
	"strings"
)

// validateE2AuthModeInputs validates E2 auth mode configuration before resolution.
// This catches invalid configurations that would be hidden by resolution (e.g., "none" with creds set).
// Canonicalizes AuthMode (lowercase, trimmed) for consistent downstream usage.
func validateE2AuthModeInputs(cfg *AppConfig) error {
	// Canonicalize once (lowercase, trim)
	cfg.Enigma2.AuthMode = strings.ToLower(strings.TrimSpace(cfg.Enigma2.AuthMode))
	mode := cfg.Enigma2.AuthMode

	// Enum check
	if mode != "inherit" && mode != "none" && mode != "explicit" {
		return fmt.Errorf("invalid XG2G_E2_AUTH_MODE: %q (must be: inherit, none, explicit)", mode)
	}

	hasE2User := strings.TrimSpace(cfg.Enigma2.Username) != ""
	hasE2Pass := strings.TrimSpace(cfg.Enigma2.Password) != ""

	// Mode "none": forbid any E2 credentials
	if mode == "none" {
		if hasE2User || hasE2Pass {
			return fmt.Errorf("XG2G_E2_AUTH_MODE=none forbids E2 credentials (XG2G_E2_USER/XG2G_E2_PASS must not be set)")
		}
	}

	// Mode "inherit" or "explicit": require pair consistency
	if mode == "inherit" || mode == "explicit" {
		if hasE2User != hasE2Pass {
			return fmt.Errorf("E2 credentials must be set as user+pass pair (both XG2G_E2_USER and XG2G_E2_PASS required)")
		}
	}

	// Mode "inherit": validate OWI credentials are also paired
	if mode == "inherit" {
		hasOWIUser := strings.TrimSpace(cfg.OWIUsername) != ""
		hasOWIPass := strings.TrimSpace(cfg.OWIPassword) != ""

		if hasOWIUser != hasOWIPass {
			return fmt.Errorf("OWI credentials must be user+pass pair for XG2G_E2_AUTH_MODE=inherit (both XG2G_OWI_USER and XG2G_OWI_PASS required)")
		}
	}

	return nil
}

// resolveE2AuthMode applies E2 authentication mode resolution logic.
// Must be called after validateE2AuthModeInputs and before final Validate.
// AuthMode is already canonicalized by validateE2AuthModeInputs.
func resolveE2AuthMode(cfg *AppConfig) {
	mode := cfg.Enigma2.AuthMode // Already normalized

	switch mode {
	case "inherit":
		// Copy OWI credentials to E2 if E2 is empty
		if cfg.Enigma2.Username == "" && cfg.Enigma2.Password == "" {
			cfg.Enigma2.Username = cfg.OWIUsername
			cfg.Enigma2.Password = cfg.OWIPassword
		}
		// If E2 already has creds, leave them unchanged

	case "none":
		// Wipe E2 credentials (validation already ensured they weren't set)
		cfg.Enigma2.Username = ""
		cfg.Enigma2.Password = ""

	case "explicit":
		// No-op: E2 credentials remain as explicitly set
	}
}
