package sourceref

import "errors"

// Sentinel errors returned by the sourceref package.
// None of these error strings contain raw references, URLs, hosts, or userinfo.
var (
	// ErrNotIPTV indicates that the reference does not belong to an IPTV service type (e.g. DVB).
	ErrNotIPTV = errors.New("service reference is not an IPTV stream")

	// ErrNotFound indicates that the requested IPTV source ID is not present in the registry.
	ErrNotFound = errors.New("iptv source not found")

	// ErrInvalidRef indicates a malformed service reference structure.
	ErrInvalidRef = errors.New("invalid IPTV service reference format")

	// ErrEmptyRef indicates that an empty or whitespace-only reference was provided.
	ErrEmptyRef = errors.New("empty IPTV service reference")

	// ErrOversizeURL indicates that the embedded URL exceeds the maximum allowed length.
	ErrOversizeURL = errors.New("IPTV URL exceeds maximum allowed length")

	// ErrInvalidScheme indicates an unsupported URL scheme (only http and https are allowed).
	ErrInvalidScheme = errors.New("invalid IPTV URL scheme: only http and https are allowed")

	// ErrInvalidURL indicates a malformed or invalid URL within the reference.
	ErrInvalidURL = errors.New("malformed IPTV URL in service reference")

	// ErrShortKey indicates that the provided HMAC key is shorter than the required minimum.
	ErrShortKey = errors.New("HMAC key is too short: minimum 16 bytes required")

	// ErrCollision indicates that distinct stream URLs yielded an identical ID in the registry.
	ErrCollision = errors.New("iptv source ID collision detected")
)
