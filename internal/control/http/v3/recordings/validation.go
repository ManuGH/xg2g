package recordings

import (
	"encoding/base64"
	"errors"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ManuGH/xg2g/internal/recordings"
)

var (
	ErrInvalidRecordingRef = errors.New("recording ref invalid")
)

// ValidateRecordingRef checks if the service reference string is valid.
func ValidateRecordingRef(serviceRef string) error {
	// Security: Ensure input is valid UTF-8 before processing
	if !utf8.ValidString(serviceRef) {
		return ErrInvalidRecordingRef
	}

	// Security: Reject control chars, \ and ?#
	for _, r := range serviceRef {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return ErrInvalidRecordingRef
		}
	}

	trimmedRef := strings.TrimSpace(serviceRef)
	if trimmedRef == "" {
		return ErrInvalidRecordingRef
	}

	receiverPath := recordings.ExtractPathFromServiceRef(trimmedRef)
	if !strings.HasPrefix(receiverPath, "/") {
		return ErrInvalidRecordingRef
	}
	cleanRef := strings.TrimLeft(receiverPath, "/")
	cleanRef = path.Clean("/" + cleanRef)
	cleanRef = strings.TrimPrefix(cleanRef, "/")
	if cleanRef == "." || cleanRef == ".." || strings.HasPrefix(cleanRef, "../") {
		return ErrInvalidRecordingRef
	}
	// Strict check: Reject any ".." usage even if it effectively stays inside root
	if strings.Contains(receiverPath, "/../") || strings.HasSuffix(receiverPath, "/..") {
		return ErrInvalidRecordingRef
	}

	// Check for traversal in decoded path (catch %2e%2e)
	if decoded, err := url.PathUnescape(receiverPath); err == nil {
		if strings.Contains(decoded, "/../") || strings.HasSuffix(decoded, "/..") {
			return ErrInvalidRecordingRef
		}
	}

	return nil
}

// EncodeRecordingID encodes a service reference into a web-safe ID.
func EncodeRecordingID(serviceRef string) string {
	if strings.TrimSpace(serviceRef) == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(serviceRef))
}

// SanitizeRecordingRelPath implementation for POSIX paths.
func SanitizeRecordingRelPath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	// Security: Reject control chars, \, ?, #, and unicode Cf
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\\' || r == '?' || r == '#' {
			return "", true
		}
	}

	// Treat as relative: strip leading slashes
	p = strings.TrimLeft(p, "/")

	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", true
	}
	if clean == "." {
		return "", false // Root
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

// IsAllowedVideoSegment provides a single canonical allowlist for recordings HLS artifact serving.
//
// Allowed artifacts (by base name):
// - init.mp4 (fMP4/CMAF init segment)
// - seg_*.ts (MPEG-TS segments)
// - seg_*.m4s (fMP4/CMAF media segments)
// - seg_*.cmfv (CMAF video segments; alternative extension used by some toolchains)
//
// Security: This is intentionally strict to prevent arbitrary file exposure.
func IsAllowedVideoSegment(path string) bool {
	parts := strings.Split(path, "/")
	base := parts[len(parts)-1]

	// Allow init.mp4 for fMP4
	if base == "init.mp4" {
		return true
	}
	// Enforce prefix to prevent arbitrary file exposure
	if !strings.HasPrefix(base, "seg_") {
		return false
	}

	baseLower := strings.ToLower(base)
	for _, ext := range []string{".ts", ".m4s", ".cmfv"} {
		if strings.HasSuffix(baseLower, ext) {
			return true
		}
	}
	return false
}
