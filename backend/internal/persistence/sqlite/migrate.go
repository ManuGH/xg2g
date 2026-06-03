package sqlite

import (
	"database/sql"
	"fmt"
)

// RunMigration reads the SQLite PRAGMA user_version and, when it is below
// targetVersion, runs apply inside a transaction, bumps user_version to
// targetVersion, and commits. apply receives the transaction and the current
// (pre-migration) schema version. If already at or above targetVersion it is a
// no-op. Extracted from the identical migrate() skeleton repeated across stores.
func RunMigration(db *sql.DB, targetVersion int, apply func(tx *sql.Tx, currentVersion int) error) error {
	var currentVersion int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion >= targetVersion {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := apply(tx, currentVersion); err != nil {
		return err
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", targetVersion)); err != nil {
		return err
	}
	return tx.Commit()
}
