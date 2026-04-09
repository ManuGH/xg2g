package household

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func TestSqliteStoreRoundTrip(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	maxFSK := 12
	input := Profile{
		ID:                  "child-room",
		Name:                "Kinderzimmer",
		Kind:                ProfileKindChild,
		MaxFSK:              &maxFSK,
		AllowedBouquets:     []string{"kids"},
		AllowedServiceRefs:  []string{"1:0:1:ABCD"},
		FavoriteServiceRefs: []string{"1:0:1:FFFF"},
		Permissions: Permissions{
			DVRPlayback: true,
			DVRManage:   false,
			Settings:    false,
		},
	}

	if err := store.Upsert(context.Background(), input); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	profile, ok, err := store.Get(context.Background(), "child-room")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if !ok {
		t.Fatal("expected stored profile to exist")
	}
	if profile.ID != input.ID || profile.Name != input.Name {
		t.Fatalf("unexpected stored profile: %#v", profile)
	}
	if len(profile.AllowedServiceRefs) != 1 || profile.AllowedServiceRefs[0] != "1:0:1:ABCD" {
		t.Fatalf("unexpected service refs: %#v", profile.AllowedServiceRefs)
	}

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles including default, got %d", len(profiles))
	}
}

func TestSqliteStoreSeedsDefaultProfileOnInit(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	profile, ok, err := store.Get(context.Background(), DefaultProfileID)
	if err != nil {
		t.Fatalf("get default profile: %v", err)
	}
	if !ok {
		t.Fatal("expected sqlite store to seed default profile")
	}
	if profile.ID != DefaultProfileID {
		t.Fatalf("expected default profile id %q, got %q", DefaultProfileID, profile.ID)
	}
}

