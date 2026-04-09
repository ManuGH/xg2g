package entitlements

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func TestSqliteStoreUpsertIsIdempotentPerPrincipalScopeSource(t *testing.T) {
	store, err := NewStore("sqlite", t.TempDir())
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close sqlite store: %v", err)
		}
	}()

	firstGrantedAt := time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC)
	secondGrantedAt := firstGrantedAt.Add(2 * time.Hour)
	secondExpiresAt := secondGrantedAt.Add(24 * time.Hour)

	firstGrant := Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      SourceGooglePlay,
		GrantedAt:   firstGrantedAt,
	}
	secondGrant := Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      SourceGooglePlay,
		GrantedAt:   secondGrantedAt,
		ExpiresAt:   &secondExpiresAt,
	}

	if err := store.Upsert(context.Background(), firstGrant); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := store.Upsert(context.Background(), secondGrant); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected exactly one upserted grant, got %d", len(grants))
	}
	if !grants[0].GrantedAt.Equal(secondGrantedAt) {
		t.Fatalf("expected upsert to keep latest grantedAt %s, got %s", secondGrantedAt, grants[0].GrantedAt)
	}
	if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(secondExpiresAt) {
		t.Fatalf("expected upsert to keep latest expiresAt %v, got %v", secondExpiresAt, grants[0].ExpiresAt)
	}
}

func TestSqliteStore_ReopenRoundTripAndDeleteGrant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "entitlements.sqlite")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	expiresAt := time.Date(2026, time.April, 5, 12, 0, 0, 0, time.UTC)
	grant := Grant{
		PrincipalID: "viewer",
		Scope:       "xg2g:unlock",
		Source:      SourceGooglePlay,
		GrantedAt:   time.Date(2026, time.April, 4, 12, 0, 0, 0, time.UTC),
		ExpiresAt:   &expiresAt,
	}
	if err := store.Upsert(context.Background(), grant); err != nil {
		t.Fatalf("upsert grant: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants after reopen: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected one persisted grant, got %#v", grants)
	}
	if grants[0].Scope != "xg2g:unlock" || grants[0].Source != SourceGooglePlay {
		t.Fatalf("unexpected persisted grant: %#v", grants[0])
	}
	if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("expected persisted expiry %v, got %v", expiresAt, grants[0].ExpiresAt)
	}
	if err := store.Delete(context.Background(), "viewer", "xg2g:unlock", SourceGooglePlay); err != nil {
		t.Fatalf("delete grant: %v", err)
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

	grants, err = store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list grants after delete reopen: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("expected deleted grant to stay absent after reopen, got %#v", grants)
	}
}

func TestSqliteStore_MigratesLegacyV1RowsToCanonicalGrants(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "entitlements.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE entitlements (
			principal_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			source TEXT NOT NULL,
			granted_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (principal_id, scope, source)
		);
		INSERT INTO entitlements(principal_id, scope, source, granted_at_ms, expires_at_ms, updated_at_ms) VALUES
			(' viewer ', ' XG2G:DVR ', ' Admin_Override ', 1000, NULL, 2000),
			('viewer', 'xg2g:dvr', 'admin_override', 500, NULL, 1000),
			('viewer', 'XG2G:UNLOCK', ' GOOGLE_PLAY ', 1500, 2500, 1500);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed legacy entitlement rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated entitlement store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated entitlement store: %v", err)
		}
	}()

	var version int
	if err := store.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("query entitlement schema version: %v", err)
	}
	if version != sqliteSchemaVersion {
		t.Fatalf("expected entitlement schema version %d, got %d", sqliteSchemaVersion, version)
	}

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list migrated grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected two canonical grants after migration, got %#v", grants)
	}
	if grants[0].Scope != "xg2g:dvr" || grants[0].Source != SourceAdminOverride {
		t.Fatalf("expected canonical dvr grant first, got %#v", grants[0])
	}
	if !grants[0].GrantedAt.Equal(time.UnixMilli(1000).UTC()) {
		t.Fatalf("expected latest canonical dvr grant to win, got %s", grants[0].GrantedAt)
	}
	if grants[1].Scope != "xg2g:unlock" || grants[1].Source != SourceGooglePlay {
		t.Fatalf("expected canonical unlock grant second, got %#v", grants[1])
	}
	if grants[1].ExpiresAt == nil || !grants[1].ExpiresAt.Equal(time.UnixMilli(2500).UTC()) {
		t.Fatalf("expected migrated unlock expiry, got %v", grants[1].ExpiresAt)
	}

	var rowCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM entitlements WHERE principal_id = 'viewer'`).Scan(&rowCount); err != nil {
		t.Fatalf("count canonical grants: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("expected two canonical stored grants, got %d", rowCount)
	}
	var legacyCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM entitlements WHERE principal_id != 'viewer' OR scope != lower(scope) OR source != lower(source)`).Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy entitlement rows: %v", err)
	}
	if legacyCount != 0 {
		t.Fatalf("expected canonical entitlement rows after migration, got %d legacy rows", legacyCount)
	}

	updatedExpiresAt := time.Date(2026, time.April, 7, 12, 0, 0, 0, time.UTC)
	if err := store.Upsert(context.Background(), Grant{
		PrincipalID: "viewer",
		Scope:       "XG2G:DVR",
		Source:      "ADMIN_OVERRIDE",
		GrantedAt:   time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC),
		ExpiresAt:   &updatedExpiresAt,
	}); err != nil {
		t.Fatalf("rewrite migrated dvr grant: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close rewritten entitlement store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen rewritten entitlement store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close final rewritten entitlement store: %v", err)
		}
	}()

	grants, err = store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list rewritten grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected two grants after rewrite, got %#v", grants)
	}
	if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(updatedExpiresAt) {
		t.Fatalf("expected rewritten dvr expiry %v, got %v", updatedExpiresAt, grants[0].ExpiresAt)
	}
}

