// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package library

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO)
)

// Store provides SQLite persistence for library metadata.
type Store struct {
	db *sql.DB
}

// NewStore initializes a new SQLite store and runs migrations.
// Per P0+ Gate: Sets WAL mode + busy_timeout for read-heavy workload.
func NewStore(dbPath string) (*Store, error) {
	// Open database with pragmas
	// busy_timeout avoids "database locked" errors
	dsn := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{db: db}

	// Run migrations
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate runs database schema migrations.
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS library_roots (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		last_scan_time TEXT,
		last_scan_status TEXT NOT NULL DEFAULT 'never' CHECK(last_scan_status IN ('never', 'running', 'ok', 'degraded', 'failed')),
		total_items INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS library_items (
		root_id TEXT NOT NULL,
		rel_path TEXT NOT NULL,
		filename TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		mod_time TEXT NOT NULL,
		scan_time TEXT NOT NULL,
		duration_seconds INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'ok' CHECK(status IN ('ok', 'unreadable')),
		PRIMARY KEY (root_id, rel_path)
	);

	CREATE INDEX IF NOT EXISTS idx_library_items_root ON library_items(root_id);
	CREATE INDEX IF NOT EXISTS idx_library_items_scan_time ON library_items(scan_time);
	CREATE INDEX IF NOT EXISTS idx_library_items_root_modtime ON library_items(root_id, mod_time);
	`

	_, err := s.db.Exec(schema)
	return err
}

// UpsertRoot inserts or updates a library root.
func (s *Store) UpsertRoot(ctx context.Context, id, typ string) error {
	query := `
	INSERT INTO library_roots (id, type, last_scan_status)
	VALUES (?, ?, 'never')
	ON CONFLICT(id) DO UPDATE SET type = excluded.type
	`
	_, err := s.db.ExecContext(ctx, query, id, typ)
	return err
}

// GetRoots retrieves all library roots.
func (s *Store) GetRoots(ctx context.Context) ([]Root, error) {
	query := `
	SELECT id, type, last_scan_time, last_scan_status, total_items
	FROM library_roots
	ORDER BY id
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var roots []Root
	for rows.Next() {
		var r Root
		var lastScanTimeStr sql.NullString

		if err := rows.Scan(&r.ID, &r.Type, &lastScanTimeStr, &r.LastScanStatus, &r.TotalItems); err != nil {
			return nil, err
		}

		if lastScanTimeStr.Valid {
			t, err := time.Parse(time.RFC3339, lastScanTimeStr.String)
			if err == nil {
				r.LastScanTime = &t
			}
		}

		roots = append(roots, r)
	}

	return roots, rows.Err()
}

// GetRoot retrieves a single library root by ID.
func (s *Store) GetRoot(ctx context.Context, id string) (*Root, error) {
	query := `
	SELECT id, type, last_scan_time, last_scan_status, total_items
	FROM library_roots
	WHERE id = ?
	`

	var r Root
	var lastScanTimeStr sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.Type, &lastScanTimeStr, &r.LastScanStatus, &r.TotalItems,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, err
	}

	if lastScanTimeStr.Valid {
		t, err := time.Parse(time.RFC3339, lastScanTimeStr.String)
		if err == nil {
			r.LastScanTime = &t
		}
	}

	return &r, nil
}

// UpdateRootScanStatus updates the scan metadata for a root.
func (s *Store) UpdateRootScanStatus(ctx context.Context, id string, status RootStatus, scanTime time.Time, totalItems int) error {
	query := `
	UPDATE library_roots
	SET last_scan_status = ?,
	    last_scan_time = ?,
	    total_items = ?
	WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, status.String(), scanTime.Format(time.RFC3339), totalItems, id)
	return err
}

// UpsertItem inserts or updates a library item.
// Used within TX during scan.
func (s *Store) UpsertItem(ctx context.Context, tx *sql.Tx, item Item) error {
	query := `
	INSERT INTO library_items (root_id, rel_path, filename, size_bytes, mod_time, scan_time, duration_seconds, status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(root_id, rel_path) DO UPDATE SET
		filename = excluded.filename,
		size_bytes = excluded.size_bytes,
		mod_time = excluded.mod_time,
		scan_time = excluded.scan_time,
		duration_seconds = excluded.duration_seconds,
		status = excluded.status
	`

	_, err := tx.ExecContext(ctx, query,
		item.RootID,
		item.RelPath,
		item.Filename,
		item.SizeBytes,
		item.ModTime.Format(time.RFC3339),
		item.ScanTime.Format(time.RFC3339),
		item.DurationSeconds,
		item.Status.String(),
	)
	return err
}

// GetItems retrieves paginated library items for a root.
func (s *Store) GetItems(ctx context.Context, rootID string, limit, offset int) ([]Item, int, error) {
	// Get total count
	var total int
	countQuery := `SELECT COUNT(*) FROM library_items WHERE root_id = ?`
	if err := s.db.QueryRowContext(ctx, countQuery, rootID).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get paginated items
	query := `
	SELECT root_id, rel_path, filename, size_bytes, mod_time, scan_time, duration_seconds, status
	FROM library_items
	WHERE root_id = ?
	ORDER BY rel_path
	LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, rootID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var items []Item
	for rows.Next() {
		var item Item
		var modTimeStr, scanTimeStr string

		if err := rows.Scan(
			&item.RootID,
			&item.RelPath,
			&item.Filename,
			&item.SizeBytes,
			&modTimeStr,
			&scanTimeStr,
			&item.DurationSeconds,
			&item.Status,
		); err != nil {
			return nil, 0, err
		}

		item.ModTime, _ = time.Parse(time.RFC3339, modTimeStr)
		item.ScanTime, _ = time.Parse(time.RFC3339, scanTimeStr)

		items = append(items, item)
	}

	return items, total, rows.Err()
}

// BeginTx starts a new transaction.
// Used by scanner for atomic upserts.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}
