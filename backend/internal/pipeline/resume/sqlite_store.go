package resume

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 4 // v4 adds title/channel display snapshots for continue-watching
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
		title TEXT NOT NULL DEFAULT '',
		channel TEXT NOT NULL DEFAULT '',
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

		if currentVersion > 0 && currentVersion < schemaVersion {
			hasFingerprint, err := sqlite.TableHasColumn(tx, "resume_states", "fingerprint")
			if err != nil {
				return err
			}
			if !hasFingerprint {
				if _, err := tx.Exec(`ALTER TABLE resume_states ADD COLUMN fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
					return err
				}
			}
			for _, col := range []string{"title", "channel"} {
				hasCol, err := sqlite.TableHasColumn(tx, "resume_states", col)
				if err != nil {
					return err
				}
				if !hasCol {
					if _, err := tx.Exec(`ALTER TABLE resume_states ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil {
						return err
					}
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
	INSERT INTO resume_states (principal_id, recording_id, pos_seconds, duration_seconds, finished, fingerprint, title, channel, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(principal_id, recording_id) DO UPDATE SET
		pos_seconds = excluded.pos_seconds,
		duration_seconds = excluded.duration_seconds,
		finished = excluded.finished,
		fingerprint = excluded.fingerprint,
		title = excluded.title,
		channel = excluded.channel,
		updated_at = excluded.updated_at
	`
	_, err := s.DB.ExecContext(ctx, query,
		principalID, recordingKey, state.PosSeconds, state.DurationSeconds, state.Finished, state.Fingerprint, state.Title, state.Channel, state.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (s *SqliteStore) Get(ctx context.Context, principalID, recordingKey string) (*State, error) {
	query := `SELECT pos_seconds, duration_seconds, finished, fingerprint, title, channel, updated_at FROM resume_states WHERE principal_id = ? AND recording_id = ?`
	var state State
	var updatedAtStr string
	err := s.DB.QueryRowContext(ctx, query, principalID, recordingKey).Scan(
		&state.PosSeconds, &state.DurationSeconds, &state.Finished, &state.Fingerprint, &state.Title, &state.Channel, &updatedAtStr,
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
	return &state, nil
}

// ListRecent returns the principal's unfinished resume entries with a
// position > 0, most recently updated first.
func (s *SqliteStore) ListRecent(ctx context.Context, principalID string, limit int) ([]RecentEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
	SELECT recording_id, pos_seconds, duration_seconds, finished, fingerprint, title, channel, updated_at
	FROM resume_states
	WHERE principal_id = ? AND finished = 0 AND pos_seconds > 0
	ORDER BY updated_at DESC
	LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, query, principalID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	entries := make([]RecentEntry, 0, limit)
	for rows.Next() {
		var e RecentEntry
		var updatedAtStr string
		if err := rows.Scan(
			&e.RecordingKey, &e.State.PosSeconds, &e.State.DurationSeconds, &e.State.Finished,
			&e.State.Fingerprint, &e.State.Title, &e.State.Channel, &updatedAtStr,
		); err != nil {
			return nil, err
		}
		e.State.UpdatedAt, err = time.Parse(time.RFC3339, updatedAtStr)
		if err != nil {
			return nil, fmt.Errorf("resume store: invalid updated_at %q: %w", updatedAtStr, err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *SqliteStore) Delete(ctx context.Context, principalID, recordingKey string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM resume_states WHERE principal_id = ? AND recording_id = ?", principalID, recordingKey)
	return err
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}
