// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"testing"
)

func realKey(t *testing.T) JWKECPublicKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))),
		Y:   base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))),
	}
}

func TestRealKeyIsAccepted(t *testing.T) {
	for i := 0; i < 20; i++ {
		if err := ValidateEnrollmentJWK(realKey(t)); err != nil {
			t.Fatalf("a freshly generated P-256 key must validate, got %v", err)
		}
	}
}

func TestWrongKeyTypeOrCurveIsRejected(t *testing.T) {
	base := realKey(t)

	for name, mutate := range map[string]func(JWKECPublicKey) JWKECPublicKey{
		"rsa":       func(j JWKECPublicKey) JWKECPublicKey { j.Kty = "RSA"; return j },
		"p-384":     func(j JWKECPublicKey) JWKECPublicKey { j.Crv = "P-384"; return j },
		"empty kty": func(j JWKECPublicKey) JWKECPublicKey { j.Kty = ""; return j },
		"empty crv": func(j JWKECPublicKey) JWKECPublicKey { j.Crv = ""; return j },
	} {
		if err := ValidateEnrollmentJWK(mutate(base)); !errors.Is(err, ErrInvalidJWKCurve) {
			t.Errorf("%s: err = %v, want ErrInvalidJWKCurve", name, err)
		}
	}
}

// The gap that matters: a coordinate whose leading zero was dropped decodes to
// the same number and lies on the curve, but hashes to a different thumbprint.
// Since jwk_thumbprint is UNIQUE, one physical key could otherwise enrol twice.
func TestShortCoordinateIsRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// big.Int.Bytes() drops leading zeros — the natural way to get a short
	// encoding of a genuine coordinate.
	jwk := JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(append([]byte{}, key.X.Bytes()...)),
		Y:   base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))),
	}

	// Only meaningful when this key actually has a leading zero byte.
	if len(key.X.Bytes()) == 32 {
		jwk.X = base64.RawURLEncoding.EncodeToString(key.X.Bytes()[1:])
	}

	if err := ValidateEnrollmentJWK(jwk); !errors.Is(err, ErrJWKCoordinateLength) {
		t.Errorf("err = %v, want ErrJWKCoordinateLength", err)
	}
}

func TestOversizedCoordinateIsRejected(t *testing.T) {
	jwk := realKey(t)
	jwk.X = base64.RawURLEncoding.EncodeToString(make([]byte, 33))

	if err := ValidateEnrollmentJWK(jwk); !errors.Is(err, ErrJWKCoordinateLength) {
		t.Errorf("err = %v, want ErrJWKCoordinateLength", err)
	}
}

func TestEmptyCoordinateIsRejected(t *testing.T) {
	jwk := realKey(t)
	jwk.Y = ""

	if err := ValidateEnrollmentJWK(jwk); !errors.Is(err, ErrJWKCoordinateLength) {
		t.Errorf("err = %v, want ErrJWKCoordinateLength", err)
	}
}

// Padded or standard-alphabet base64 hashes differently, so it is refused
// rather than normalized.
func TestNonRawURLEncodingIsRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw := key.X.FillBytes(make([]byte, 32))

	for name, encoded := range map[string]string{
		"padded url alphabet": base64.URLEncoding.EncodeToString(raw),
		"standard alphabet":   base64.StdEncoding.EncodeToString(raw),
		"not base64":          "!!!!not-base64!!!!",
	} {
		jwk := realKey(t)
		jwk.X = encoded
		if err := ValidateEnrollmentJWK(jwk); err == nil {
			t.Errorf("%s: expected a rejection", name)
		}
	}
}

// An off-curve point is not merely malformed; accepting one can leak private
// key material in some protocols.
func TestOffCurvePointIsRejected(t *testing.T) {
	valid := realKey(t)
	xBytes, err := base64.RawURLEncoding.DecodeString(valid.X)
	if err != nil {
		t.Fatalf("decode x: %v", err)
	}

	// Flip a bit in x; the point almost certainly leaves the curve while the
	// encoding stays perfectly well-formed and correctly sized.
	xBytes[31] ^= 0x01
	valid.X = base64.RawURLEncoding.EncodeToString(xBytes)

	if err := ValidateEnrollmentJWK(valid); !errors.Is(err, ErrJWKPointOffCurve) {
		t.Errorf("err = %v, want ErrJWKPointOffCurve", err)
	}
}

func TestPointAtInfinityIsRejected(t *testing.T) {
	zero := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	jwk := JWKECPublicKey{Kty: "EC", Crv: "P-256", X: zero, Y: zero}

	if err := ValidateEnrollmentJWK(jwk); !errors.Is(err, ErrJWKPointOffCurve) {
		t.Errorf("err = %v, want ErrJWKPointOffCurve", err)
	}
}

// A validated key must produce a thumbprint, and equal keys must produce equal
// thumbprints — that pairing is what makes the UNIQUE constraint meaningful.
func TestValidatedKeyThumbprintsAreStable(t *testing.T) {
	jwk := realKey(t)
	if err := ValidateEnrollmentJWK(jwk); err != nil {
		t.Fatalf("validate: %v", err)
	}

	first, err := ComputeJWKThumbprint(jwk)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	second, err := ComputeJWKThumbprint(jwk)
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}

	if first != second {
		t.Errorf("thumbprint is not stable: %q vs %q", first, second)
	}
	if len(first) != 43 {
		t.Errorf("thumbprint length = %d, want 43 (unpadded base64url SHA-256)", len(first))
	}
}

// Guard against a future refactor reintroducing a length-agnostic path: two
// encodings of one key must never both validate.
//
// A random coordinate has a leading zero byte only about once in 256, so the
// key is searched for rather than hoped for. A test that silently skips itself
// most of the time is not a test.
func TestOneKeyCannotValidateUnderTwoEncodings(t *testing.T) {
	var key *ecdsa.PrivateKey
	for i := 0; i < 5000; i++ {
		candidate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		if len(candidate.X.Bytes()) < p256CoordinateBytes {
			key = candidate
			break
		}
	}
	if key == nil {
		t.Fatal("no key with a leading zero byte in x found in 5000 attempts")
	}

	padded := JWKECPublicKey{
		Kty: "EC", Crv: "P-256",
		X: base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, p256CoordinateBytes))),
		Y: base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, p256CoordinateBytes))),
	}
	trimmed := padded
	trimmed.X = base64.RawURLEncoding.EncodeToString(new(big.Int).Set(key.X).Bytes())

	if padded.X == trimmed.X {
		t.Fatal("the search returned a key whose encodings are identical")
	}
	if err := ValidateEnrollmentJWK(padded); err != nil {
		t.Fatalf("the canonical encoding must validate, got %v", err)
	}
	if err := ValidateEnrollmentJWK(trimmed); err == nil {
		t.Error("the trimmed encoding must be refused, or one key enrols twice")
	}
}
