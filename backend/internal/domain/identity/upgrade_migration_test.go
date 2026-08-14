// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package identity_test

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

// TestUpgradeMigration_PreservesExistingAdminIdentity verifies that upgrading an existing
// installation with pre-v1 admin credentials, passkeys, and web sessions performs an in-place
// lossless migration without inventing a new admin or resetting credentials.
func TestUpgradeMigration_PreservesExistingAdminIdentity(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy_xg2g.db")

	sqlStore, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)

	now := time.Now()

	// 1. Seed legacy pre-v1 state with an established Admin user and session
	legacyAdmin := &identity.User{
		ID:        "usr_legacy_admin_123",
		Username:  "admin",
		Role:      identity.RoleAdmin,
		CreatedAt: now.Add(-24 * time.Hour),
	}

	legacySession := &identity.WebSession{
		SessionID: "sess_legacy_token_999",
		UserID:    legacyAdmin.ID,
		UserAgent: "Mozilla/5.0 (Macintosh)",
		LastIP:    "127.0.0.1",
		ExpiresAt: now.Add(7 * 24 * time.Hour),
		CreatedAt: now,
	}

	passkey := &identity.PasskeyCredential{
		ID:              "pk_legacy_1",
		UserID:          legacyAdmin.ID,
		PublicKey:       []byte("dummy_pk"),
		AttestationType: "none",
		AAGUID:          "0000000000000000",
		SignCount:       1,
		CreatedAt:       now,
	}

	recCode := identity.RecoveryCode{
		CodeHash:  "dummy_hash",
		UserID:    legacyAdmin.ID,
		CreatedAt: now,
	}

	// 1. Commit initial admin bootstrap for legacy admin
	err = sqlStore.CommitInitialAdminBootstrap(ctx, legacyAdmin, passkey, []identity.RecoveryCode{recCode}, legacySession)
	require.NoError(t, err)

	// Create default household membership
	mem := &identity.HouseholdMembership{
		HouseholdID: "default_household",
		UserID:      legacyAdmin.ID,
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
	}
	err = sqlStore.PutHouseholdMembership(ctx, mem)
	require.NoError(t, err)

	// 2. Initialize new Identity Service (simulating server upgrade startup)
	svc := identity.NewService(identity.Config{
		RPName:         "xg2g",
		RPID:           "localhost",
		ExpectedOrigin: "http://localhost:8088",
	}, sqlStore)
	defer svc.Close()

	// 3. Verify bootstrap status does NOT report setup_required, but setup_in_progress (awaiting v1 acknowledgment)
	bootState, err := svc.GetBootstrapStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, identity.BootstrapStateSetupInProgress, bootState, "Existing admin upgrade MUST NOT trigger setup_required")

	// 4. Verify existing admin user & session remain intact
	validatedSess, user, err := svc.ValidateWebSession(ctx, "sess_legacy_token_999", "Mozilla/5.0 (Macintosh)", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, validatedSess)
	require.NotNil(t, user)
	assert.Equal(t, "usr_legacy_admin_123", user.ID)
	assert.Equal(t, identity.RoleAdmin, user.Role)

	// 5. Verify Household auto-creation and membership assignment
	memRes, err := sqlStore.GetHouseholdMembership(ctx, "default_household", user.ID)
	require.NoError(t, err)
	require.NotNil(t, memRes)
	assert.Equal(t, identity.RoleAdmin, memRes.Role)

	// 6. Acknowledge recovery codes and verify BootstrapStateReady & IsIdentityReady
	err = svc.AcknowledgeRecoveryCodes(ctx, user.ID)
	require.NoError(t, err)

	ready, err := svc.IsIdentityReady(ctx)
	require.NoError(t, err)
	assert.True(t, ready, "Acknowledged admin MUST be marked identity ready")

	bootStateAfterAck, err := svc.GetBootstrapStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, identity.BootstrapStateReady, bootStateAfterAck)
}
