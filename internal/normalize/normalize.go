package normalize

import (
	"strings"
	"unicode"
)

// Token normalizes a string token for matching:
// - trims Unicode whitespace + invisible edge characters
// - lowercases for case-insensitive comparisons
func Token(s string) string {
	return strings.ToLower(strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) ||
			r == '\u200B' || // Zero Width Space
			r == '\u200C' || // Zero Width Non-Joiner
			r == '\u200D' || // Zero Width Joiner
			r == '\uFEFF' // Zero Width Non-Breaking Space (BOM)
	}))
}
