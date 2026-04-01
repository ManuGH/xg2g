package household

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const sqliteSchemaVersion = 1

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
		return nil, fmt.Errorf("household store: migration failed: %w", err)
	}
	if err := store.seedDefaultProfile(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("household store: seed default profile: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func (s *SqliteStore) List(ctx context.Context) ([]Profile, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			id,
			name,
			kind,
			max_fsk,
			allowed_bouquets_json,
			allowed_service_refs_json,
			favorite_service_refs_json,
			dvr_playback,
			dvr_manage,
			settings_access
		FROM household_profiles
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	profiles := make([]Profile, 0)
	for rows.Next() {
		profile, err := scanProfile(rows.Scan)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *SqliteStore) Get(ctx context.Context, id string) (Profile, bool, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			kind,
			max_fsk,
			allowed_bouquets_json,
			allowed_service_refs_json,
			favorite_service_refs_json,
			dvr_playback,
			dvr_manage,
			settings_access
		FROM household_profiles
		WHERE id = ?
	`, normalizeIdentifier(id))

	profile, err := scanProfile(row.Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return Profile{}, false, nil
		}
		return Profile{}, false, err
	}
	return profile, true, nil
}

func (s *SqliteStore) Upsert(ctx context.Context, profile Profile) error {
	normalized, err := PrepareProfile(profile)
	if err != nil {
		return err
	}

	allowedBouquetsJSON, err := marshalStringList(normalized.AllowedBouquets)
	if err != nil {
		return err
	}
	allowedServiceRefsJSON, err := marshalStringList(normalized.AllowedServiceRefs)
	if err != nil {
		return err
	}
	favoriteServiceRefsJSON, err := marshalStringList(normalized.FavoriteServiceRefs)
	if err != nil {
		return err
	}

	var maxFSK any
	if normalized.MaxFSK != nil {
		maxFSK = *normalized.MaxFSK
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO household_profiles (
			id,
			name,
			kind,
			max_fsk,
			allowed_bouquets_json,
			allowed_service_refs_json,
			favorite_service_refs_json,
			dvr_playback,
			dvr_manage,
			settings_access,
			updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			kind = excluded.kind,
			max_fsk = excluded.max_fsk,
			allowed_bouquets_json = excluded.allowed_bouquets_json,
			allowed_service_refs_json = excluded.allowed_service_refs_json,
			favorite_service_refs_json = excluded.favorite_service_refs_json,
			dvr_playback = excluded.dvr_playback,
			dvr_manage = excluded.dvr_manage,
			settings_access = excluded.settings_access,
			updated_at_ms = excluded.updated_at_ms
	`, normalized.ID, normalized.Name, normalized.Kind, maxFSK, allowedBouquetsJSON, allowedServiceRefsJSON, favoriteServiceRefsJSON, boolToInt(normalized.Permissions.DVRPlayback), boolToInt(normalized.Permissions.DVRManage), boolToInt(normalized.Permissions.Settings), time.Now().UTC().UnixMilli())
	return err
}

func (s *SqliteStore) Delete(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM household_profiles
		WHERE id = ?
	`, normalizeIdentifier(id))
	return err
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	if err := s.DB.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion >= sqliteSchemaVersion {
		return nil
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS household_profiles (
			id TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			max_fsk INTEGER,
			allowed_bouquets_json TEXT NOT NULL,
			allowed_service_refs_json TEXT NOT NULL,
			favorite_service_refs_json TEXT NOT NULL,
			dvr_playback INTEGER NOT NULL,
			dvr_manage INTEGER NOT NULL,
			settings_access INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_household_profiles_kind ON household_profiles(kind, name);
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SqliteStore) seedDefaultProfile(ctx context.Context) error {
	_, ok, err := s.Get(ctx, DefaultProfileID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return s.Upsert(ctx, CreateDefaultProfile())
}

type scannerFunc func(dest ...any) error

func scanProfile(scan scannerFunc) (Profile, error) {
	var (
		id                      string
		name                    string
		kind                    string
		maxFSK                  sql.NullInt64
		allowedBouquetsJSON     string
		allowedServiceRefsJSON  string
		favoriteServiceRefsJSON string
		dvrPlayback             int
		dvrManage               int
		settingsAccess          int
	)
	if err := scan(&id, &name, &kind, &maxFSK, &allowedBouquetsJSON, &allowedServiceRefsJSON, &favoriteServiceRefsJSON, &dvrPlayback, &dvrManage, &settingsAccess); err != nil {
		return Profile{}, err
	}

	allowedBouquets, err := unmarshalStringList(allowedBouquetsJSON)
	if err != nil {
		return Profile{}, err
	}
	allowedServiceRefs, err := unmarshalStringList(allowedServiceRefsJSON)
	if err != nil {
		return Profile{}, err
	}
	favoriteServiceRefs, err := unmarshalStringList(favoriteServiceRefsJSON)
	if err != nil {
		return Profile{}, err
	}

	var normalizedMaxFSK *int
	if maxFSK.Valid {
		value := int(maxFSK.Int64)
		normalizedMaxFSK = &value
	}

	return NormalizeProfile(Profile{
		ID:                  id,
		Name:                name,
		Kind:                ProfileKind(kind),
		MaxFSK:              normalizedMaxFSK,
		AllowedBouquets:     allowedBouquets,
		AllowedServiceRefs:  allowedServiceRefs,
		FavoriteServiceRefs: favoriteServiceRefs,
		Permissions: Permissions{
			DVRPlayback: dvrPlayback != 0,
			DVRManage:   dvrManage != 0,
			Settings:    settingsAccess != 0,
		},
	}), nil
}

func marshalStringList(values []string) (string, error) {
	data, err := json.Marshal(normalizeIdentifierOrServiceList(values))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeIdentifierOrServiceList(values []string) []string {
	return append([]string(nil), values...)
}

func unmarshalStringList(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
