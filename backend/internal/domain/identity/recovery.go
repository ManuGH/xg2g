// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"
)

const (
	recoveryAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codeBlockSize    = 4
	codeBlockCount   = 4
	defaultCodeCount = 10
)

// GenerateRecoveryCodes produces n high-entropy, human-readable recovery codes
// and their corresponding hashed database records.
func GenerateRecoveryCodes(userID string, count int, now time.Time) ([]string, []RecoveryCode, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, nil, ErrInvalidUserID
	}
	if count <= 0 {
		count = defaultCodeCount
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rawCodes := make([]string, count)
	records := make([]RecoveryCode, count)

	for i := 0; i < count; i++ {
		code, err := generateSingleCode()
		if err != nil {
			return nil, nil, err
		}
		rawCodes[i] = code
		records[i] = RecoveryCode{
			CodeHash:  HashRecoveryCode(code),
			UserID:    userID,
			CreatedAt: now.UTC(),
		}
	}

	return rawCodes, records, nil
}

// HashRecoveryCode normalizes and computes the SHA-256 hash of a recovery code.
func HashRecoveryCode(code string) string {
	normalized := NormalizeRecoveryCode(code)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// NormalizeRecoveryCode strips whitespace, dashes, and converts to uppercase.
func NormalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

// MatchRecoveryCode returns true if rawCode matches the stored hash in constant time.
func MatchRecoveryCode(rawCode string, storedHash string) bool {
	computed := HashRecoveryCode(rawCode)
	if len(computed) != len(storedHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

func generateSingleCode() (string, error) {
	buf := make([]byte, codeBlockSize*codeBlockCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	blocks := make([]string, codeBlockCount)
	alphaLen := byte(len(recoveryAlphabet))

	for b := 0; b < codeBlockCount; b++ {
		blockChars := make([]byte, codeBlockSize)
		for c := 0; c < codeBlockSize; c++ {
			idx := buf[b*codeBlockSize+c] % alphaLen
			blockChars[c] = recoveryAlphabet[idx]
		}
		blocks[b] = string(blockChars)
	}

	return strings.Join(blocks, "-"), nil
}
