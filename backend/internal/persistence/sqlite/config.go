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
	// Construct DSN with mandatory PRAGMAs to ensure they apply to ALL connections in the pool.
	// modernc.org/sqlite supports _pragma in the DSN.
	// Format: file:path?_pragma=foo(bar)&_pragma=baz(qux)
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)",
		dbPath, cfg.BusyTimeout.Milliseconds())

	db, err := sql.Open("sqlite", dsn)
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

	return db, nil
}
