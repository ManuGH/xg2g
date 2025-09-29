// SPDX-License-Identifier: MIT
package epg

import (
	"regexp"
	"strings"
)

// makeStableID creates a stable identifier from a channel name for EPG use.
// This implementation is specific to EPG channel ID generation and may differ
// from the playlist ID generation in other packages.
func makeStableID(input string) string {
	if input == "" {
		return ""
	}

	// Convert to lowercase first
	result := strings.ToLower(input)

	// Remove all non-alphanumeric characters except spaces, dots, hyphens, and basic separators
	// Keep umlauts and other Unicode letters for international channel names
	cleaned := regexp.MustCompile(`[^a-zA-ZÀ-ÿ0-9\s.\-_]`).ReplaceAllString(result, "")

	// If after cleaning we only have separators/spaces, return empty
	if regexp.MustCompile(`^[\s.\-_]*$`).MatchString(cleaned) {
		return ""
	}

	// Replace consecutive spaces, dots, hyphens, and underscores with single dots
	normalized := regexp.MustCompile(`[\s.\-_]+`).ReplaceAllString(cleaned, ".")

	// Trim leading/trailing dots
	normalized = strings.Trim(normalized, ".")

	return normalized
}
