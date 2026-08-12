// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity_test

import (
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
