// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const schemaVersion = 1

// SQLiteEnrichmentStore provides persistent, SQLite-backed storage for EPG metadata enrichment.
type SQLiteEnrichmentStore struct {
	db     *sql.DB
	mu     sync.RWMutex
	closed bool
}

// NewSQLiteEnrichmentStore creates a new SQLite-backed enrichment store and runs migrations.
func NewSQLiteEnrichmentStore(db *sql.DB) (*SQLiteEnrichmentStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite enrichment store: nil db handle")
	}

	err := sqlite.RunMigration(db, schemaVersion, func(tx *sql.Tx, currentVersion int) error {
		schema := `
		CREATE TABLE IF NOT EXISTS epg_enrichment (
			fingerprint_key TEXT PRIMARY KEY,
			fingerprint_version INTEGER NOT NULL,
			matcher_version INTEGER NOT NULL,
			status TEXT NOT NULL,
			provider TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			data_json TEXT NOT NULL,
			fetched_at_unix INTEGER NOT NULL,
			expires_at_unix INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_epg_enrichment_expires ON epg_enrichment(expires_at_unix);
		`
		_, err := tx.Exec(schema)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite enrichment store migration failed: %w", err)
	}

	return &SQLiteEnrichmentStore{db: db}, nil
}

func (s *SQLiteEnrichmentStore) Get(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, false, nil
	}

	key := fp.Key()
	query := `
	SELECT data_json, matcher_version, expires_at_unix 
	FROM epg_enrichment 
	WHERE fingerprint_key = ?
	`
	var (
		dataJSON       string
		matcherVersion int
		expiresAtUnix  int64
	)

	err := s.db.QueryRowContext(ctx, query, key).Scan(&dataJSON, &matcherVersion, &expiresAtUnix)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("sqlite enrichment store get: %w", err)
	}

	nowUnix := time.Now().Unix()
	if expiresAtUnix > 0 && expiresAtUnix <= nowUnix {
		// Expired row is treated as a cache miss
		return nil, false, nil
	}

	if matcherVersion != epg.CurrentMatcherVersion {
		// Outdated matcher version is treated as a miss (stale)
		return nil, false, nil
	}

	var data epg.EnrichmentData
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		return nil, false, fmt.Errorf("sqlite enrichment store unmarshal: %w", err)
	}

	return &data, true, nil
}

func (s *SQLiteEnrichmentStore) Put(ctx context.Context, data *epg.EnrichmentData) error {
	if data == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("sqlite enrichment store marshal: %w", err)
	}

	query := `
	INSERT INTO epg_enrichment (
		fingerprint_key,
		fingerprint_version,
		matcher_version,
		status,
		provider,
		provider_id,
		data_json,
		fetched_at_unix,
		expires_at_unix
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(fingerprint_key) DO UPDATE SET
		fingerprint_version = excluded.fingerprint_version,
		matcher_version = excluded.matcher_version,
		status = excluded.status,
		provider = excluded.provider,
		provider_id = excluded.provider_id,
		data_json = excluded.data_json,
		fetched_at_unix = excluded.fetched_at_unix,
		expires_at_unix = excluded.expires_at_unix;
	`

	var expiresUnix int64
	if !data.ExpiresAt.IsZero() {
		expiresUnix = data.ExpiresAt.Unix()
	}

	_, err = s.db.ExecContext(
		ctx,
		query,
		data.FingerprintKey,
		data.FingerprintVersion,
		data.MatcherVersion,
		string(data.Status),
		data.Identity.Provider,
		data.Identity.ID,
		string(dataJSON),
		data.FetchedAt.Unix(),
		expiresUnix,
	)
	if err != nil {
		return fmt.Errorf("sqlite enrichment store put: %w", err)
	}

	return nil
}

func (s *SQLiteEnrichmentStore) PruneExpired(ctx context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, nil
	}

	query := `DELETE FROM epg_enrichment WHERE expires_at_unix > 0 AND expires_at_unix <= ?`
	res, err := s.db.ExecContext(ctx, query, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("sqlite enrichment store prune: %w", err)
	}

	return res.RowsAffected()
}

func (s *SQLiteEnrichmentStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	// The DB handle lifecycle itself is managed by the persistence owner.
	return nil
}
