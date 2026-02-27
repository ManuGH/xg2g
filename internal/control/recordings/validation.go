package recordings

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
)

var (
	ErrInvalidRecordingRef = errors.New("recording ref invalid")
)

const (
	recordingIDMinLen = 1
	recordingIDMaxLen = 1024
)

// ValidateRecordingRef checks if the service reference string is valid.
func ValidateRecordingRef(serviceRef string) error {
	if !utf8.ValidString(serviceRef) {
		return ErrInvalidRecordingRef
	}

	for _, r := range serviceRef {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return ErrInvalidRecordingRef
		}
	}

	trimmedRef := strings.TrimSpace(serviceRef)
	if trimmedRef == "" {
		return ErrInvalidRecordingRef
	}

	receiverPath := internalrecordings.ExtractPathFromServiceRef(trimmedRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return ErrInvalidRecordingRef
	}
	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return ErrInvalidRecordingRef
	}
	if strings.Contains(receiverPath, "/../") || strings.HasSuffix(receiverPath, "/..") {
		return ErrInvalidRecordingRef
	}

	if decoded, err := url.PathUnescape(receiverPath); err == nil {
		if strings.Contains(decoded, "/../") || strings.HasSuffix(decoded, "/..") {
			return ErrInvalidRecordingRef
		}
	}

	return nil
}

// ValidateLiveRef strictly asserts that a service reference is a Live Enigma2 structural format.
// It explicitly denies any paths (no slashes) to prevent cross-contamination with DVR routes.
func ValidateLiveRef(serviceRef string) error {
	if !utf8.ValidString(serviceRef) {
		return ErrInvalidRecordingRef
	}

	for _, r := range serviceRef {
		// Control chars, slashes or invalid query modifiers are forbidden in clean ETB DVB streams
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' || r == '/' {
			return ErrInvalidRecordingRef
		}
	}

	trimmedRef := strings.TrimSpace(serviceRef)
	if trimmedRef == "" {
		return ErrInvalidRecordingRef
	}

	// Live streams MUST contain colons for Enigma2 formatting (1:0:1:...)
	if !strings.Contains(trimmedRef, ":") {
		return ErrInvalidRecordingRef
	}

	return nil
}

// EncodeRecordingID encodes a service reference into a web-safe ID (Hex).
func EncodeRecordingID(serviceRef string) string {
	if strings.TrimSpace(serviceRef) == "" {
		return ""
	}
	return hex.EncodeToString([]byte(serviceRef))
}

// ValidRecordingID checks if the ID conforms to our expected charset (Hex).
func ValidRecordingID(id string) bool {
	if len(id) < recordingIDMinLen || len(id) > recordingIDMaxLen {
		return false
	}
	// Hex only allows 0-9, a-f, A-F
	for _, r := range id {
		isDigit := r >= '0' && r <= '9'
		isAF := r >= 'a' && r <= 'f'
		isAf := r >= 'A' && r <= 'F'
		if !isDigit && !isAF && !isAf {
			return false
		}
	}
	return true
}

// DecodeRecordingID decodes a recording ID back to its service reference.
func DecodeRecordingID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if !ValidRecordingID(id) {
		return "", false
	}
	decodedBytes, err := hex.DecodeString(id)
	if err != nil {
		return "", false
	}
	if len(decodedBytes) == 0 {
		return "", false
	}
	if !utf8.Valid(decodedBytes) {
		return "", false
	}
	decoded := string(decodedBytes)
	if strings.TrimSpace(decoded) == "" {
		return "", false
	}
	if strings.ContainsRune(decoded, '\x00') {
		return "", false
	}
	if err := ValidateRecordingRef(decoded); err != nil {
		return "", false
	}
	return decoded, true
}

// SanitizeRecordingRelPath implementation for POSIX paths.
func SanitizeRecordingRelPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return "", true
		}
	}

	p = strings.TrimLeft(p, "/")
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", true
	}
	if clean == "." {
		return "", false
	}

	return clean, false
}

// EscapeServiceRefPath percent-encodes a string for use in a URL path.
func EscapeServiceRefPath(s string) string {
	const upperhex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if ShouldEscapeRefChar(c) {
			b.WriteByte('%')
			b.WriteByte(upperhex[c>>4])
			b.WriteByte(upperhex[c&15])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ShouldEscapeRefChar checks if a byte should be percent-encoded.
func ShouldEscapeRefChar(c byte) bool {
	if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' {
		return false
	}
	switch c {
	case '-', '.', '_', '~', ':', '/':
		return false
	}
	return true
}

// IsAllowedVideoSegment provides a single canonical check for segment serving.
func IsAllowedVideoSegment(path string) bool {
	name := strings.Split(path, "/")
	base := name[len(name)-1]

	if base == "init.mp4" {
		return true
	}
	if !strings.HasPrefix(base, "seg_") {
		return false
	}

	baseLower := strings.ToLower(base)
	return strings.HasSuffix(baseLower, ".ts") || strings.HasSuffix(baseLower, ".m4s") || strings.HasSuffix(baseLower, ".cmfv")
}