func TestSqliteStore_ReopenDeleteAndReseedDefaultProfile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "household.sqlite")

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	if err := store.Upsert(context.Background(), Profile{
		ID:   "room-1",
		Name: "Wohnzimmer",
		Kind: ProfileKindAdult,
	}); err != nil {
		t.Fatalf("upsert room-1: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	profile, ok, err := store.Get(context.Background(), "room-1")
	if err != nil {
		t.Fatalf("get room-1 after reopen: %v", err)
	}
	if !ok || profile.ID != "room-1" {
		t.Fatalf("expected room-1 after reopen, got %#v ok=%v", profile, ok)
	}
	if err := store.Delete(context.Background(), "room-1"); err != nil {
		t.Fatalf("delete room-1: %v", err)
	}
	if err := store.Delete(context.Background(), DefaultProfileID); err != nil {
		t.Fatalf("delete default profile: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close mutated sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("second reopen sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close final sqlite store: %v", err)
		}
	}()

	_, ok, err = store.Get(context.Background(), "room-1")
	if err != nil {
		t.Fatalf("get room-1 after delete: %v", err)
	}
	if ok {
		t.Fatal("expected room-1 to stay deleted after reopen")
	}

	defaultProfile, ok, err := store.Get(context.Background(), DefaultProfileID)
	if err != nil {
		t.Fatalf("get reseeded default profile: %v", err)
	}
	if !ok {
		t.Fatal("expected default profile to be reseeded on reopen")
	}
	if defaultProfile.ID != DefaultProfileID {
		t.Fatalf("expected reseeded default profile id %q, got %q", DefaultProfileID, defaultProfile.ID)
	}

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list final profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != DefaultProfileID {
		t.Fatalf("expected only reseeded default profile, got %#v", profiles)
	}
}

func TestSqliteStore_MigratesLegacyV1RowsToCanonicalProfiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "household.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE household_profiles (
			id TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			max_fsk INTEGER,
			allowed_bouquets_json TEXT NOT NULL,
			allowed_service_refs_json TEXT NOT NULL,
			favorite_service_refs_json TEXT NOT NULL,
			dvr_playback INTEGER NOT NULL,
			dvr_manage INTEGER NOT NULL,
			settings_access INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		);
		INSERT INTO household_profiles(
			id, name, kind, max_fsk, allowed_bouquets_json, allowed_service_refs_json, favorite_service_refs_json,
			dvr_playback, dvr_manage, settings_access, updated_at_ms
		) VALUES
			('Household-Default', '  Zuhause  ', 'adult', NULL, '[]', '[]', '[]', 1, 1, 1, 1000),
			('Child-Room', '  Kinderzimmer  ', 'CHILD', 12, '[" Kids ","kids",""]', '["1:0:1:abcd:","1:0:1:ABCD",""]', '["1:0:1:ffff:","1:0:1:FFFF"]', 1, 0, 0, 2000);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed legacy household rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated household store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated household store: %v", err)
		}
	}()

	var version int
	if err := store.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("query household schema version: %v", err)
	}
	if version != sqliteSchemaVersion {
		t.Fatalf("expected household schema version %d, got %d", sqliteSchemaVersion, version)
	}

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list migrated profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected two canonical profiles after migration, got %#v", profiles)
	}
	if profiles[0].ID != DefaultProfileID || profiles[0].Name != "Zuhause" {
		t.Fatalf("expected migrated default profile first, got %#v", profiles[0])
	}
	if profiles[1].ID != "child-room" {
		t.Fatalf("expected migrated child-room profile, got %#v", profiles[1])
	}

	child, ok, err := store.Get(context.Background(), "child-room")
	if err != nil {
		t.Fatalf("get migrated child-room: %v", err)
	}
	if !ok {
		t.Fatal("expected migrated child-room profile")
	}
	if child.Kind != ProfileKindChild {
		t.Fatalf("expected child kind after migration, got %q", child.Kind)
	}
	if len(child.AllowedBouquets) != 1 || child.AllowedBouquets[0] != "kids" {
		t.Fatalf("expected canonical bouquets after migration, got %#v", child.AllowedBouquets)
	}
	if len(child.AllowedServiceRefs) != 1 || child.AllowedServiceRefs[0] != "1:0:1:ABCD" {
		t.Fatalf("expected canonical service refs after migration, got %#v", child.AllowedServiceRefs)
	}
	if len(child.FavoriteServiceRefs) != 1 || child.FavoriteServiceRefs[0] != "1:0:1:FFFF" {
		t.Fatalf("expected canonical favorites after migration, got %#v", child.FavoriteServiceRefs)
	}

	child.Name = "Kinderzimmer Nord"
	if err := store.Upsert(context.Background(), child); err != nil {
		t.Fatalf("rewrite migrated child profile: %v", err)
	}

	var defaultCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM household_profiles WHERE id = ?`, DefaultProfileID).Scan(&defaultCount); err != nil {
		t.Fatalf("count canonical default profile rows: %v", err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected one canonical default profile row, got %d", defaultCount)
	}
	var legacyIDCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM household_profiles WHERE id IN ('Household-Default', 'Child-Room')`).Scan(&legacyIDCount); err != nil {
		t.Fatalf("count legacy profile ids: %v", err)
	}
	if legacyIDCount != 0 {
		t.Fatalf("expected legacy profile ids to be normalized away, got %d", legacyIDCount)
	}

	var bouquetsJSON string
	var serviceRefsJSON string
	var favoritesJSON string
	if err := store.DB.QueryRow(`
		SELECT allowed_bouquets_json, allowed_service_refs_json, favorite_service_refs_json
		FROM household_profiles
		WHERE id = 'child-room'
	`).Scan(&bouquetsJSON, &serviceRefsJSON, &favoritesJSON); err != nil {
		t.Fatalf("query canonical migrated row: %v", err)
	}
	if bouquetsJSON != `["kids"]` {
		t.Fatalf("expected canonical bouquets JSON, got %s", bouquetsJSON)
	}
	if serviceRefsJSON != `["1:0:1:ABCD"]` {
		t.Fatalf("expected canonical service refs JSON, got %s", serviceRefsJSON)
	}
	if favoritesJSON != `["1:0:1:FFFF"]` {
		t.Fatalf("expected canonical favorites JSON, got %s", favoritesJSON)
	}
}

