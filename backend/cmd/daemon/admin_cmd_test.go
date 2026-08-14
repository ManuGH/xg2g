// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminCLI_FullLifecycle(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")

	// 1. Initial Status on empty DB
	code := runAdminCLI([]string{"status", "--db", dbPath})
	assert.Equal(t, 0, code)

	// 2. Generate Bootstrap Token via CLI
	code = runAdminCLI([]string{"bootstrap-token", "--db", dbPath})
	assert.Equal(t, 0, code)

	// Verify token in DB
	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer s.Close()

	// Simulate bootstrap completion
	now := time.Now().UTC()
	adminUser := &identity.User{
		ID:          "usr_admin_cli_test",
		Username:    "admin",
		DisplayName: "Administrator",
		Role:        identity.RoleAdmin,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	passkey := &identity.PasskeyCredential{
		ID:              "cred_cli_test",
		UserID:          adminUser.ID,
		PublicKey:       []byte("spki_bytes"),
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

	err = s.CommitInitialAdminBootstrap(context.Background(), adminUser, passkey, recRecords, nil)
	require.NoError(t, err)

	// 3. Second bootstrap-token generation fails because admin already exists
	code = runAdminCLI([]string{"bootstrap-token", "--db", dbPath})
	assert.Equal(t, 1, code)

	// 4. Generate Emergency Recovery Codes via CLI
	code = runAdminCLI([]string{"generate-recovery-codes", "--user", "admin", "--db", dbPath})
	assert.Equal(t, 0, code)

	// 5. Check status
	code = runAdminCLI([]string{"status", "--db", dbPath})
	assert.Equal(t, 0, code)
}
