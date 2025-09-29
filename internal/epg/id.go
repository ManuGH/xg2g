// SPDX-License-Identifier: MIT
package epg

import (
	"regexp"
	"strings"
	"unicode"
)

// makeStableID creates a stable identifier from a channel name for EPG use.
// This implementation is specific to EPG channel ID generation and may differ
// from the playlist ID generation in other packages.
func makeStableID(input string) string {
	if input == "" {
		return ""
	}

	// Convert to lowercase first
	lower := strings.ToLower(input)

	// Keep:
	// - all ASCII letters and digits
	// - any rune > 127 (full Unicode; allows umlauts, fractions like ½, etc.)
	// - separators: space, dot, hyphen, underscore
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || unicode.IsLetter(r) || unicode.IsNumber(r) || r > 127 || r == ' ' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, lower)

	// If after cleaning we only have separators/spaces, return empty
	if reOnlySeparators.MatchString(cleaned) {
		return ""
	}

	// Replace consecutive spaces, dots, hyphens, and underscores with single dots
	normalized := reSepNormalize.ReplaceAllString(cleaned, ".")

	// Trim leading/trailing dots
	normalized = strings.Trim(normalized, ".")

	return normalized
}

// Precompiled regexes for normalization
var (
	reOnlySeparators = regexp.MustCompile(`^[\s.\-_]*$`)
	reSepNormalize   = regexp.MustCompile(`[\s.\-_]+`)
)
