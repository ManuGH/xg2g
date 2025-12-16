// SPDX-License-Identifier: MIT

package config

import (
	"fmt"

	"github.com/ManuGH/xg2g/internal/log"
)

// Deprecation represents a deprecated configuration field
type Deprecation struct {
	OldField        string // The deprecated field name (e.g., "timeout_ms")
	NewField        string // The replacement field name (e.g., "timeoutMs")
	DeprecatedSince string // Version when it was deprecated (e.g., "1.8.0")
	RemovalVersion  string // Version when it will be removed (e.g., "2.0.0")
}

// deprecationRegistry contains all known deprecated configuration fields
// This is intentionally empty for now but provides the infrastructure
// for future deprecations.
var deprecationRegistry = map[string]Deprecation{
	// Example (not currently used):
	// "timeout_ms": {
	//     OldField:        "timeout_ms",
	//     NewField:        "timeoutMs",
	//     DeprecatedSince: "1.8.0",
	//     RemovalVersion:  "2.0.0",
	// },
}

// checkDeprecations scans the raw YAML data for deprecated fields
// and logs warnings if any are found.
// This provides early warning to users before fields are removed.
func checkDeprecations(data []byte) {
	// For now, this is a placeholder implementation that can be extended
	// when actual deprecations are introduced.
	//
	// Future implementation strategy:
	// 1. Parse YAML into map[string]interface{} (without strict mode)
	// 2. Check for deprecated keys in the registry
	// 3. Log warnings with migration path
	// 4. Optionally increment telemetry counter: config_deprecation_warnings_total{field="..."}
	//
	// Example detection logic:
	//
	// var raw map[string]interface{}
	// if err := yaml.Unmarshal(data, &raw); err != nil {
	//     return // Skip deprecation check if YAML is malformed
	// }
	//
	// for key := range raw {
	//     if dep, found := deprecationRegistry[key]; found {
	//         log.WithComponent("config").Warn().
	//             Str("old_field", dep.OldField).
	//             Str("new_field", dep.NewField).
	//             Str("deprecated_since", dep.DeprecatedSince).
	//             Str("removal_version", dep.RemovalVersion).
	//             Msg("deprecated configuration field detected")
	//     }
	// }
}

// LogDeprecationWarning logs a structured warning for a deprecated field
func LogDeprecationWarning(dep Deprecation) {
	logger := log.WithComponent("config")
	logger.Warn().
		Str("old_field", dep.OldField).
		Str("new_field", dep.NewField).
		Str("deprecated_since", dep.DeprecatedSince).
		Str("removal_version", dep.RemovalVersion).
		Msgf("deprecated configuration field '%s' detected, please use '%s' instead (will be removed in %s)",
			dep.OldField, dep.NewField, dep.RemovalVersion)
}

// GetDeprecation looks up a deprecation by old field name
func GetDeprecation(oldField string) (Deprecation, bool) {
	dep, found := deprecationRegistry[oldField]
	return dep, found
}

// AddDeprecation registers a new deprecation (used for testing)
func AddDeprecation(dep Deprecation) {
	deprecationRegistry[dep.OldField] = dep
}

// ClearDeprecations clears all deprecations (used for testing)
func ClearDeprecations() {
	deprecationRegistry = make(map[string]Deprecation)
}

// ValidateDeprecations checks if any deprecated fields are used in FileConfig
// This is a helper for future use when we actually have deprecated fields
func ValidateDeprecations(cfg *FileConfig) error {
	// Placeholder for future validation logic
	// For example, checking if old field names are present in the raw YAML
	// and providing migration suggestions
	return nil
}

// DeprecationSummary returns a human-readable summary of all registered deprecations
func DeprecationSummary() string {
	if len(deprecationRegistry) == 0 {
		return "No deprecated configuration fields"
	}

	summary := "Deprecated configuration fields:\n"
	for _, dep := range deprecationRegistry {
		summary += fmt.Sprintf("  - %s → %s (deprecated since %s, will be removed in %s)\n",
			dep.OldField, dep.NewField, dep.DeprecatedSince, dep.RemovalVersion)
	}
	return summary
}
