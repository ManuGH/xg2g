package recordings

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidRecordingRef = errors.New("recording ref invalid")
)

const (
	recordingIDMinLen = 1
	recordingIDMaxLen = 1024
)

// ValidateRecordingRef checks if the service reference string is valid and safe (R5).
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

	// 1. Structural Enigma2 check (1:0:0:0:0:0:0:0:0:0:PATH)
	parts := strings.Split(trimmedRef, ":")
	if len(parts) < 11 {
		return ErrInvalidRecordingRef
	}

	// 2. Strict SSRF/Traversal guards on the path component (Gate R5)
	receiverPath := parts[10]
	if receiverPath == "" {
		return ErrInvalidRecordingRef
	}

	// Explicitly deny common SSRF/Traversal patterns
	if strings.Contains(receiverPath, "..") {
		return ErrInvalidRecordingRef
	}
	if strings.Contains(receiverPath, "://") {
		return ErrInvalidRecordingRef
	}

	// 3. Path normalization check
	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return ErrInvalidRecordingRef
	}

	if decoded, err := url.PathUnescape(receiverPath); err == nil {
		if strings.Contains(decoded, "..") || strings.Contains(decoded, "://") {
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

// CanonicalResumeKeyFromServiceRef returns the single canonical resume key for a
// recording service reference. Resume persistence is keyed by the public
// recording ID encoding, not by raw serviceRef strings.
func CanonicalResumeKeyFromServiceRef(serviceRef string) (string, bool) {
	serviceRef = strings.TrimSpace(serviceRef)
	if err := ValidateRecordingRef(serviceRef); err != nil {
		return "", false
	}
	return EncodeRecordingID(serviceRef), true
}

// CanonicalResumeKeyFromRecordingID returns the single canonical resume key for
// a recording ID. Invalid IDs are rejected instead of silently producing a
// competing technical key.
func CanonicalResumeKeyFromRecordingID(recordingID string) (string, bool) {
	serviceRef, ok := DecodeRecordingID(recordingID)
	if !ok {
		return "", false
	}
	return CanonicalResumeKeyFromServiceRef(serviceRef)
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
