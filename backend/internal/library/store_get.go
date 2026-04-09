package library

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetItem retrieves a single library item by root ID and relative path.
func (s *Store) GetItem(ctx context.Context, rootID, relPath string) (*Item, error) {
	query := `
	SELECT
		i.root_id,
		i.rel_path,
		i.filename,
		i.size_bytes,
		i.mod_time,
		i.scan_time,
		COALESCE(d.duration_seconds, i.duration_seconds) AS duration_seconds,
		i.status
	FROM library_items i
	LEFT JOIN library_item_durations d
		ON d.root_id = i.root_id AND d.rel_path = i.rel_path
	WHERE i.root_id = ? AND i.rel_path = ?
	`

	var item Item
	var modTimeStr, scanTimeStr string

	err := s.db.QueryRowContext(ctx, query, rootID, relPath).Scan(
		&item.RootID,
		&item.RelPath,
		&item.Filename,
		&item.SizeBytes,
		&modTimeStr,
		&scanTimeStr,
		&item.DurationSeconds,
		&item.Status,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, err
	}

	item.ModTime, err = time.Parse(time.RFC3339, modTimeStr)
	if err != nil {
		return nil, fmt.Errorf("parse mod_time %q: %w", modTimeStr, err)
	}
	item.ScanTime, err = time.Parse(time.RFC3339, scanTimeStr)
	if err != nil {
		return nil, fmt.Errorf("parse scan_time %q: %w", scanTimeStr, err)
	}

	return &item, nil
}

// GetItemDuration returns persisted duration truth for a root/path without
// asserting that a catalog row exists. Probe-side duration persistence is
// auxiliary to the catalog snapshot and may legitimately exist before the first
// scan confirms the item.
func (s *Store) GetItemDuration(ctx context.Context, rootID, relPath string) (int64, bool, error) {
	var duration int64
	err := s.db.QueryRowContext(ctx, `
		SELECT duration_seconds
		FROM library_item_durations
		WHERE root_id = ? AND rel_path = ?
	`, rootID, relPath).Scan(&duration)
	if err == nil {
		return duration, duration > 0, nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}

	item, err := s.GetItem(ctx, rootID, relPath)
	if err != nil {
		return 0, false, err
	}
	if item == nil || item.DurationSeconds <= 0 {
		return 0, false, nil
	}
	return item.DurationSeconds, true, nil
}

// UpdateItemDuration updates the auxiliary duration truth for a root/path
// without creating catalog rows.
func (s *Store) UpdateItemDuration(ctx context.Context, rootID, relPath string, duration int64) error {
	query := `
	INSERT INTO library_item_durations (root_id, rel_path, duration_seconds, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(root_id, rel_path) DO UPDATE SET
		duration_seconds = excluded.duration_seconds,
		updated_at = excluded.updated_at
	`
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, query,
		rootID,
		relPath,
		duration,
		now,
	)
	return err
}
