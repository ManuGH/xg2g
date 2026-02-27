package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// JWTError classifications for strict HTTP 401/403 mapping.
var (
	ErrTokenMissing    = errors.New("token missing")
	ErrTokenMalformed  = errors.New("token malformed")
	ErrInvalidAlg      = errors.New("invalid algorithm: must be HS256")
	ErrInvalidSig      = errors.New("invalid signature")
	ErrTokenExpired    = errors.New("token expired")
	ErrTokenNotActive  = errors.New("token not yet active (nbf)")
	ErrMissingIAT      = errors.New("missing iat claim")
	ErrMissingExp      = errors.New("missing exp claim")
	ErrMissingNbf      = errors.New("missing nbf claim")
	ErrMismatchIss     = errors.New("issuer mismatch")
	ErrMismatchAud     = errors.New("audience mismatch")
	ErrMismatchSub     = errors.New("subject mismatch (serviceRef)")
	ErrMismatchScope   = errors.New("scope mismatch")
	ErrMismatchMode    = errors.New("mode mismatch")
	ErrMismatchCapHash = errors.New("capabilities hash mismatch")
	ErrTokenTTLTooLong = errors.New("token ttl exceeds maximum allowed policy duration (120s)")
)

type TokenClaims struct {
	Iss     string `json:"iss"`
	Aud     string `json:"aud"`
	Sub     string `json:"sub"`
	Jti     string `json:"jti"`
	Iat     int64  `json:"iat"`
	Nbf     int64  `json:"nbf"`
	Exp     int64  `json:"exp,omitempty"`
	Mode    string `json:"mode,omitempty"`
	CapHash string `json:"capHash,omitempty"`
	TraceID string `json:"traceId,omitempty"`
}

type JWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

// GenerateHS256 generates a strict HS256 JWT.
func GenerateHS256(secret []byte, claims TokenClaims, kid string) (string, error) {
	header := JWTHeader{
		Alg: "HS256",
		Typ: "JWT",
		Kid: kid,
	}

	hJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	hBase64 := base64.RawURLEncoding.EncodeToString(hJSON)
	cBase64 := base64.RawURLEncoding.EncodeToString(cJSON)

	payload := hBase64 + "." + cBase64

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)
	sBase64 := base64.RawURLEncoding.EncodeToString(signature)

	return payload + "." + sBase64, nil
}

// VerifyStrict verifies an HS256 JWT according to operator-grade rules.
func VerifyStrict(token string, secret []byte, expectedAud, expectedIss string) (*TokenClaims, error) {
	return VerifyStrictAt(token, secret, expectedAud, expectedIss, time.Now().Unix())
}

// VerifyStrictAt is like VerifyStrict but allows providing a custom 'now' timestamp.
// Useful for deterministic testing and clock-drift simulation.
func VerifyStrictAt(token string, secret []byte, expectedAud, expectedIss string, now int64) (*TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	// 1. Check Signature First (Prevents timing attacks on claims)
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidSig
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return nil, ErrInvalidSig
	}

	// 2. Strict Header Validation
	hJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	var header JWTHeader
	if err := json.Unmarshal(hJSON, &header); err != nil {
		return nil, ErrTokenMalformed
	}
	// "alg=none" and others are strictly rejected here.
	if header.Alg != "HS256" {
		return nil, ErrInvalidAlg
	}

	// 3. Claims Validation
	cJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenMalformed
	}
	var claims TokenClaims
	if err := json.Unmarshal(cJSON, &claims); err != nil {
		return nil, ErrTokenMalformed
	}

	// Required logical claims (401 semantics if these fail)
	if claims.Iat == 0 {
		return nil, ErrMissingIAT
	}
	if claims.Exp == 0 {
		return nil, ErrMissingExp
	}
	if claims.Nbf == 0 {
		return nil, ErrMissingNbf
	}

	// Time boundaries with 30s skew tolerance
	const skew = 30
	if now < (claims.Nbf - skew) {
		return nil, ErrTokenNotActive
	}
	if now > (claims.Exp + skew) {
		return nil, ErrTokenExpired
	}

	// Ensure the token has a valid positive time window and isn't absurdly long
	ttl := claims.Exp - claims.Iat
	if ttl <= 0 {
		return nil, ErrTokenExpired // Non-positive window is logically expired/invalid
	}
	if ttl > 120 {
		return nil, ErrTokenTTLTooLong
	}

	// Semantic Claims Setup
	if claims.Iss != expectedIss {
		return nil, ErrMismatchIss
	}
	if claims.Aud != expectedAud {
		return nil, ErrMismatchAud
	}

	return &claims, nil
}
