// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package dpop_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/dpop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeJWKThumbprint_RFC7638Compliance(t *testing.T) {
	jwk := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   "f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",
		Y:   "x_m_VK2KjBq2B2x1X2K3L4M5N6O7P8Q9R0S1T2U3V4W",
	}

	jkt, err := dpop.ComputeJWKThumbprint(jwk)
	require.NoError(t, err)
	assert.NotEmpty(t, jkt)

	// Verify that changing key order or formatting in JWK struct yields identical jkt
	jwkReordered := dpop.JWKECPublicKey{
		Y:   jwk.Y,
		X:   jwk.X,
		Crv: jwk.Crv,
		Kty: jwk.Kty,
	}
	jkt2, err := dpop.ComputeJWKThumbprint(jwkReordered)
	require.NoError(t, err)
	assert.Equal(t, jkt, jkt2, "RFC 7638 thumbprint must be identical regardless of struct field order")
}

func TestNormalizeHTU_RFC9449Compliance(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "HTTPS://XG2G.HOME.MATRIXCENTRAL.DE:443/api/v3/services?foo=bar#section",
			expected: "https://xg2g.home.matrixcentral.de/api/v3/services",
		},
		{
			input:    "http://xg2g.home.matrixcentral.de:80/api/v3/services",
			expected: "http://xg2g.home.matrixcentral.de/api/v3/services",
		},
		{
			input:    "https://xg2g.home.matrixcentral.de:8089/api//double-slash/",
			expected: "https://xg2g.home.matrixcentral.de:8089/api//double-slash/",
		},
	}

	for _, tt := range tests {
		normalized := dpop.NormalizeHTU(tt.input)
		assert.Equal(t, tt.expected, normalized)
	}
}

func TestDPoPValidator_ValidProof(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	pubKey := &privKey.PublicKey
	xBytes := pubKey.X.Bytes()
	yBytes := pubKey.Y.Bytes()

	jwk := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(xBytes),
		Y:   base64.RawURLEncoding.EncodeToString(yBytes),
	}

	jktExpected, err := dpop.ComputeJWKThumbprint(jwk)
	require.NoError(t, err)

	now := time.Now().UTC()
	targetURL := "https://xg2g.home.matrixcentral.de/api/v3/services"
	accessToken := "at_dpop_opaque_test_token_12345"
	athExpected := dpop.ComputeAccessTokenHash(accessToken)

	header := dpop.ProofHeader{
		Typ: "dpop+jwt",
		Alg: "ES256",
		JWK: jwk,
	}
	payload := dpop.ProofPayload{
		JTI: "jti_proof_nonce_001",
		HTM: "GET",
		HTU: targetURL,
		IAT: now.Unix(),
		ATH: athExpected,
	}

	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signedData := headerB64 + "." + payloadB64
	hash := sha256.Sum256([]byte(signedData))
	rBig, sBig, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	require.NoError(t, err)

	rBytes := rBig.Bytes()
	sBytes := sBig.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	proofJWT := signedData + "." + base64.RawURLEncoding.EncodeToString(sigBytes)

	req := httptest.NewRequest(http.MethodGet, targetURL, nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	validator := dpop.NewDefaultValidator()
	claims, err := validator.ValidateProof(req, proofJWT, accessToken, now)
	require.NoError(t, err)
	assert.Equal(t, jktExpected, claims.JWKThumbprint)
	assert.Equal(t, "jti_proof_nonce_001", claims.Payload.JTI)

	// Replaying the exact same proof must fail with ErrReplayedJTI
	_, errReplay := validator.ValidateProof(req, proofJWT, accessToken, now)
	assert.ErrorIs(t, errReplay, dpop.ErrReplayedJTI)
}

func TestDPoPValidator_RejectsSymmetricAlgorithms(t *testing.T) {
	hB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"dpop+jwt","alg":"HS256","jwk":{"kty":"EC","crv":"P-256","x":"a","y":"b"}}`))
	pB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"jti":"jti_002","htm":"GET","htu":"https://example.com/api","iat":12345}`))
	sigB64 := base64.RawURLEncoding.EncodeToString(make([]byte, 64))

	proof := hB64 + "." + pB64 + "." + sigB64
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api", nil)

	validator := dpop.NewDefaultValidator()
	_, err := validator.ValidateProof(req, proof, "", time.Now())
	assert.ErrorIs(t, err, dpop.ErrUnsupportedAlgorithm, "HS256 symmetric algorithm must be rejected")
}

func TestReplayCache_HardCapacityEviction(t *testing.T) {
	cache := dpop.NewReplayCache(100)
	now := time.Now().UTC()

	// Push 250 unique JTIs into a 100-entry capacity cache
	for i := 0; i < 250; i++ {
		jti := fmt.Sprintf("jti_stress_%d", i)
		exp := now.Add(time.Duration(i+1) * time.Minute)
		cache.AddIfNew(jti, exp, now)
	}

	assert.LessOrEqual(t, cache.Len(), 100, "ReplayCache size MUST be hard bounded at maxSize")
}

