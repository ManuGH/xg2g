package resume

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 2 // Incremented for migration_history
)

// SqliteStore implements Store using SQLite.
type SqliteStore struct {
	DB *sql.DB
}

// NewSqliteStore initializes a new SQLite resume store.
func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	if err != nil {
		return nil, err
	}

	s := &SqliteStore{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("resume store: migration failed: %w", err)
	}

	return s, nil
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	err := s.DB.QueryRow("PRAGMA user_version").Scan(&currentVersion)
	if err != nil {
		return err
	}

	if currentVersion >= schemaVersion {
		return nil
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	schema := `
	CREATE TABLE IF NOT EXISTS resume_states (
		principal_id TEXT NOT NULL,
		recording_id TEXT NOT NULL,
		pos_seconds INTEGER NOT NULL,
		duration_seconds INTEGER,
		finished BOOLEAN NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (principal_id, recording_id)
	);
	CREATE INDEX IF NOT EXISTS idx_resume_updated ON resume_states(updated_at);

	CREATE TABLE IF NOT EXISTS migration_history (
		module TEXT PRIMARY KEY,
		source_type TEXT NOT NULL,
		source_path TEXT NOT NULL,
		migrated_at_ms INTEGER NOT NULL,
		record_count INTEGER NOT NULL,
		checksum TEXT
	);
	`

	if _, err := tx.Exec(schema); err != nil {
		return err
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SqliteStore) Put(ctx context.Context, principalID, recordingID string, state *State) error {
	query := `
	INSERT INTO resume_states (principal_id, recording_id, pos_seconds, duration_seconds, finished, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(principal_id, recording_id) DO UPDATE SET
		pos_seconds = excluded.pos_seconds,
		duration_seconds = excluded.duration_seconds,
		finished = excluded.finished,
		updated_at = excluded.updated_at
	`
	_, err := s.DB.ExecContext(ctx, query,
		principalID, recordingID, state.PosSeconds, state.DurationSeconds, state.Finished, state.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *SqliteStore) Get(ctx context.Context, principalID, recordingID string) (*State, error) {
	query := `SELECT pos_seconds, duration_seconds, finished, updated_at FROM resume_states WHERE principal_id = ? AND recording_id = ?`
	var state State
	var updatedAtStr string
	err := s.DB.QueryRowContext(ctx, query, principalID, recordingID).Scan(
		&state.PosSeconds, &state.DurationSeconds, &state.Finished, &updatedAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &state, nil
}

func (s *SqliteStore) Delete(ctx context.Context, principalID, recordingID string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM resume_states WHERE principal_id = ? AND recording_id = ?", principalID, recordingID)
	return err
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}
