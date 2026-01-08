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

	// Mode "inherit": validate E2 credentials are also paired
	if mode == "inherit" {
		hasUser := strings.TrimSpace(cfg.Enigma2.Username) != ""
		hasPass := strings.TrimSpace(cfg.Enigma2.Password) != ""

		if hasUser != hasPass {
			return fmt.Errorf("Enigma2 credentials must be user+pass pair (both XG2G_E2_USER and XG2G_E2_PASS required or inherited)")
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
		// Credentials already set via merge logic or defaults.
		// If both are empty, that's allowed (no auth).
		// If only one set, validation already caught it.

	case "none":
		// Wipe E2 credentials (validation already ensured they weren't set)
		cfg.Enigma2.Username = ""
		cfg.Enigma2.Password = ""

	case "explicit":
		// No-op: E2 credentials remain as explicitly set
	}
}
