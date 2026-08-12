// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 19 * 1024 // 19 MiB
	argonIterations  = 2
	argonParallelism = 1
	argonSaltLen     = 16
	argonKeyLen      = 32
)

// HashPassword hashes a plain text password using OWASP-compliant Argon2id settings.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", ErrPasswordTooShort
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate argon2 salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism, b64Salt, b64Hash)

	return encoded, nil
}

// VerifyPassword verifies a plain text password against an Argon2id encoded hash string.
func VerifyPassword(password, encodedHash string) bool {
	if password == "" || encodedHash == "" {
		return false
	}

	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	var memory, time, threads uint32
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedHash) == 0 {
		return false
	}

	_ = version // Unused

	computedHash := argon2.IDKey([]byte(password), salt, time, memory, byte(threads), uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1
}

// HashProfilePIN hashes a 4-digit profile exit PIN.
func HashProfilePIN(pin string) (string, error) {
	pin = strings.TrimSpace(pin)
	if len(pin) == 0 {
		return "", errors.New("pin must not be empty")
	}
	return HashPassword("profile_pin_" + pin)
}

// VerifyProfilePIN verifies a 4-digit profile exit PIN against its hash.
func VerifyProfilePIN(pin, encodedHash string) bool {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return false
	}
	return VerifyPassword("profile_pin_"+pin, encodedHash)
}