func TestReplayCache_ReplayCannotBypassAtCapacity(t *testing.T) {
	cache := dpop.NewReplayCache(100)
	now := time.Now().UTC()

	// 1. Fill cache completely with 100 active entries
	for i := 0; i < 100; i++ {
		jti := fmt.Sprintf("jti_entry_%d", i)
		exp := now.Add(time.Duration(i+1) * time.Minute)
		ok := cache.AddIfNew(jti, exp, now)
		require.True(t, ok, "initial insert must succeed")
	}
	assert.Equal(t, 100, cache.Len())

	// 2. Re-present the oldest entry ("jti_entry_0") while cache is at capacity
	replayed := cache.AddIfNew("jti_entry_0", now.Add(15*time.Minute), now)
	assert.False(t, replayed, "Replaying existing JTI at full cache capacity MUST be detected and rejected")

	// 3. Re-present another entry ("jti_entry_50")
	replayed50 := cache.AddIfNew("jti_entry_50", now.Add(15*time.Minute), now)
	assert.False(t, replayed50, "Replaying intermediate JTI at full cache capacity MUST be detected and rejected")

	// 4. Ensure cache remains bounded
	assert.LessOrEqual(t, cache.Len(), 100)
}

func TestDPoP_RejectsPointsNotOnP256Curve(t *testing.T) {
	// (x=1, y=1) does NOT lie on the P-256 curve y^2 = x^3 - 3x + b (mod p)
	invalidJWK := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString([]byte{1}),
		Y:   base64.RawURLEncoding.EncodeToString([]byte{1}),
	}

	_, err := dpop.ComputeJWKThumbprint(invalidJWK)
	require.NoError(t, err)

	hB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"dpop+jwt","alg":"ES256","jwk":{"kty":"EC","crv":"P-256","x":"AQ","y":"AQ"}}`))
	pB64 := base64.RawURLEncoding.EncodeToString([]byte(`{"jti":"jti_invalid_curve","htm":"GET","htu":"https://example.com/api","iat":12345}`))
	sigB64 := base64.RawURLEncoding.EncodeToString(make([]byte, 64))

	proof := hB64 + "." + pB64 + "." + sigB64
	req := httptest.NewRequest(http.MethodGet, "https://example.com/api", nil)

	validator := dpop.NewDefaultValidator()
	_, err = validator.ValidateProof(req, proof, "", time.Now())
	assert.ErrorIs(t, err, dpop.ErrInvalidJWKCurve, "Points not on P-256 curve must be rejected with ErrInvalidJWKCurve")
}

// TestDPoPValidator_RejectsDEREncodedSignature pins the wire contract that native clients must
// meet. JCA (Android) and SecKeyCreateSignature (Apple) both emit ASN.1 DER by default; JWS
// ES256 (RFC 7518 §3.4) requires the raw 64-byte R||S concatenation. A client shipping DER
// authenticates against nothing, so the two encodings must be verifiably distinguishable here.
func TestDPoPValidator_RejectsDEREncodedSignature(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	jwk := dpop.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(privKey.PublicKey.X.Bytes()),
		Y:   base64.RawURLEncoding.EncodeToString(privKey.PublicKey.Y.Bytes()),
	}

	now := time.Now().UTC()
	targetURL := "https://xg2g.home.matrixcentral.de/api/v3/services"

	headerJSON, _ := json.Marshal(dpop.ProofHeader{Typ: "dpop+jwt", Alg: "ES256", JWK: jwk})
	payloadJSON, _ := json.Marshal(dpop.ProofPayload{
		JTI: "jti_der_encoding_001",
		HTM: "GET",
		HTU: targetURL,
		IAT: now.Unix(),
	})
	signedData := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	hash := sha256.Sum256([]byte(signedData))

	// DER (what an unconverted JCA / SecKey client sends) must be rejected.
	derSig, err := ecdsa.SignASN1(rand.Reader, privKey, hash[:])
	require.NoError(t, err)
	require.NotEqual(t, 64, len(derSig), "DER signature is never exactly 64 bytes for P-256")
	require.Equal(t, byte(0x30), derSig[0], "DER signature starts with a SEQUENCE tag")

	derProof := signedData + "." + base64.RawURLEncoding.EncodeToString(derSig)
	req := httptest.NewRequest(http.MethodGet, targetURL, nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	_, err = dpop.NewDefaultValidator().ValidateProof(req, derProof, "", now)
	assert.ErrorIs(t, err, dpop.ErrInvalidSignature, "DER-encoded ES256 signatures must be rejected")

	// The same signature converted to raw R||S must be accepted.
	rBig, sBig, err := ecdsa.Sign(rand.Reader, privKey, hash[:])
	require.NoError(t, err)
	rawSig := make([]byte, 64)
	rBig.FillBytes(rawSig[:32])
	sBig.FillBytes(rawSig[32:])

	rawProof := signedData + "." + base64.RawURLEncoding.EncodeToString(rawSig)
	claims, err := dpop.NewDefaultValidator().ValidateProof(req, rawProof, "", now)
	require.NoError(t, err, "raw R||S signatures must be accepted")
	assert.Equal(t, "jti_der_encoding_001", claims.Payload.JTI)
}
