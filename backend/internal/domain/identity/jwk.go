// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

var ErrInvalidJWKCurve = errors.New("dpop: jwk must be EC curve P-256")

// JWKECPublicKey represents an EC P-256 public key in JWK format.
type JWKECPublicKey struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// ComputeJWKThumbprint computes the canonical RFC 7638 SHA-256 Base64URL thumbprint (jkt) for EC P-256.
// Invariant: Member keys must be ordered lexicographically: "crv", "kty", "x", "y".
func ComputeJWKThumbprint(jwk JWKECPublicKey) (string, error) {
	if jwk.Kty != "EC" || jwk.Crv != "P-256" || jwk.X == "" || jwk.Y == "" {
		return "", ErrInvalidJWKCurve
	}

	canonicalMap := map[string]string{
		"crv": jwk.Crv,
		"kty": jwk.Kty,
		"x":   jwk.X,
		"y":   jwk.Y,
	}

	rawJSON := fmt.Sprintf(`{"crv":%q,"kty":%q,"x":%q,"y":%q}`,
		canonicalMap["crv"], canonicalMap["kty"], canonicalMap["x"], canonicalMap["y"])

	hash := sha256.Sum256([]byte(rawJSON))
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}
