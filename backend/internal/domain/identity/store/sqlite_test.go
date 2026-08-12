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

func TestSQLiteIdentityStore_BootstrapTokensAndAtomicCommit(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity_bootstrap.sqlite")

	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	cfg := sqlite.DefaultConfig()

	s, err := OpenSQLite(dbPath, cfg)
	require.NoError(t, err)
	defer s.Close()

	// 1. Bootstrap Tokens
	tokenHash := "hash_test_1234567890abcdef"
	err = s.PutBootstrapToken(ctx, tokenHash, now, now.Add(15*time.Minute))
	require.NoError(t, err)

	// Consume token successfully
	consumed, err := s.ConsumeBootstrapToken(ctx, tokenHash, now.Add(2*time.Minute))
	require.NoError(t, err)
	assert.True(t, consumed)

	// Consuming second time fails
	consumed2, err := s.ConsumeBootstrapToken(ctx, tokenHash, now.Add(3*time.Minute))
	require.NoError(t, err)
	assert.False(t, consumed2)

	// Expired token fails
	expiredHash := "hash_expired_test"
	require.NoError(t, s.PutBootstrapToken(ctx, expiredHash, now, now.Add(5*time.Minute)))
	consumedExpired, err := s.ConsumeBootstrapToken(ctx, expiredHash, now.Add(10*time.Minute))
	require.NoError(t, err)
	assert.False(t, consumedExpired)

	// 2. Atomic Initial Admin Bootstrap Commit
	adminUser := &identity.User{
		ID:          "usr_admin_init",
		Username:    "admin",
		DisplayName: "Administrator",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	passkey := &identity.PasskeyCredential{
		ID:              "cred_admin_init",
		UserID:          adminUser.ID,
		PublicKey:       []byte("dummy_key_spki"),
		AttestationType: "none",
		AAGUID:          "00000000000000000000000000000000",
		SignCount:       0,
		BackupEligible:  true,
		BackupState:     true,
		Transports:      []string{"internal"},
		Nickname:        "TouchID",
		CreatedAt:       now,
	}
	_, recRecords, err := identity.GenerateRecoveryCodes(adminUser.ID, 10, now)
	require.NoError(t, err)

	webSess := &identity.WebSession{
		SessionID:    "sess_init_admin",
		UserID:       adminUser.ID,
		UserAgent:    "Safari Mac",
		LastIP:       "127.0.0.1",
		CreatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
		LastActiveAt: now,
	}

	err = s.CommitInitialAdminBootstrap(ctx, adminUser, passkey, recRecords, webSess)
	require.NoError(t, err)

	// Meta check: recovery codes not yet acknowledged
	meta, err := s.GetBootstrapMeta(ctx)
	require.NoError(t, err)
	assert.Equal(t, adminUser.ID, meta.InitialAdminID)
	assert.False(t, meta.BootstrapClosed)
	assert.Nil(t, meta.RecoveryCodesAcknowledgedAt)

	// Second attempt at initial bootstrap must fail atomically with ErrAdminAlreadyExists
	err = s.CommitInitialAdminBootstrap(ctx, adminUser, passkey, recRecords, webSess)
	require.ErrorIs(t, err, identity.ErrAdminAlreadyExists)

	// 3. Acknowledge Recovery Codes
	err = s.AcknowledgeRecoveryCodes(ctx, adminUser.ID, now.Add(time.Minute))
	require.NoError(t, err)

	metaAck, err := s.GetBootstrapMeta(ctx)
	require.NoError(t, err)
	assert.True(t, metaAck.BootstrapClosed)
	assert.NotNil(t, metaAck.RecoveryCodesAcknowledgedAt)
}

func TestSQLiteIdentityStore_ConcurrentBootstrapLocking(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")
	s, err := OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer s.Close()

	now := time.Now().UTC()
	concurrentCount := 10
	errChan := make(chan error, concurrentCount)

	for i := 0; i < concurrentCount; i++ {
		go func(idx int) {
			adminUser := &identity.User{
				ID:          "usr_admin_conc",
				Username:    "admin",
				DisplayName: "Administrator",
				Role:        identity.RoleAdmin,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			passkey := &identity.PasskeyCredential{
				ID:              "cred_conc",
				UserID:          adminUser.ID,
				PublicKey:       []byte("dummy_key_spki"),
				AttestationType: "none",
				AAGUID:          "00000000000000000000000000000000",
				SignCount:       0,
				BackupEligible:  true,
				BackupState:     true,
				Transports:      []string{"internal"},
				Nickname:        "TouchID",
				CreatedAt:       now,
			}
			_, recRecords, _ := identity.GenerateRecoveryCodes(adminUser.ID, 10, now)

			errCommit := s.CommitInitialAdminBootstrap(context.Background(), adminUser, passkey, recRecords, nil)
			errChan <- errCommit
		}(i)
	}

	var successCount, conflictCount int
	for i := 0; i < concurrentCount; i++ {
		errRes := <-errChan
		if errRes == nil {
			successCount++
		} else if assert.ErrorIs(t, errRes, identity.ErrAdminAlreadyExists) {
			conflictCount++
		}
	}

	assert.Equal(t, 1, successCount, "Exactly ONE concurrent bootstrap request must succeed")
	assert.Equal(t, 9, conflictCount, "All other concurrent bootstrap requests must fail with ErrAdminAlreadyExists")
}

func TestSQLiteIdentityStore_ReplaceRecoveryCodes(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")
	s, err := OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	user := &identity.User{
		ID:          "usr_rec_test",
		Username:    "testuser",
		DisplayName: "Test",
		Role:        identity.RoleMember,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, s.PutUser(ctx, user))

	// Initial 10 recovery codes
	raw1, recs1, err := identity.GenerateRecoveryCodes(user.ID, 10, now)
	require.NoError(t, err)
	require.NoError(t, s.PutRecoveryCodes(ctx, recs1))

	codesInit, err := s.ListRecoveryCodesByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, codesInit, 10)

	// Replace with fresh 10 recovery codes
	raw2, recs2, err := identity.GenerateRecoveryCodes(user.ID, 10, now.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, s.ReplaceRecoveryCodesForUser(ctx, user.ID, recs2))

	codesReplaced, err := s.ListRecoveryCodesByUser(ctx, user.ID)
	require.NoError(t, err)
	assert.Len(t, codesReplaced, 10, "Should have exactly 10 active codes after replacement")

	// Attempting to consume old raw code #1 must fail
	oldCodeHash := identity.HashRecoveryCode(raw1[0])
	err = s.ConsumeRecoveryCode(ctx, user.ID, oldCodeHash, now.Add(2*time.Minute))
	assert.ErrorIs(t, err, identity.ErrRecoveryCodeNotFound, "Old recovery code must be invalidated and unconsumable")

	// Attempting to consume new raw code #1 must succeed
	newCodeHash := identity.HashRecoveryCode(raw2[0])
	err = s.ConsumeRecoveryCode(ctx, user.ID, newCodeHash, now.Add(2*time.Minute))
	assert.NoError(t, err, "New recovery code must consume successfully")
}
