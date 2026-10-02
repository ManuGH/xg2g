package sourceref

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	// MaxURLLength defines the maximum permitted URL length (4096 bytes).
	MaxURLLength = 4096
)

var (
	// portContinuationRegex detects unencoded colon before port in bouquets (e.g. "...:8080/path:Name").
	// When parts[11] matches ^[0-9]{1,5}(/|$) and decodedURL has no explicit port, parts[11] is
	// authority/path continuation rather than a channel name.
	portContinuationRegex = regexp.MustCompile(`^[0-9]{1,5}(/|$)`)
)

// Supported IPTV service types.
const (
	TypeGStreamer  = "4097"
	TypeExteplayer = "5001"
	TypeGstPlayer  = "5002"
)

// IsIPTVServiceType returns true if the service type string is a recognized IPTV type.
func IsIPTVServiceType(st string) bool {
	return st == TypeGStreamer || st == TypeExteplayer || st == TypeGstPlayer
}

// Parser parses Enigma2 IPTV references and generates opaque Sources using a keyed HMAC.
type Parser struct {
	key []byte
}

// NewParser creates a new Parser with the given HMAC secret key.
// The key must be at least 16 bytes long.
func NewParser(hmacKey []byte) (*Parser, error) {
	if len(hmacKey) < 16 {
		return nil, ErrShortKey
	}
	keyCopy := make([]byte, len(hmacKey))
	copy(keyCopy, hmacKey)
	return &Parser{key: keyCopy}, nil
}

// Parse extracts and validates an IPTV source reference, returning a redacted Source.
func (p *Parser) Parse(ref string) (Source, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return Source{}, ErrEmptyRef
	}

	parts := strings.Split(trimmed, ":")
	if len(parts) == 0 {
		return Source{}, ErrEmptyRef
	}

	serviceType := parts[0]
	if !IsIPTVServiceType(serviceType) {
		return Source{}, ErrNotIPTV
	}

	// An Enigma2 IPTV service reference must have at least 11 fields:
	// indices 0..9 are header fields, index 10 is the URL field.
	// Index 11+ is the optional channel name (which may contain colons).
	if len(parts) < 11 {
		return Source{}, ErrInvalidRef
	}

	rawURLField := parts[10]
	if rawURLField == "" {
		return Source{}, ErrInvalidRef
	}

	if len(rawURLField) > MaxURLLength {
		return Source{}, ErrOversizeURL
	}

	// Service name is reconstructed from any trailing fields.
	var serviceName string
	if len(parts) > 11 {
		serviceName = strings.Join(parts[11:], ":")
	}

	// Percent-decode exactly once using PathUnescape (preserves literal '+')
	decodedURL, err := url.PathUnescape(rawURLField)
	if err != nil {
		return Source{}, ErrInvalidURL
	}

	if len(decodedURL) > MaxURLLength {
		return Source{}, ErrOversizeURL
	}

	// Reject control characters, newlines, and NUL bytes
	for i := 0; i < len(decodedURL); i++ {
		c := decodedURL[i]
		if c < 0x20 || c == 0x7f {
			return Source{}, ErrInvalidURL
		}
	}

	parsed, err := url.Parse(decodedURL)
	if err != nil {
		return Source{}, ErrInvalidURL
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Source{}, ErrInvalidScheme
	}

	if parsed.Hostname() == "" {
		return Source{}, ErrInvalidURL
	}

	// Heuristic for unencoded port colon (D3 / E2):
	// If a hand-edited bouquet contains an unencoded colon before the port
	// (e.g. "http%3a//h.invalid:8080/live/x.ts:Chan" or "http%3a//h.invalid:8080:Chan"),
	// the colon split causes parts[10] to receive only the host and parts[11] to receive
	// the port and optional path.
	// To avoid false positives on valid references where parts[10] already has a path or query
	// (e.g. "http%3a//h.invalid/live.m3u8:101" or "http%3a//h.invalid/live.m3u8:5/6 Kanal"),
	// the heuristic rejects with ErrInvalidRef iff ALL of the following hold:
	// - len(parts) > 11
	// - parsed.Port() == "" (no explicit port decoded)
	// - decoded path is "" or "/" (parsed.Path == "" || parsed.Path == "/")
	// - parsed.RawQuery == "" (no query string)
	// - portContinuationRegex.MatchString(parts[11])
	// Residual ambiguity (documented, fail-closed):
	// A host-only URL followed by a purely numeric channel name is rejected.
	isHostOnly := (parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == ""
	if len(parts) > 11 && parsed.Port() == "" && isHostOnly && portContinuationRegex.MatchString(parts[11]) {
		return Source{}, ErrInvalidRef
	}

	canonical := CanonicalizeURL(parsed)
	id := deriveID(canonical, p.key)

	return Source{
		d: &sourceData{
			id:           id,
			serviceType:  serviceType,
			serviceName:  serviceName,
			rawURL:       func() string { return decodedURL },
			canonicalURL: func() string { return canonical },
		},
	}, nil
}

// Parse is a convenience function that constructs a temporary Parser and parses the reference.
func Parse(ref string, hmacKey []byte) (Source, error) {
	p, err := NewParser(hmacKey)
	if err != nil {
		return Source{}, err
	}
	return p.Parse(ref)
}
