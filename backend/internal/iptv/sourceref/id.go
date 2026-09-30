package sourceref

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"net"
	"net/url"
	"strings"
)

const (
	// IDPrefix is the mandatory prefix for all opaque IPTV source identifiers.
	IDPrefix = "iptv_"

	// idDigestTruncationBytes specifies the number of HMAC bytes used for ID generation.
	// 16 bytes (128 bits) yields 26 lowercase base32 characters.
	idDigestTruncationBytes = 16
)

var (
	// base32Encoding uses lowercase standard base32 without padding.
	base32Encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
)

// ID represents an opaque, deterministic, non-reversible IPTV source identifier.
type ID string

// String returns the string representation of the ID.
func (id ID) String() string {
	return string(id)
}

// IsValid checks whether the ID conforms to the expected format and length.
func (id ID) IsValid() bool {
	s := string(id)
	if !strings.HasPrefix(s, IDPrefix) {
		return false
	}
	body := s[len(IDPrefix):]
	// 16 bytes in unpadded base32 is exactly 26 characters
	if len(body) != 26 {
		return false
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// CanonicalizeURL produces a normalized representation of a URL for deterministic ID generation.
// The normalization is strictly lossless for path and query:
//   - Scheme and host are lowercased.
//   - Default ports (80 for http, 443 for https) are stripped.
//   - Empty path becomes "/".
//   - Raw path (EscapedPath()) and query (RawQuery) are preserved byte-for-byte,
//     with no re-encoding, sorting, or dropping of parameters.
//   - Userinfo is preserved as part of the hash input.
func CanonicalizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	scheme := strings.ToLower(u.Scheme)

	// Userinfo
	var userInfoStr string
	if u.User != nil {
		userInfoStr = u.User.String() + "@"
	}

	// Host and Port normalization
	hostName := strings.ToLower(u.Hostname())
	port := u.Port()

	// Strip default ports
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	var hostStr string
	if port != "" {
		hostStr = net.JoinHostPort(hostName, port)
	} else {
		// Preserve IPv6 bracket formatting if needed
		if strings.Contains(hostName, ":") && !strings.HasPrefix(hostName, "[") {
			hostStr = "[" + hostName + "]"
		} else {
			hostStr = hostName
		}
	}

	// Path normalization: preserve EscapedPath() byte-for-byte, empty path -> "/"
	pathStr := u.EscapedPath()
	if pathStr == "" {
		pathStr = "/"
	}

	// Query normalization: preserve RawQuery byte-for-byte without re-encoding or sorting
	if u.RawQuery != "" {
		pathStr += "?" + u.RawQuery
	}

	return scheme + "://" + userInfoStr + hostStr + pathStr
}

// deriveID generates a keyed HMAC-SHA256 identifier from a canonical URL.
func deriveID(canonicalURL string, key []byte) ID {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(canonicalURL))
	digest := mac.Sum(nil)
	encoded := base32Encoding.EncodeToString(digest[:idDigestTruncationBytes])
	return ID(IDPrefix + encoded)
}
