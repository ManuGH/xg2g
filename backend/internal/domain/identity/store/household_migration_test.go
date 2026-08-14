// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHouseholdMigration_ExistingInstallationIsIdempotentAndPreservesAuth(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "legacy_identity.sqlite")

	// 1. Initial Store Initialization & Setup of Legacy Admin
	s1, err := store.OpenStateStore("sqlite", dbPath)
	require.NoError(t, err)

	now := time.Now().UTC()
	adminUser := identity.User{
		ID:          "usr_legacy_admin",
		Username:    "admin",
		DisplayName: "System Papa Admin",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	require.NoError(t, s1.PutUser(context.Background(), &adminUser))

	// Register a Passkey Credential for existing admin
	cred := &identity.PasskeyCredential{
		ID:              "cred_legacy_123",
		UserID:          adminUser.ID,
		PublicKey:       []byte("dummy_pk"),
		AttestationType: "none",
		AAGUID:          "00000000-0000-0000-0000-000000000000",
		Nickname:        "Papa TouchID",
		CreatedAt:       now,
	}
	require.NoError(t, s1.PutPasskey(context.Background(), cred))

	// Save web session
	sess := &identity.WebSession{
		SessionID:    "sess_legacy_abc",
		UserID:       adminUser.ID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
		LastActiveAt: now,
	}
	require.NoError(t, s1.PutSession(context.Background(), sess))

	require.NoError(t, s1.Close())

	// 2. Re-Open Store (Triggers Migration)
	s2, err := store.OpenStateStore("sqlite", dbPath)
	require.NoError(t, err)

	// Verify Admin User & Credentials intact
	uFetch, err := s2.GetUser(context.Background(), adminUser.ID)
	require.NoError(t, err)
	assert.Equal(t, "admin", uFetch.Username)

	creds, err := s2.ListPasskeysByUser(context.Background(), adminUser.ID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, "cred_legacy_123", creds[0].ID)

	sessFetch, err := s2.GetSession(context.Background(), "sess_legacy_abc")
	require.NoError(t, err)
	assert.Equal(t, adminUser.ID, sessFetch.UserID)

	require.NoError(t, s2.Close())

	// 3. Re-Open Store 3 Times (Idempotency Check on Restarts & Upgrades)
	for i := 0; i < 3; i++ {
		sLoop, err := store.OpenStateStore("sqlite", dbPath)
		require.NoError(t, err, "Re-opening store on iteration %d must not fail", i)

		uLoop, err := sLoop.GetUser(context.Background(), adminUser.ID)
		require.NoError(t, err)
		assert.Equal(t, identity.RoleAdmin, uLoop.Role)

		require.NoError(t, sLoop.Close())
	}
}
