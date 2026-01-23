package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Pure Go driver
)

// Config defines standard SQLite operational parameters.
type Config struct {
	BusyTimeout  time.Duration
	MaxOpenConns int // Set to 1 for writing safety, or larger for WAL reading
}

// DefaultConfig returns the CTO-grade recommended configuration for 2026.
func DefaultConfig() Config {
	return Config{
		BusyTimeout:  5 * time.Second,
		MaxOpenConns: 25, // database/sql will manage the pool
	}
}

// Open initializes a SQLite connection pool with mandatory PRAGMAs.
// It enforces WAL mode and busy_timeout per STORAGE_INVARIANTS.md.
func Open(dbPath string, cfg Config) (*sql.DB, error) {
	// Open database with base DSN
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open failed: %w", err)
	}

	// Connection Pool Invariants
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxOpenConns)
	db.SetConnMaxLifetime(1 * time.Hour)

	// Connectivity Check
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping failed: %w", err)
	}

	// Apply Mandatory PRAGMAs
	// We execute these manually to ensure they are set regardless of DSN parser logic.
	pragmas := []string{
		fmt.Sprintf("PRAGMA journal_mode=WAL"),
		fmt.Sprintf("PRAGMA synchronous=NORMAL"),
		fmt.Sprintf("PRAGMA busy_timeout=%d", cfg.BusyTimeout.Milliseconds()),
		fmt.Sprintf("PRAGMA foreign_keys=ON"),
	}

	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("sqlite: pragma %q failed: %w", p, err)
		}
	}

	return db, nil
}
