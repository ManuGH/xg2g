package migration

import (
	"database/sql"
)

// Module constants
const (
	ModuleSessions     = "sessions"
	ModuleResume       = "resume"
	ModuleCapabilities = "capabilities"
)

// HistoryRecord matches the migration_history table schema.
type HistoryRecord struct {
	Module       string
	SourceType   string
	SourcePath   string
	MigratedAtMs int64
	RecordCount  int
	Checksum     string
}

// IsMigrated checks if a module has already been migrated in the target DB.
func IsMigrated(db *sql.DB, module string) (bool, error) {
	var exists int
	err := db.QueryRow("SELECT COUNT(*) FROM migration_history WHERE module = ?", module).Scan(&exists)
	if err != nil {
		// If table doesn't exist, it's not migrated
		return false, nil
	}
	return exists > 0, nil
}

// RecordMigration saves the migration completion status to the target DB.
func RecordMigration(db *sql.DB, rec HistoryRecord) error {
	query := `
	INSERT INTO migration_history (module, source_type, source_path, migrated_at_ms, record_count, checksum)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(module) DO UPDATE SET
		source_type = excluded.source_type,
		source_path = excluded.source_path,
		migrated_at_ms = excluded.migrated_at_ms,
		record_count = excluded.record_count,
		checksum = excluded.checksum
	`
	_, err := db.Exec(query,
		rec.Module, rec.SourceType, rec.SourcePath, rec.MigratedAtMs, rec.RecordCount, rec.Checksum,
	)
	return err
}

// GetHistory retrieves the migration record for a module.
func GetHistory(db *sql.DB, module string) (*HistoryRecord, error) {
	var rec HistoryRecord
	query := `SELECT module, source_type, source_path, migrated_at_ms, record_count, checksum FROM migration_history WHERE module = ?`
	err := db.QueryRow(query, module).Scan(&rec.Module, &rec.SourceType, &rec.SourcePath, &rec.MigratedAtMs, &rec.RecordCount, &rec.Checksum)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &rec, err
}
