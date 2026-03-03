package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// ServiceRef normalizes an Enigma2 Service Reference
// It removes trailing colons, trims whitespace, and uppercases hexadecimal blocks
// to ensure deterministic matching between client requests and JWT signed payloads.
func ServiceRef(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	// Remove trailing colons frequently added by Enigma2 boxes
	for strings.HasSuffix(s, ":") {
		s = strings.TrimSuffix(s, ":")
	}
	return s
}

// MapHash takes any map[string]interface{} (used often for capabilities or query params),
// deterministically marshals it using Go's built-in sorted json.Marshal algorithm,
// and returns a SHA-256 hexadecimal string representation.
// This is used for generating cryptographically stable `capHash` bindings in JWT tokens.
func MapHash(m map[string]interface{}) (string, error) {
	if len(m) == 0 {
		return "", nil // Empty map has no hash signature
	}

	// Go 1.14+ json.Marshal guarantees deterministic map key sorting.
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}
