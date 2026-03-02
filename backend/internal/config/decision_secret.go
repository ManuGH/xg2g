// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import (
	"os"
	"strings"
)

// DecisionSecretFromEnv reads XG2G_DECISION_SECRET from process environment.
// Returns nil if the variable is unset or whitespace-only.
func DecisionSecretFromEnv() []byte {
	value, ok := os.LookupEnv("XG2G_DECISION_SECRET")
	if !ok {
		return nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []byte(value)
}
