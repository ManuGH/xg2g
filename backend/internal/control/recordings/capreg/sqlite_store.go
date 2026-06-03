package capreg

import (
	"database/sql"
	"fmt"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	sqliteSchemaVersion = 8
	SQLiteSchemaVersion = sqliteSchemaVersion
)

type SqliteStore struct {
	DB *sql.DB
}

func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlitepkg.Open(dbPath, sqlitepkg.DefaultConfig())
	if err != nil {
		return nil, err
	}
	store := &SqliteStore{DB: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("capability registry: migration failed: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	if err := s.DB.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	switch {
	case currentVersion <= 0:
		if err := createSchemaV8(tx); err != nil {
			return err
		}
	case currentVersion == 1:
		if err := migrateSchemaV1ToV8(tx); err != nil {
			return err
		}
	case currentVersion == 2:
		if err := migrateSchemaV2ToV8(tx); err != nil {
			return err
		}
	case currentVersion == 4:
		if err := migrateSchemaV4ToV8(tx); err != nil {
			return err
		}
	case currentVersion == 5:
		if err := migrateSchemaV5ToV8(tx); err != nil {
			return err
		}
	case currentVersion == 6:
		if err := migrateSchemaV6ToV8(tx); err != nil {
			return err
		}
	case currentVersion == 7:
		if err := migrateSchemaV7ToV8(tx); err != nil {
			return err
		}
	default:
		if err := createSchemaV8(tx); err != nil {
			return err
		}
	}
	if err := ensureSchemaV8Columns(tx); err != nil {
		return err
	}

	if currentVersion < sqliteSchemaVersion {
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteSchemaVersion)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SqliteStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}
