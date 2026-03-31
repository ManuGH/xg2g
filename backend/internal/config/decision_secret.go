// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"strings"
)

const (
	decisionSecretEnvKey       = "XG2G_DECISION_SECRET"          // #nosec G101 -- environment variable key, not a credential value.
	decisionSecretLegacyEnvKey = "XG2G_PLAYBACK_DECISION_SECRET" // #nosec G101 -- environment variable key, not a credential value.
)

// DecisionSecretFromEnv reads the canonical playback decision secret from process environment.
// It prefers XG2G_DECISION_SECRET and falls back to the legacy XG2G_PLAYBACK_DECISION_SECRET.
// Returns nil if both variables are unset or whitespace-only.
func DecisionSecretFromEnv() []byte {
	return DecisionSecretFromLookup(nil)
}

func DecisionSecretFromLookup(lookup envLookupFunc) []byte {
	if lookup == nil {
		lookup = currentProcessLookupEnv()
	}

	if value, ok := decisionSecretValueFromLookup(lookup); ok {
		return []byte(value)
	}
	return nil
}

func decisionSecretValueFromLookup(lookup envLookupFunc) (string, bool) {
	if value, ok := lookup(decisionSecretEnvKey); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value, true
		}
	}
	if value, ok := lookup(decisionSecretLegacyEnvKey); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value, true
		}
	}
	return "", false
}
