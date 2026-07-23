// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod/fsm"
)

// ArtifactStore manages persistent FSM state in SQLite.
type ArtifactStore interface {
	InitSchema(ctx context.Context) error
	UpsertArtifact(ctx context.Context, a *fsm.Artifact) error
	GetArtifact(ctx context.Context, recordingRef, variantHash string) (*fsm.Artifact, error)
	ListByRecordingRef(ctx context.Context, recordingRef string) ([]*fsm.Artifact, error)
	DeleteArtifact(ctx context.Context, recordingRef, variantHash string) error
}

type SQLiteArtifactStore struct {
	db *sql.DB
}

// NewArtifactStore creates a new SQLite artifact FSM store instance.
func NewArtifactStore(db *sql.DB) *SQLiteArtifactStore {
	return &SQLiteArtifactStore{db: db}
}

// InitSchema ensures the artifact_state table and indices exist.
func (s *SQLiteArtifactStore) InitSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS artifact_state (
		id TEXT PRIMARY KEY,
		recording_ref TEXT NOT NULL,
		variant_hash TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('PREPARING', 'READY', 'FAILED', 'DELETED')),
		failure_reason TEXT,
		manifest_path TEXT NOT NULL,
		segment_pattern TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		UNIQUE(recording_ref, variant_hash)
	);
	CREATE INDEX IF NOT EXISTS idx_artifact_state_rec_ref ON artifact_state(recording_ref);
	`
	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to init artifact_state schema: %w", err)
	}
	return nil
}

// UpsertArtifact inserts or updates an artifact's state in SQLite.
func (s *SQLiteArtifactStore) UpsertArtifact(ctx context.Context, a *fsm.Artifact) error {
	if a == nil {
		return errors.New("cannot upsert nil artifact")
	}

	query := `
	INSERT INTO artifact_state (
		id, recording_ref, variant_hash, state, failure_reason, manifest_path, segment_pattern, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(recording_ref, variant_hash) DO UPDATE SET
		state = excluded.state,
		failure_reason = excluded.failure_reason,
		manifest_path = excluded.manifest_path,
		segment_pattern = excluded.segment_pattern,
		updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query,
		a.ID,
		a.RecordingRef,
		a.VariantHash,
		string(a.State),
		a.FailureReason,
		a.ManifestPath,
		a.SegmentPattern,
		a.CreatedAt.UTC().Format(time.RFC3339Nano),
		a.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert artifact state: %w", err)
	}
	return nil
}

// GetArtifact fetches an artifact by recording ref and variant hash.
func (s *SQLiteArtifactStore) GetArtifact(ctx context.Context, recordingRef, variantHash string) (*fsm.Artifact, error) {
	query := `
	SELECT id, recording_ref, variant_hash, state, failure_reason, manifest_path, segment_pattern, created_at, updated_at
	FROM artifact_state
	WHERE recording_ref = ? AND variant_hash = ?
	`

	row := s.db.QueryRowContext(ctx, query, recordingRef, variantHash)

	var a fsm.Artifact
	var stateStr, createdAtStr, updatedAtStr string
	err := row.Scan(
		&a.ID,
		&a.RecordingRef,
		&a.VariantHash,
		&stateStr,
		&a.FailureReason,
		&a.ManifestPath,
		&a.SegmentPattern,
		&createdAtStr,
		&updatedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fsm.ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan artifact: %w", err)
	}

	a.State = fsm.State(stateStr)
	if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAtStr); err == nil {
		a.UpdatedAt = t
	}

	return &a, nil
}

// ListByRecordingRef lists all artifacts associated with a recording reference.
func (s *SQLiteArtifactStore) ListByRecordingRef(ctx context.Context, recordingRef string) ([]*fsm.Artifact, error) {
	query := `
	SELECT id, recording_ref, variant_hash, state, failure_reason, manifest_path, segment_pattern, created_at, updated_at
	FROM artifact_state
	WHERE recording_ref = ?
	ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, recordingRef)
	if err != nil {
		return nil, fmt.Errorf("failed to query artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*fsm.Artifact
	for rows.Next() {
		var a fsm.Artifact
		var stateStr, createdAtStr, updatedAtStr string
		if err := rows.Scan(
			&a.ID,
			&a.RecordingRef,
			&a.VariantHash,
			&stateStr,
			&a.FailureReason,
			&a.ManifestPath,
			&a.SegmentPattern,
			&createdAtStr,
			&updatedAtStr,
		); err != nil {
			return nil, fmt.Errorf("failed to scan artifact row: %w", err)
		}

		a.State = fsm.State(stateStr)
		if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			a.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339Nano, updatedAtStr); err == nil {
			a.UpdatedAt = t
		}
		results = append(results, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// DeleteArtifact removes an artifact entry by recording ref and variant hash.
func (s *SQLiteArtifactStore) DeleteArtifact(ctx context.Context, recordingRef, variantHash string) error {
	query := `DELETE FROM artifact_state WHERE recording_ref = ? AND variant_hash = ?`
	_, err := s.db.ExecContext(ctx, query, recordingRef, variantHash)
	if err != nil {
		return fmt.Errorf("failed to delete artifact state: %w", err)
	}
	return nil
}
