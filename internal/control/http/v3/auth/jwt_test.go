package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

var secret = DefaultDecisionSecret

func TestGenerateAndVerifyStrict(t *testing.T) {
	now := time.Now().Unix()
	claims := TokenClaims{
		Iss:  "xg2g",
		Aud:  "xg2g/v3/intents",
		Sub:  "1:0:19:283D:3FB:1:C00000:0:0:0:",
		Jti:  "test-uuid-1",
		Iat:  now,
		Nbf:  now - 10,
		Exp:  now + 60, // 60s TTL
		Mode: "hlsjs",
	}

	token, err := GenerateHS256(secret, claims, "kid-v1")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	verifiedClaims, err := VerifyStrict(token, secret, "xg2g/v3/intents", "xg2g")
	if err != nil {
		t.Fatalf("Failed to verify valid strict token: %v", err)
	}
	if verifiedClaims.Mode != "hlsjs" {
		t.Errorf("Expected mode 'hlsjs', got '%s'", verifiedClaims.Mode)
	}
}

// simulateAlgNone creates a token but sets the header alg to "none" and strips the signature.
func simulateAlgNone(claims TokenClaims) string {
	header := JWTHeader{Alg: "none", Typ: "JWT"}
	hJSON, _ := json.Marshal(header)
	cJSON, _ := json.Marshal(claims)

	hBase64 := base64.RawURLEncoding.EncodeToString(hJSON)
	cBase64 := base64.RawURLEncoding.EncodeToString(cJSON)

	return hBase64 + "." + cBase64 + "."
}

func simulateDifferentSecret(claims TokenClaims, wrongSecret []byte) string {
	token, _ := GenerateHS256(wrongSecret, claims, "kid-v1")
	return token
}

func TestVerifyStrict_Failures(t *testing.T) {
	now := time.Now().Unix()
	validClaims := TokenClaims{
		Iss: "xg2g", Aud: "xg2g/v3/intents", Sub: "test-sub",
		Iat: now, Nbf: now - 10, Exp: now + 60,
	}

	tests := []struct {
		name        string
		tokenFunc   func() string
		expectedErr error
	}{
		{
			name: "alg=none rejected (sig-first)",
			tokenFunc: func() string {
				return simulateAlgNone(validClaims)
			},
			expectedErr: ErrInvalidSig, // VerifyStrict checks signature *first* by design
		},
		{
			name: "Wrong Secret / Forged Sig",
			tokenFunc: func() string {
				return simulateDifferentSecret(validClaims, []byte("wrong-secret-123"))
			},
			expectedErr: ErrInvalidSig,
		},
		{
			name: "Expired Token",
			tokenFunc: func() string {
				c := validClaims
				c.Nbf = now - 100
				c.Iat = now - 100
				// strictly speaking expired 10 seconds ago, accounting for 30s skew it would pass, so we expire it by 40s
				c.Exp = now - 40
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrTokenExpired,
		},
		{
			name: "Future NBF Token",
			tokenFunc: func() string {
				c := validClaims
				c.Nbf = now + 40 // outside 30s skew
				c.Exp = now + 100
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrTokenNotActive,
		},
		{
			name: "Issuer Mismatch",
			tokenFunc: func() string {
				c := validClaims
				c.Iss = "hacker"
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrMismatchIss,
		},
		{
			name: "Audience Mismatch",
			tokenFunc: func() string {
				c := validClaims
				c.Aud = "xg2g/other/endpoint"
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrMismatchAud,
		},
		{
			name: "Absurd TTL",
			tokenFunc: func() string {
				c := validClaims
				c.Iat = now
				c.Exp = now + 86400 // 1 day instead of 120s max
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrTokenTTLTooLong,
		},
		{
			name: "Non-positive TTL (exp <= iat)",
			tokenFunc: func() string {
				c := validClaims
				c.Iat = now
				c.Exp = now // 0 TTL
				token, _ := GenerateHS256(secret, c, "kid-1")
				return token
			},
			expectedErr: ErrTokenExpired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := tt.tokenFunc()
			_, err := VerifyStrict(token, secret, "xg2g/v3/intents", "xg2g")

			if !errors.Is(err, tt.expectedErr) {
				t.Errorf("expected err %v, got %v", tt.expectedErr, err)
			}
		})
	}
}
