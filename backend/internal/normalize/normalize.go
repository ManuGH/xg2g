package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidLiveRef = errors.New("live service reference invalid")

const (
	minLiveRefLen = 7
	maxLiveRefLen = 256
)

// ValidateLiveRef strictly asserts that a service reference is a clean Enigma2 Live TV format.
// It enforces canonical boundaries:
//   - Must be valid UTF-8
//   - Length bounded [7, 256]
//   - No leading or trailing whitespace
//   - Strictly forbids slashes (/), backslashes (\), query injections (?, #), percent-encodings (%),
//     dots (.), null bytes, Unicode control or format characters.
//   - Must contain colons separating structural Enigma2 tokens.
//   - Every character must be an ASCII alphanumeric, colon, or underscore.
func ValidateLiveRef(serviceRef string) error {
	if !utf8.ValidString(serviceRef) {
		return ErrInvalidLiveRef
	}

	// Length bounds
	if len(serviceRef) < minLiveRefLen || len(serviceRef) > maxLiveRefLen {
		return ErrInvalidLiveRef
	}

	// No leading or trailing whitespace
	if strings.TrimSpace(serviceRef) != serviceRef {
		return ErrInvalidLiveRef
	}

	// Must contain colons for Enigma2 formatting (1:0:1:...)
	if !strings.Contains(serviceRef, ":") {
		return ErrInvalidLiveRef
	}

	parts := strings.Split(serviceRef, ":")
	if len(parts) < 4 {
		return ErrInvalidLiveRef
	}

	// Whitelist: strictly allow only ASCII alphanumeric characters [0-9A-Za-z], colon ':', and underscore '_'
	for _, r := range serviceRef {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) || r == '/' || r == '\\' || r == '?' || r == '#' || r == 0 || r > unicode.MaxASCII {
			return ErrInvalidLiveRef
		}
	}

	if strings.Contains(serviceRef, "..") {
		return ErrInvalidLiveRef
	}

	// 2. Multi-level URL decoding boundary check to eliminate double-decoding vulnerabilities
	current := serviceRef
	for i := 0; i < 3; i++ {
		if !strings.Contains(current, "%") {
			break
		}
		decoded, err := url.PathUnescape(current)
		if err != nil {
			return ErrInvalidLiveRef
		}
		if strings.Contains(decoded, "..") || strings.Contains(decoded, "/") || strings.Contains(decoded, "\\") ||
			strings.Contains(decoded, "?") || strings.Contains(decoded, "#") || strings.Contains(decoded, "\x00") ||
			strings.Contains(decoded, "\r") || strings.Contains(decoded, "\n") {
			return ErrInvalidLiveRef
		}
		for _, r := range decoded {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return ErrInvalidLiveRef
			}
		}
		if decoded == current {
			break
		}
		current = decoded
	}

	return nil
}

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

// MapHash takes any map[string]any (used often for capabilities or query params),
// deterministically marshals it using Go's built-in sorted json.Marshal algorithm,
// and returns a SHA-256 hexadecimal string representation.
// This is used for generating cryptographically stable `capHash` bindings in JWT tokens.
func MapHash(m map[string]any) (string, error) {
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
