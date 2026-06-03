package store

import (
	"database/sql"
	"fmt"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	sqliteSchemaVersion = 2
	SQLiteSchemaVersion = sqliteSchemaVersion
)

type SqliteStore struct {
	DB *sql.DB
}

func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		return nil, err
	}
	store := &SqliteStore{DB: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("device auth store: migration failed: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func (s *SqliteStore) migrate() error {
	return sqlitepkg.RunMigration(s.DB, sqliteSchemaVersion, func(tx *sql.Tx, currentVersion int) error {
		schema := `
	CREATE TABLE IF NOT EXISTS pairings (
		pairing_id TEXT PRIMARY KEY,
		pairing_secret_hash TEXT NOT NULL,
		user_code TEXT NOT NULL UNIQUE,
		qr_payload TEXT NOT NULL,
		device_name TEXT NOT NULL,
		device_type TEXT NOT NULL,
		requested_policy_profile TEXT NOT NULL,
		approved_policy_profile TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		approved_at_ms INTEGER,
		consumed_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_pairings_user_code ON pairings(user_code);
	CREATE INDEX IF NOT EXISTS idx_pairings_status ON pairings(status);

	CREATE TABLE IF NOT EXISTS devices (
		device_id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL,
		device_name TEXT NOT NULL,
		device_type TEXT NOT NULL,
		policy_profile TEXT NOT NULL,
		capabilities_json TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_devices_owner_id ON devices(owner_id);

	CREATE TABLE IF NOT EXISTS device_grants (
		grant_id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		grant_hash TEXT NOT NULL UNIQUE,
		issued_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		rotate_after_ms INTEGER,
		last_used_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_device_grants_device_id ON device_grants(device_id);

	CREATE TABLE IF NOT EXISTS access_sessions (
		session_id TEXT PRIMARY KEY,
		subject_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		policy_version TEXT NOT NULL,
		scopes_json TEXT NOT NULL,
		auth_strength TEXT NOT NULL,
		issued_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_access_sessions_device_id ON access_sessions(device_id);
	CREATE INDEX IF NOT EXISTS idx_access_sessions_token_hash ON access_sessions(token_hash);

	CREATE TABLE IF NOT EXISTS web_bootstraps (
		bootstrap_id TEXT PRIMARY KEY,
		bootstrap_secret_hash TEXT NOT NULL,
		source_access_session_id TEXT NOT NULL,
		target_path TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		consumed_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_web_bootstraps_source_session_id ON web_bootstraps(source_access_session_id);
	`
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		return nil
	})
}
