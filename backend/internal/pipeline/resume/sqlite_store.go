package resume

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 3 // v3 persists fingerprint in resume truth
	SchemaVersion = schemaVersion
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
	return sqlite.RunMigration(s.DB, schemaVersion, func(tx *sql.Tx, currentVersion int) error {
		schema := `
	CREATE TABLE IF NOT EXISTS resume_states (
		principal_id TEXT NOT NULL,
		recording_id TEXT NOT NULL,
		pos_seconds INTEGER NOT NULL,
		duration_seconds INTEGER,
		finished BOOLEAN NOT NULL DEFAULT 0,
		fingerprint TEXT NOT NULL DEFAULT '',
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

		if currentVersion < schemaVersion {
			hasFingerprint, err := sqlite.TableHasColumn(tx, "resume_states", "fingerprint")
			if err != nil {
				return err
			}
			if !hasFingerprint {
				if _, err := tx.Exec(`ALTER TABLE resume_states ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *SqliteStore) Put(ctx context.Context, principalID, recordingKey string, state *State) error {
	if state == nil {
		return ErrNilState
	}

	query := `
	INSERT INTO resume_states (principal_id, recording_id, pos_seconds, duration_seconds, finished, fingerprint, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(principal_id, recording_id) DO UPDATE SET
		pos_seconds = excluded.pos_seconds,
		duration_seconds = excluded.duration_seconds,
		finished = excluded.finished,
		fingerprint = excluded.fingerprint,
		updated_at = excluded.updated_at
	`
	_, err := s.DB.ExecContext(ctx, query,
		principalID, recordingKey, state.PosSeconds, state.DurationSeconds, state.Finished, state.Fingerprint, state.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *SqliteStore) Get(ctx context.Context, principalID, recordingKey string) (*State, error) {
	query := `SELECT pos_seconds, duration_seconds, finished, fingerprint, updated_at FROM resume_states WHERE principal_id = ? AND recording_id = ?`
	var state State
	var fingerprint string
	var updatedAtStr string
	err := s.DB.QueryRowContext(ctx, query, principalID, recordingKey).Scan(
		&state.PosSeconds, &state.DurationSeconds, &state.Finished, &fingerprint, &updatedAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("resume store: invalid updated_at %q: %w", updatedAtStr, err)
	}
	state.Fingerprint = fingerprint
	return &state, nil
}

func (s *SqliteStore) Delete(ctx context.Context, principalID, recordingKey string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM resume_states WHERE principal_id = ? AND recording_id = ?", principalID, recordingKey)
	return err
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}
