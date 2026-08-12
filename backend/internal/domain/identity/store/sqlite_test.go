// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteIdentityStore_LifecycleAndPersistence(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")

	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	cfg := sqlite.DefaultConfig()

	// 1. Open Database
	s, err := OpenSQLite(dbPath, cfg)
	require.NoError(t, err)

	// 2. Create User
	user := &identity.User{
		ID:          "usr_manuel",
		Username:    "manuel",
		DisplayName: "Manuel",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = s.PutUser(ctx, user)
	require.NoError(t, err)

	gotUser, err := s.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.Username, gotUser.Username)
	assert.Equal(t, user.Role, gotUser.Role)

	// 3. Register Passkey
	passkey := &identity.PasskeyCredential{
		ID:              "cred_12345",
		UserID:          user.ID,
		PublicKey:       []byte("fake_spki_der_bytes"),
		AttestationType: "none",
		AAGUID:          "0102030405060708090a0b0c0d0e0f10",
		SignCount:       10,
		BackupEligible:  true,
		BackupState:     true,
		Transports:      []string{"internal"},
		Nickname:        "MacBook Touch ID",
		CreatedAt:       now,
	}
	err = s.PutPasskey(ctx, passkey)
	require.NoError(t, err)

	gotPasskey, err := s.GetPasskey(ctx, passkey.ID)
	require.NoError(t, err)
	assert.Equal(t, passkey.UserID, gotPasskey.UserID)
	assert.True(t, gotPasskey.BackupState)
	assert.Equal(t, uint32(10), gotPasskey.SignCount)

	// Update Passkey signCount
	err = s.UpdatePasskey(ctx, passkey.ID, 15, true, now.Add(time.Minute))
	require.NoError(t, err)
	gotPasskeyUpdated, err := s.GetPasskey(ctx, passkey.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(15), gotPasskeyUpdated.SignCount)

	// 4. Create Recovery Codes
	_, recCodes, err := identity.GenerateRecoveryCodes(user.ID, 3, now)
	require.NoError(t, err)
	err = s.PutRecoveryCodes(ctx, recCodes)
	require.NoError(t, err)

	storedRecCodes, err := s.ListRecoveryCodesByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, storedRecCodes, 3)

	// Consume one code with correct user
	err = s.ConsumeRecoveryCode(ctx, user.ID, storedRecCodes[0].CodeHash, now.Add(2*time.Minute))
	require.NoError(t, err)
	// Consuming second time should fail
	err = s.ConsumeRecoveryCode(ctx, user.ID, storedRecCodes[0].CodeHash, now.Add(3*time.Minute))
	require.ErrorIs(t, err, identity.ErrRecoveryCodeNotFound)
	// Cross-user consumption attempt must fail
	err = s.ConsumeRecoveryCode(ctx, "other_user_id", storedRecCodes[1].CodeHash, now.Add(4*time.Minute))
	require.ErrorIs(t, err, identity.ErrRecoveryCodeNotFound)

	// 5. Create Web Sessions
	sess1 := &identity.WebSession{
		SessionID:    "sess_alpha",
		UserID:       user.ID,
		UserAgent:    "Safari/19.0",
		LastIP:       "10.0.0.1",
		CreatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
		LastActiveAt: now,
	}
	sess2 := &identity.WebSession{
		SessionID:    "sess_beta",
		UserID:       user.ID,
		UserAgent:    "Safari iOS",
		LastIP:       "10.0.0.2",
		CreatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
		LastActiveAt: now,
	}
	require.NoError(t, s.PutSession(ctx, sess1))
	require.NoError(t, s.PutSession(ctx, sess2))

	gotSess1, err := s.GetSession(ctx, sess1.SessionID)
	require.NoError(t, err)
	assert.True(t, gotSess1.IsActive(now))

	// Touch Session (simulate network roaming from 10.0.0.1 to 198.51.100.42)
	err = s.TouchSession(ctx, sess1.SessionID, now.Add(time.Hour), "Safari/19.0", "198.51.100.42")
	require.NoError(t, err)
	touchedSess, err := s.GetSession(ctx, sess1.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "198.51.100.42", touchedSess.LastIP)

	// Revoke other sessions (e.g. from sess1 revoke sess2)
	err = s.RevokeOtherSessions(ctx, user.ID, sess1.SessionID, now.Add(2*time.Hour))
	require.NoError(t, err)

	gotSess2, err := s.GetSession(ctx, sess2.SessionID)
	require.NoError(t, err)
	assert.False(t, gotSess2.IsActive(now))
	assert.NotNil(t, gotSess2.RevokedAt)

	// 6. Close and Re-open to prove SQLite persistence (Daemon Restart Test)
	require.NoError(t, s.Close())

	reopenedStore, err := OpenSQLite(dbPath, cfg)
	require.NoError(t, err)
	defer reopenedStore.Close()

	persistedUser, err := reopenedStore.GetUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, user.DisplayName, persistedUser.DisplayName)

	persistedPasskey, err := reopenedStore.GetPasskey(ctx, passkey.ID)
	require.NoError(t, err)
	assert.Equal(t, uint32(15), persistedPasskey.SignCount)

	persistedSess, err := reopenedStore.GetSession(ctx, sess1.SessionID)
	require.NoError(t, err)
	assert.True(t, persistedSess.IsActive(now.Add(3*time.Hour)))
}