func TestSqliteStore_MigrationCollisionKeepsNewestCanonicalGrant(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "entitlements.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE entitlements (
			principal_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			source TEXT NOT NULL,
			granted_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (principal_id, scope, source)
		);
		INSERT INTO entitlements(principal_id, scope, source, granted_at_ms, expires_at_ms, updated_at_ms) VALUES
			('viewer', 'xg2g:dvr', 'admin_override', 1000, NULL, 1500),
			(' viewer ', ' XG2G:DVR ', ' ADMIN_OVERRIDE ', 3000, 4000, 2500),
			('viewer', 'xg2g:unlock', 'google_play', 5000, 6000, 5000);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed colliding legacy entitlement rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated entitlement store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated entitlement store: %v", err)
		}
	}()

	grants, err := store.ListByPrincipal(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("list migrated grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected canonical dvr plus unlock grants, got %#v", grants)
	}
	if grants[0].Scope != "xg2g:dvr" || grants[0].Source != SourceAdminOverride {
		t.Fatalf("expected canonical dvr grant first, got %#v", grants[0])
	}
	if !grants[0].GrantedAt.Equal(time.UnixMilli(3000).UTC()) {
		t.Fatalf("expected newest colliding dvr grant to win, got %#v", grants[0])
	}
	if grants[0].ExpiresAt == nil || !grants[0].ExpiresAt.Equal(time.UnixMilli(4000).UTC()) {
		t.Fatalf("expected newest colliding dvr expiry to win, got %#v", grants[0])
	}

	var rowCount int
	if err := store.DB.QueryRow(`SELECT COUNT(*) FROM entitlements WHERE principal_id = 'viewer' AND scope = 'xg2g:dvr' AND source = 'admin_override'`).Scan(&rowCount); err != nil {
		t.Fatalf("count canonical dvr rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected exactly one canonical dvr grant after collision migration, got %d", rowCount)
	}
}

func TestSqliteStore_MigrationIsIdempotentOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "entitlements.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE entitlements (
			principal_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			source TEXT NOT NULL,
			granted_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (principal_id, scope, source)
		);
		INSERT INTO entitlements(principal_id, scope, source, granted_at_ms, expires_at_ms, updated_at_ms) VALUES
			(' viewer ', ' XG2G:DVR ', ' Admin_Override ', 1000, NULL, 2000),
			('viewer', 'XG2G:UNLOCK', ' GOOGLE_PLAY ', 1500, 2500, 1500);
		PRAGMA user_version = 1;
	`)
	if err != nil {
		t.Fatalf("seed legacy entitlement rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated entitlement store: %v", err)
	}
	firstSnapshot := snapshotEntitlementRows(t, store.DB)
	if err := store.Close(); err != nil {
		t.Fatalf("close first migrated entitlement store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen migrated entitlement store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close reopened entitlement store: %v", err)
		}
	}()

	secondSnapshot := snapshotEntitlementRows(t, store.DB)
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatalf("expected migration reopen to be idempotent\nfirst:  %#v\nsecond: %#v", firstSnapshot, secondSnapshot)
	}
}

type rawEntitlementRow struct {
	PrincipalID string
	Scope       string
	Source      string
	GrantedAtMS int64
	ExpiresAtMS sql.NullInt64
	UpdatedAtMS int64
}

func snapshotEntitlementRows(t *testing.T, db *sql.DB) []rawEntitlementRow {
	t.Helper()

	rows, err := db.Query(`
		SELECT principal_id, scope, source, granted_at_ms, expires_at_ms, updated_at_ms
		FROM entitlements
		ORDER BY principal_id, scope, source
	`)
	if err != nil {
		t.Fatalf("query entitlement snapshot rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := make([]rawEntitlementRow, 0)
	for rows.Next() {
		var row rawEntitlementRow
		if err := rows.Scan(&row.PrincipalID, &row.Scope, &row.Source, &row.GrantedAtMS, &row.ExpiresAtMS, &row.UpdatedAtMS); err != nil {
			t.Fatalf("scan entitlement snapshot row: %v", err)
		}
		snapshot = append(snapshot, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate entitlement snapshot rows: %v", err)
	}
	return snapshot
}
