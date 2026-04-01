package household

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"crypto/pbkdf2"
)

const (
	MinPINLength = 4
	MaxPINLength = 12

	pinHashAlgorithm  = "pbkdf2-sha256"
	pinHashIterations = 200_000
	pinSaltBytes      = 16
	pinKeyBytes       = 32
)

var (
	ErrInvalidPIN        = errors.New("household pin must contain 4-12 digits")
	ErrInvalidStoredHash = errors.New("invalid stored household pin hash")
)

func ValidatePIN(pin string) error {
	normalized := strings.TrimSpace(pin)
	if len(normalized) < MinPINLength || len(normalized) > MaxPINLength {
		return ErrInvalidPIN
	}
	for _, ch := range normalized {
		if ch < '0' || ch > '9' {
			return ErrInvalidPIN
		}
	}
	return nil
}

func HashPIN(pin string) (string, error) {
	normalized := strings.TrimSpace(pin)
	if err := ValidatePIN(normalized); err != nil {
		return "", err
	}

	salt := make([]byte, pinSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate household pin salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, normalized, salt, pinHashIterations, pinKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive household pin hash: %w", err)
	}

	return fmt.Sprintf(
		"%s$%d$%s$%s",
		pinHashAlgorithm,
		pinHashIterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(key),
	), nil
}

func VerifyStoredPIN(storedHash, pin string) (bool, error) {
	parsed, err := parseStoredPINHash(storedHash)
	if err != nil {
		return false, err
	}
	if err := ValidatePIN(pin); err != nil {
		return false, err
	}

	derived, err := pbkdf2.Key(sha256.New, strings.TrimSpace(pin), parsed.salt, parsed.iterations, len(parsed.hash))
	if err != nil {
		return false, fmt.Errorf("derive household pin hash: %w", err)
	}

	return subtle.ConstantTimeCompare(derived, parsed.hash) == 1, nil
}

func ValidateStoredPINHash(storedHash string) error {
	trimmed := strings.TrimSpace(storedHash)
	if trimmed == "" {
		return nil
	}

	_, err := parseStoredPINHash(trimmed)
	return err
}

type storedPINHash struct {
	iterations int
	salt       []byte
	hash       []byte
}

func parseStoredPINHash(storedHash string) (storedPINHash, error) {
	parts := strings.Split(strings.TrimSpace(storedHash), "$")
	if len(parts) != 4 {
		return storedPINHash{}, ErrInvalidStoredHash
	}
	if parts[0] != pinHashAlgorithm {
		return storedPINHash{}, ErrInvalidStoredHash
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return storedPINHash{}, ErrInvalidStoredHash
	}

	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return storedPINHash{}, ErrInvalidStoredHash
	}

	hash, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(hash) == 0 {
		return storedPINHash{}, ErrInvalidStoredHash
	}

	return storedPINHash{
		iterations: iterations,
		salt:       salt,
		hash:       hash,
	}, nil
}