func TestSqliteStore_MigrationCollisionKeepsNewestCanonicalProfile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "household.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE household_profiles (
			id TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			max_fsk INTEGER,
			allowed_bouquets_json TEXT NOT NULL,
			allowed_service_refs_json TEXT NOT NULL,
			favorite_service_refs_json TEXT NOT NULL,
			dvr_playback INTEGER NOT NULL,
			dvr_manage INTEGER NOT NULL,
			settings_access INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		);
		INSERT INTO household_profiles(
			id, name, kind, max_fsk, allowed_bouquets_json, allowed_service_refs_json, favorite_service_refs_json,
			dvr_playback, dvr_manage, settings_access, updated_at_ms
		) VALUES
			('Household-Default', 'Haushalt', 'adult', NULL, '[]', '[]', '[]', 1, 1, 1, 1000),
			('Child-Room', 'Kinderzimmer Alt', 'child', 6, '["kids-alt"]', '["1:0:1:AAAA"]', '["1:0:1:BBBB"]', 0, 0, 0, 1500),
			(' child-room ', ' Kinderzimmer Neu ', 'CHILD', 12, '[" Kids-Neu ","kids-neu"]', '["1:0:1:cccc:","1:0:1:CCCC"]', '["1:0:1:dddd:","1:0:1:DDDD"]', 1, 0, 0, 2500);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed colliding legacy household rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated household store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated household store: %v", err)
		}
	}()

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list migrated profiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected default plus one canonical child profile, got %#v", profiles)
	}

	child, ok, err := store.Get(context.Background(), "child-room")
	if err != nil {
		t.Fatalf("get migrated child-room: %v", err)
	}
	if !ok {
		t.Fatal("expected canonical child-room after collision migration")
	}
	if child.Name != "Kinderzimmer Neu" {
		t.Fatalf("expected newest colliding profile to win, got %#v", child)
	}
	if child.MaxFSK == nil || *child.MaxFSK != 12 {
		t.Fatalf("expected newest colliding max FSK 12, got %v", child.MaxFSK)
	}
	if len(child.AllowedBouquets) != 1 || child.AllowedBouquets[0] != "kids-neu" {
		t.Fatalf("expected newest colliding bouquets, got %#v", child.AllowedBouquets)
	}
	if len(child.AllowedServiceRefs) != 1 || child.AllowedServiceRefs[0] != "1:0:1:CCCC" {
		t.Fatalf("expected newest colliding service refs, got %#v", child.AllowedServiceRefs)
	}
	if len(child.FavoriteServiceRefs) != 1 || child.FavoriteServiceRefs[0] != "1:0:1:DDDD" {
		t.Fatalf("expected newest colliding favorite refs, got %#v", child.FavoriteServiceRefs)
	}

	var rowCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM household_profiles WHERE id = 'child-room'`).Scan(&rowCount); err != nil {
		t.Fatalf("count canonical child-room rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly one canonical child-room row after collision migration, got %d", rowCount)
	}
}

func TestSqliteStore_MigrationIsIdempotentOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "household.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE household_profiles (
			id TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			max_fsk INTEGER,
			allowed_bouquets_json TEXT NOT NULL,
			allowed_service_refs_json TEXT NOT NULL,
			favorite_service_refs_json TEXT NOT NULL,
			dvr_playback INTEGER NOT NULL,
			dvr_manage INTEGER NOT NULL,
			settings_access INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		);
		INSERT INTO household_profiles(
			id, name, kind, max_fsk, allowed_bouquets_json, allowed_service_refs_json, favorite_service_refs_json,
			dvr_playback, dvr_manage, settings_access, updated_at_ms
		) VALUES
			('Household-Default', '  Zuhause  ', 'adult', NULL, '[]', '[]', '[]', 1, 1, 1, 1000),
			('Child-Room', '  Kinderzimmer  ', 'CHILD', 12, '[" Kids ","kids",""]', '["1:0:1:abcd:","1:0:1:ABCD",""]', '["1:0:1:ffff:","1:0:1:FFFF"]', 1, 0, 0, 2000);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed legacy household rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated household store: %v", err)
	}
	firstSnapshot := snapshotHouseholdRows(t, store.DB)
	if err := store.Close(); err != nil {
		t.Fatalf("close first migrated household store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen migrated household store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close reopened household store: %v", err)
		}
	}()

	secondSnapshot := snapshotHouseholdRows(t, store.DB)
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatalf("expected migration reopen to be idempotent\nfirst:  %#v\nsecond: %#v", firstSnapshot, secondSnapshot)
	}
}

type rawHouseholdRow struct {
	ID                      string
	Name                    string
	Kind                    string
	MaxFSK                  sql.NullInt64
	AllowedBouquetsJSON     string
	AllowedServiceRefsJSON  string
	FavoriteServiceRefsJSON string
	DVRPlayback             int
	DVRManage               int
	SettingsAccess          int
	UpdatedAtMS             int64
}

func snapshotHouseholdRows(t *testing.T, db *sql.DB) []rawHouseholdRow {
	t.Helper()

	rows, err := db.Query(`
		SELECT
			id,
			name,
			kind,
			max_fsk,
			allowed_bouquets_json,
			allowed_service_refs_json,
			favorite_service_refs_json,
			dvr_playback,
			dvr_manage,
			settings_access,
			updated_at_ms
		FROM household_profiles
		ORDER BY id
	`)
	if err != nil {
		t.Fatalf("query household snapshot rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := make([]rawHouseholdRow, 0)
	for rows.Next() {
		var row rawHouseholdRow
		if err := rows.Scan(
			&row.ID,
			&row.Name,
			&row.Kind,
			&row.MaxFSK,
			&row.AllowedBouquetsJSON,
			&row.AllowedServiceRefsJSON,
			&row.FavoriteServiceRefsJSON,
			&row.DVRPlayback,
			&row.DVRManage,
			&row.SettingsAccess,
			&row.UpdatedAtMS,
		); err != nil {
			t.Fatalf("scan household snapshot row: %v", err)
		}
		snapshot = append(snapshot, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate household snapshot rows: %v", err)
	}
	return snapshot
}
