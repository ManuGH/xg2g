package policy

import (
	"fmt"
	"strings"
)

// LoadConfig parses and validates the XG2G_POLICY_PREEMPTION_MODE configuration value once at bootstrap.
// Allowed values for Step E2: "disabled" (or empty string) and "audit-only".
// Values "enforce" or unrecognized strings return ErrInvalidPreemptionMode.
func LoadConfig(envValue string) (Config, error) {
	trimmed := strings.TrimSpace(envValue)
	if trimmed == "" {
		return Config{Mode: PreemptionModeDisabled}, nil
	}

	mode := PreemptionMode(trimmed)
	if !mode.IsValid() {
		return Config{}, fmt.Errorf("%w: unrecognized or forbidden preemption mode %q (allowed: %q, %q)", ErrInvalidPreemptionMode, trimmed, PreemptionModeDisabled, PreemptionModeAuditOnly)
	}

	return Config{Mode: mode}, nil
}
