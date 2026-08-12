// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrInvalidCOSEKey = errors.New("invalid cose key")
	ErrUnsupportedAlg = errors.New("unsupported cose algorithm")
)

const (
	// COSE Key Types
	coseKtyOKP = 1
	coseKtyEC2 = 2
	coseKtyRSA = 3

	// COSE Algorithms
	coseAlgES256 = -7
	coseAlgEdDSA = -8
	coseAlgRS256 = -257

	// COSE Elliptic Curves
	coseCrvP256    = 1
	coseCrvEd25519 = 6
)

// ParseCOSEPublicKey extracts a standard crypto.PublicKey and its DER SPKI bytes
// from a decoded CBOR COSE key map.
func ParseCOSEPublicKey(coseMap map[any]any) (crypto.PublicKey, []byte, error) {
	ktyVal, ok := coseMap[int64(1)]
	if !ok {
		return nil, nil, fmt.Errorf("%w: missing kty", ErrInvalidCOSEKey)
	}
	kty, ok := ktyVal.(int64)
	if !ok {
		return nil, nil, fmt.Errorf("%w: kty not an integer", ErrInvalidCOSEKey)
	}

	algVal, ok := coseMap[int64(3)]
	if !ok {
		return nil, nil, fmt.Errorf("%w: missing alg", ErrInvalidCOSEKey)
	}
	alg, ok := algVal.(int64)
	if !ok {
		return nil, nil, fmt.Errorf("%w: alg not an integer", ErrInvalidCOSEKey)
	}

	var pub crypto.PublicKey

	switch kty {
	case coseKtyEC2:
		if alg != coseAlgES256 {
			return nil, nil, fmt.Errorf("%w: EC2 alg %d", ErrUnsupportedAlg, alg)
		}
		crvVal, ok := coseMap[int64(-1)].(int64)
		if !ok || crvVal != coseCrvP256 {
			return nil, nil, fmt.Errorf("%w: unsupported EC2 curve", ErrInvalidCOSEKey)
		}
		xBytes, okX := coseMap[int64(-2)].([]byte)
		yBytes, okY := coseMap[int64(-3)].([]byte)
		if !okX || !okY || len(xBytes) == 0 || len(yBytes) == 0 {
			return nil, nil, fmt.Errorf("%w: missing EC2 coordinates", ErrInvalidCOSEKey)
		}

		x := new(big.Int).SetBytes(xBytes)
		y := new(big.Int).SetBytes(yBytes)
		curve := elliptic.P256()

		if !curve.IsOnCurve(x, y) {
			return nil, nil, fmt.Errorf("%w: EC point not on P-256 curve", ErrInvalidCOSEKey)
		}

		pub = &ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		}

	case coseKtyOKP:
		if alg != coseAlgEdDSA {
			return nil, nil, fmt.Errorf("%w: OKP alg %d", ErrUnsupportedAlg, alg)
		}
		crvVal, ok := coseMap[int64(-1)].(int64)
		if !ok || crvVal != coseCrvEd25519 {
			return nil, nil, fmt.Errorf("%w: unsupported OKP curve", ErrInvalidCOSEKey)
		}
		xBytes, okX := coseMap[int64(-2)].([]byte)
		if !okX || len(xBytes) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("%w: invalid Ed25519 public key bytes", ErrInvalidCOSEKey)
		}
		pub = ed25519.PublicKey(xBytes)

	case coseKtyRSA:
		if alg != coseAlgRS256 {
			return nil, nil, fmt.Errorf("%w: RSA alg %d", ErrUnsupportedAlg, alg)
		}
		nBytes, okN := coseMap[int64(-1)].([]byte)
		eBytes, okE := coseMap[int64(-2)].([]byte)
		if !okN || !okE || len(nBytes) == 0 || len(eBytes) == 0 {
			return nil, nil, fmt.Errorf("%w: missing RSA modulus or exponent", ErrInvalidCOSEKey)
		}

		n := new(big.Int).SetBytes(nBytes)
		var e int
		for _, b := range eBytes {
			e = (e << 8) | int(b)
		}

		pub = &rsa.PublicKey{
			N: n,
			E: e,
		}

	default:
		return nil, nil, fmt.Errorf("%w: kty %d", ErrUnsupportedAlg, kty)
	}

	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal public key to SPKI: %w", err)
	}

	return pub, spki, nil
}
