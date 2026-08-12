// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package identity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryCodes(t *testing.T) {
	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	userID := "usr_test123"

	rawCodes, records, err := GenerateRecoveryCodes(userID, 10, now)
	require.NoError(t, err)
	assert.Len(t, rawCodes, 10)
	assert.Len(t, records, 10)

	for i, code := range rawCodes {
		assert.Regexp(t, `^[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}-[2-9A-HJ-NP-Z]{4}$`, code)
		assert.Equal(t, userID, records[i].UserID)
		assert.Equal(t, now, records[i].CreatedAt)
		assert.Nil(t, records[i].ConsumedAt)

		// Test exact match
		assert.True(t, MatchRecoveryCode(code, records[i].CodeHash))

		// Test case and separator insensitivity
		assert.True(t, MatchRecoveryCode(code, records[i].CodeHash))
		assert.True(t, MatchRecoveryCode(NormalizeRecoveryCode(code), records[i].CodeHash))

		// Test wrong code rejection
		assert.False(t, MatchRecoveryCode("WRONG-CODE-1234-5678", records[i].CodeHash))
	}

	// Test invalid user ID
	_, _, err = GenerateRecoveryCodes("", 10, now)
	assert.ErrorIs(t, err, ErrInvalidUserID)
}
