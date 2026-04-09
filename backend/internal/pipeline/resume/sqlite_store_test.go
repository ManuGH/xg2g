package resume

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func TestSqliteStore_ReopenRoundTripPersistsFingerprint(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "resume.sqlite")
	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	want := &State{
		PosSeconds:      123,
		DurationSeconds: 3600,
		UpdatedAt:       time.Date(2026, time.April, 9, 15, 0, 0, 0, time.UTC),
		Fingerprint:     "id:recording-1",
		Finished:        true,
	}
	if err := store.Put(context.Background(), "viewer", "recording-1", want); err != nil {
		t.Fatalf("put state: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close reopened sqlite store: %v", err)
		}
	}()

	got, err := store.Get(context.Background(), "viewer", "recording-1")
	if err != nil {
		t.Fatalf("get persisted state: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected reopened state %#v, got %#v", want, got)
	}
}

func TestSqliteStore_MigratesV2RowsToFingerprintSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "resume.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE resume_states (
			principal_id TEXT NOT NULL,
			recording_id TEXT NOT NULL,
			pos_seconds INTEGER NOT NULL,
			duration_seconds INTEGER,
			finished BOOLEAN NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (principal_id, recording_id)
		);
		CREATE INDEX idx_resume_updated ON resume_states(updated_at);
		CREATE TABLE migration_history (
			module TEXT PRIMARY KEY,
			source_type TEXT NOT NULL,
			source_path TEXT NOT NULL,
			migrated_at_ms INTEGER NOT NULL,
			record_count INTEGER NOT NULL,
			checksum TEXT
		);
		INSERT INTO resume_states(principal_id, recording_id, pos_seconds, duration_seconds, finished, updated_at)
		VALUES ('viewer', 'recording-1', 90, 3600, 1, '2026-04-09T12:00:00Z');
		PRAGMA user_version = 2;
	`)
	if err != nil {
		t.Fatalf("seed legacy v2 resume rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close migrated sqlite store: %v", err)
		}
	}()

	var version int
	if err := store.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("expected schema version %d, got %d", schemaVersion, version)
	}
	if !dbHasColumn(t, store.DB, "resume_states", "fingerprint") {
		t.Fatal("expected migrated resume_states table to include fingerprint column")
	}

	got, err := store.Get(context.Background(), "viewer", "recording-1")
	if err != nil {
		t.Fatalf("get migrated state: %v", err)
	}
	want := &State{
		PosSeconds:      90,
		DurationSeconds: 3600,
		UpdatedAt:       time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC),
		Fingerprint:     "",
		Finished:        true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected migrated state %#v, got %#v", want, got)
	}

	want.Fingerprint = "id:recording-1"
	want.UpdatedAt = time.Date(2026, time.April, 9, 13, 0, 0, 0, time.UTC)
	if err := store.Put(context.Background(), "viewer", "recording-1", want); err != nil {
		t.Fatalf("rewrite migrated state: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close rewritten sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen rewritten sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close final sqlite store: %v", err)
		}
	}()

	got, err = store.Get(context.Background(), "viewer", "recording-1")
	if err != nil {
		t.Fatalf("get rewritten state: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected rewritten state %#v, got %#v", want, got)
	}
}

func TestSqliteStore_MigrationIsIdempotentOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "resume.sqlite")
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		t.Fatalf("open raw sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE resume_states (
			principal_id TEXT NOT NULL,
			recording_id TEXT NOT NULL,
			pos_seconds INTEGER NOT NULL,
			duration_seconds INTEGER,
			finished BOOLEAN NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (principal_id, recording_id)
		);
		CREATE INDEX idx_resume_updated ON resume_states(updated_at);
		INSERT INTO resume_states(principal_id, recording_id, pos_seconds, duration_seconds, finished, updated_at)
		VALUES ('viewer', 'recording-1', 90, 3600, 1, '2026-04-09T12:00:00Z');
		PRAGMA user_version = 2;
	`)
	if err != nil {
		t.Fatalf("seed legacy resume rows: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw sqlite db: %v", err)
	}

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open migrated sqlite store: %v", err)
	}
	firstSnapshot := snapshotResumeRows(t, store.DB)
	if err := store.Close(); err != nil {
		t.Fatalf("close first migrated sqlite store: %v", err)
	}

	store, err = NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen migrated sqlite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close reopened sqlite store: %v", err)
		}
	}()

	secondSnapshot := snapshotResumeRows(t, store.DB)
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatalf("expected migration reopen to be idempotent\nfirst:  %#v\nsecond: %#v", firstSnapshot, secondSnapshot)
	}
}

type rawResumeRow struct {
	PrincipalID     string
	RecordingID     string
	PosSeconds      int64
	DurationSeconds sql.NullInt64
	Finished        bool
	Fingerprint     string
	UpdatedAt       string
}

func snapshotResumeRows(t *testing.T, db *sql.DB) []rawResumeRow {
	t.Helper()

	rows, err := db.Query(`
		SELECT principal_id, recording_id, pos_seconds, duration_seconds, finished, fingerprint, updated_at
		FROM resume_states
		ORDER BY principal_id, recording_id
	`)
	if err != nil {
		t.Fatalf("query resume rows snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()

	snapshot := make([]rawResumeRow, 0)
	for rows.Next() {
		var row rawResumeRow
		if err := rows.Scan(&row.PrincipalID, &row.RecordingID, &row.PosSeconds, &row.DurationSeconds, &row.Finished, &row.Fingerprint, &row.UpdatedAt); err != nil {
			t.Fatalf("scan resume row snapshot: %v", err)
		}
		snapshot = append(snapshot, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate resume row snapshot: %v", err)
	}
	return snapshot
}

func dbHasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("query table info for %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan table info for %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info for %s: %v", table, err)
	}
	return false
}
