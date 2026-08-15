// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
)

// p256CoordinateBytes is the fixed width of a P-256 coordinate.
const p256CoordinateBytes = 32

var (
	// ErrJWKCoordinateEncoding: x or y is not unpadded base64url.
	ErrJWKCoordinateEncoding = errors.New("identity: jwk coordinate must be unpadded base64url")
	// ErrJWKCoordinateLength: x or y is not exactly 32 bytes.
	ErrJWKCoordinateLength = errors.New("identity: jwk coordinate must be exactly 32 bytes")
	// ErrJWKPointOffCurve: (x, y) is not a point on P-256.
	ErrJWKPointOffCurve = errors.New("identity: jwk point does not lie on the P-256 curve")
)

// ValidateEnrollmentJWK enforces the strict rules for a key that is about to
// become a durable device identity.
//
// Stricter than proof-time parsing on one point that matters only here: the
// coordinates must be **exactly** 32 bytes each.
//
// The thumbprint is computed from the canonical JSON *strings*, not from the
// integers. A coordinate whose leading zero byte was dropped still decodes to
// the same number and still lies on the curve, but produces a different `jkt`.
// Since `identity.devices.jwk_thumbprint` is UNIQUE, one physical key could
// then enrol twice as two separate device identities, and a revocation of one
// would leave the other alive. Fixing the width closes that.
//
// Must be called before any persistent write. An invalid key has to fail while
// nothing has been created yet.
func ValidateEnrollmentJWK(jwk JWKECPublicKey) error {
	if jwk.Kty != "EC" || jwk.Crv != "P-256" {
		return ErrInvalidJWKCurve
	}

	x, err := decodeP256Coordinate(jwk.X, "x")
	if err != nil {
		return err
	}
	y, err := decodeP256Coordinate(jwk.Y, "y")
	if err != nil {
		return err
	}

	// An off-curve point is not merely malformed; accepting one can leak private
	// key material in some protocols, so it is refused outright.
	if !elliptic.P256().IsOnCurve(new(big.Int).SetBytes(x), new(big.Int).SetBytes(y)) {
		return ErrJWKPointOffCurve
	}

	return nil
}

func decodeP256Coordinate(value, name string) ([]byte, error) {
	// RawURLEncoding rejects padding and the non-URL alphabet, which is what
	// JOSE requires. A padded or standard-alphabet coordinate is not merely
	// ugly — it would hash to a different thumbprint.
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", ErrJWKCoordinateEncoding, name)
	}
	if len(decoded) != p256CoordinateBytes {
		return nil, fmt.Errorf("%w (%s has %d)", ErrJWKCoordinateLength, name, len(decoded))
	}
	return decoded, nil
}
