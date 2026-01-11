package library

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"
)

// GetItem retrieves a single library item by root ID and relative path.
func (s *Store) GetItem(ctx context.Context, rootID, relPath string) (*Item, error) {
	query := `
	SELECT root_id, rel_path, filename, size_bytes, mod_time, scan_time, duration_seconds, status
	FROM library_items
	WHERE root_id = ? AND rel_path = ?
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

	item.ModTime, _ = time.Parse(time.RFC3339, modTimeStr)
	item.ScanTime, _ = time.Parse(time.RFC3339, scanTimeStr)

	return &item, nil
}

// UpdateItemDuration updates the duration for an item, creating it if it doesn't exist (skeleton).
func (s *Store) UpdateItemDuration(ctx context.Context, rootID, relPath string, duration int64) error {
	query := `
	INSERT INTO library_items (root_id, rel_path, filename, size_bytes, mod_time, scan_time, duration_seconds, status)
	VALUES (?, ?, ?, 0, ?, ?, ?, 'ok')
	ON CONFLICT(root_id, rel_path) DO UPDATE SET
		duration_seconds = MAX(duration_seconds, excluded.duration_seconds)
	`
	filename := filepath.Base(relPath)
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, query,
		rootID,
		relPath,
		filename,
		now,
		now,
		duration,
	)
	return err
}
