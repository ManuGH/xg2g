package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// Principal represents the authenticated identity of a caller.
type Principal struct {
	// ID is the stable, unique identifier for the user.
	// It is either the explicit User from config or a hash of the token.
	ID string

	// Token is the raw authentication token (optional, usually kept empty for security in logs).
	Token string

	// Scopes are the permissions granted to this principal.
	Scopes []string

	// User is the human-readable username if configured (e.g., "dad").
	User string

	// DeviceID names the enrolled device this credential belongs to, and is
	// set only for device-bound (DPoP) credentials — empty for config tokens,
	// browser sessions and everything else.
	//
	// It exists so an endpoint that must act on "the calling device" can read
	// the answer from the authenticated identity instead of trusting a field in
	// the request. Re-deriving it in a handler is not an option either: the
	// DPoP proof's jti is replay-cached, so validating the same proof a second
	// time is indistinguishable from an attack and is refused.
	DeviceID string
}

// NewPrincipal creates a Principal from a token and optional user/scopes.
func NewPrincipal(token string, user string, scopes []string) *Principal {
	id := user
	if id == "" {
		// Fallback: derive stable ID from token
		// "t_" prefix to distinguish from potential username collisions
		hash := sha256.Sum256([]byte(token))
		id = "t_" + hex.EncodeToString(hash[:])[:16]
	}

	return &Principal{
		ID:     id,
		Token:  token, // We store it for session creation exchange, but typically don't log it
		Scopes: scopes,
		User:   user,
	}
}
