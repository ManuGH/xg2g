package scan

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 2 // Incremented for migration_history
)

// SqliteStore implements Capability storage using SQLite.
type SqliteStore struct {
	DB *sql.DB
}

// NewSqliteStore initializes a new SQLite capability store.
func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	if err != nil {
		return nil, err
	}

	s := &SqliteStore{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("capability store: migration failed: %w", err)
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
	defer tx.Rollback()

	schema := `
	CREATE TABLE IF NOT EXISTS capabilities (
		service_ref TEXT PRIMARY KEY,
		interlaced BOOLEAN NOT NULL DEFAULT 0,
		last_scan TEXT NOT NULL,
		resolution TEXT NOT NULL,
		codec TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_capabilities_scan ON capabilities(last_scan);

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

func (s *SqliteStore) Update(cap Capability) {
	query := `
	INSERT INTO capabilities (service_ref, interlaced, last_scan, resolution, codec)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(service_ref) DO UPDATE SET
		interlaced = excluded.interlaced,
		last_scan = excluded.last_scan,
		resolution = excluded.resolution,
		codec = excluded.codec
	`
	_, _ = s.DB.Exec(query,
		cap.ServiceRef, cap.Interlaced, cap.LastScan.Format(time.RFC3339), cap.Resolution, cap.Codec,
	)
}

func (s *SqliteStore) Get(serviceRef string) (Capability, bool) {
	query := `SELECT interlaced, last_scan, resolution, codec FROM capabilities WHERE service_ref = ?`
	var cap Capability
	var lastScanStr string
	err := s.DB.QueryRow(query, serviceRef).Scan(
		&cap.Interlaced, &lastScanStr, &cap.Resolution, &cap.Codec,
	)
	if err != nil {
		return Capability{}, false
	}
	cap.ServiceRef = serviceRef
	cap.LastScan, _ = time.Parse(time.RFC3339, lastScanStr)
	return cap, true
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}
