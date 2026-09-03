// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity_test

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgon2idPasswordHashingAndVerification(t *testing.T) {
	// Short password rejected
	_, err := identity.HashPassword("short")
	assert.ErrorIs(t, err, identity.ErrPasswordTooShort)

	// Valid password hashing
	hash, err := identity.HashPassword("superSecretPass123!")
	require.NoError(t, err)
	assert.Contains(t, hash, "$argon2id$v=19$m=19456,t=2,p=1$")

	// Correct password verification
	assert.True(t, identity.VerifyPassword("superSecretPass123!", hash))

	// Wrong password verification
	assert.False(t, identity.VerifyPassword("wrongPassword123!", hash))
	assert.False(t, identity.VerifyPassword("", hash))
}

func TestProfilePINHashingAndVerification(t *testing.T) {
	pinHash, err := identity.HashProfilePIN("1234")
	require.NoError(t, err)
	assert.Contains(t, pinHash, "$argon2id$")

	assert.True(t, identity.VerifyProfilePIN("1234", pinHash))
	assert.False(t, identity.VerifyProfilePIN("9999", pinHash))
	assert.False(t, identity.VerifyProfilePIN("", pinHash))
}

func TestVerifyPassword_ParameterBoundingDefenseInDepth(t *testing.T) {
	// Standard valid salt (16 bytes) and hash (32 bytes) base64
	validSalt := "c29tZXNhbHQxMjM0NTY3OA"                      // 16 bytes decoded
	validHash := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 bytes decoded

	tests := []struct {
		name       string
		hashString string
		wantValid  bool
	}{
		{
			name:       "ExorbitantMemoryDoS",
			hashString: "$argon2id$v=19$m=4294967295,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "TooSmallMemory",
			hashString: "$argon2id$v=19$m=1024,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "ExorbitantIterationsDoS",
			hashString: "$argon2id$v=19$m=19456,t=1000,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "ZeroIterations",
			hashString: "$argon2id$v=19$m=19456,t=0,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "ExorbitantThreadsDoS",
			hashString: "$argon2id$v=19$m=19456,t=2,p=255$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "ShortSalt",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$c2hvcnQ$" + validHash,
			wantValid:  false,
		},
		{
			name:       "InvalidScheme",
			hashString: "$bcrypt$v=19$m=19456,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "MalformedParams",
			hashString: "$argon2id$v=19$garbage$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "WrongVersionLower",
			hashString: "$argon2id$v=18$m=19456,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "WrongVersionHigher",
			hashString: "$argon2id$v=20$m=19456,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "MissingLeadingDollar",
			hashString: "argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
		{
			name:       "ExtraFields",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$" + validHash + "$extra",
			wantValid:  false,
		},
		{
			name:       "TooFewFields",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$" + validSalt,
			wantValid:  false,
		},
		{
			name:       "OversizedPHCString",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$" + validHash + strings.Repeat("A", 500),
			wantValid:  false,
		},
		{
			name:       "MalformedBase64Salt",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$invalid!base64!salt$" + validHash,
			wantValid:  false,
		},
		{
			name:       "MalformedBase64Hash",
			hashString: "$argon2id$v=19$m=19456,t=2,p=1$" + validSalt + "$invalid!base64!hash",
			wantValid:  false,
		},
		{
			name:       "ThreadOverflow",
			hashString: "$argon2id$v=19$m=19456,t=2,p=999$" + validSalt + "$" + validHash,
			wantValid:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := identity.VerifyPassword("anyPassword123!", tc.hashString)
			assert.Equal(t, tc.wantValid, res, "hash with invalid parameters must fail-closed")
		})
	}
}
