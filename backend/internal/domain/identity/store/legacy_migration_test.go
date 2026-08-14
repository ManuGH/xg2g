// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestLegacySQLiteDatabaseAutoMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "legacy_v30.db")

	// 1. Create a raw legacy v3.0 database (without household tables)
	rawDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	legacyDDL := `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);
		INSERT INTO users (id, username, password_hash, role, created_at)
		VALUES ('user_legacy_admin', 'admin', 'hash_legacy_123', 'admin', CURRENT_TIMESTAMP);
	`
	_, err = rawDB.Exec(legacyDDL)
	require.NoError(t, err)
	require.NoError(t, rawDB.Close())

	// 2. Open legacy DB with NewSQLiteStore -> Triggers auto-migration
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sqliteStore, err := NewSQLiteStore(dbPath)
	require.NoError(t, err)
	defer sqliteStore.Close()

	// 3. Verify auto-migrated household structures
	mem, err := sqliteStore.GetHouseholdMembership(ctx, "default_household", "user_legacy_admin")
	require.NoError(t, err)
	require.NotNil(t, mem)
	require.Equal(t, identity.RoleAdmin, mem.Role)

	// 4. Verify profile creation under auto-migrated household
	prof := &identity.Profile{
		ID:              "prof_admin_1",
		HouseholdID:     "default_household",
		Name:            "Hauptnutzer",
		IsChild:         false,
		CreatedByUserID: "user_legacy_admin",
		CreatedAt:       time.Now().UTC(),
	}
	err = sqliteStore.PutProfile(ctx, prof, &identity.ProfilePolicy{ProfileID: prof.ID, MaturityLevel: 18, DVRAllowed: true})
	require.NoError(t, err)

	profiles, err := sqliteStore.ListProfilesByHousehold(ctx, "default_household")
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, "prof_admin_1", profiles[0].ID)
}
