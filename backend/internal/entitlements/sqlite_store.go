package entitlements

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const sqliteSchemaVersion = 2

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
		return nil, fmt.Errorf("entitlement store: migration failed: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func (s *SqliteStore) ListByPrincipal(ctx context.Context, principalID string) ([]Grant, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT principal_id, scope, source, granted_at_ms, expires_at_ms
		FROM entitlements
		WHERE principal_id = ?
		ORDER BY scope, source
	`, normalizePrincipalID(principalID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	grants := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		var grantedAtMS int64
		var expiresAtMS sql.NullInt64
		if err := rows.Scan(&grant.PrincipalID, &grant.Scope, &grant.Source, &grantedAtMS, &expiresAtMS); err != nil {
			return nil, err
		}
		grants = append(grants, Grant{
			PrincipalID: normalizePrincipalID(grant.PrincipalID),
			Scope:       normalizeScope(grant.Scope),
			Source:      normalizeSource(grant.Source),
			GrantedAt:   time.UnixMilli(grantedAtMS).UTC(),
			ExpiresAt:   nullableUnixMillis(expiresAtMS),
		})
	}
	sortGrants(grants)
	return grants, rows.Err()
}

func (s *SqliteStore) Upsert(ctx context.Context, grant Grant) error {
	normalized, err := normalizeGrant(grant)
	if err != nil {
		return err
	}

	updatedAtMS := time.Now().UTC().UnixMilli()
	var expiresAtMS any
	if normalized.ExpiresAt != nil {
		expiresAtMS = normalized.ExpiresAt.UTC().UnixMilli()
	}

	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO entitlements (
			principal_id,
			scope,
			source,
			granted_at_ms,
			expires_at_ms,
			updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(principal_id, scope, source) DO UPDATE SET
			granted_at_ms = excluded.granted_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			updated_at_ms = excluded.updated_at_ms
	`, normalized.PrincipalID, normalized.Scope, normalized.Source, normalized.GrantedAt.UTC().UnixMilli(), expiresAtMS, updatedAtMS)
	return err
}

func (s *SqliteStore) Delete(ctx context.Context, principalID, scope, source string) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM entitlements
		WHERE principal_id = ? AND scope = ? AND source = ?
	`, normalizePrincipalID(principalID), normalizeScope(scope), normalizeSource(source))
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
		CREATE TABLE IF NOT EXISTS entitlements (
			principal_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			source TEXT NOT NULL,
			granted_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (principal_id, scope, source)
		);
		CREATE INDEX IF NOT EXISTS idx_entitlements_principal ON entitlements(principal_id, scope);
		CREATE INDEX IF NOT EXISTS idx_entitlements_expires ON entitlements(expires_at_ms);
	`); err != nil {
		return err
	}

	if currentVersion < sqliteSchemaVersion {
		if err := normalizeEntitlementsTable(tx); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

// normalizeEntitlementsTable rewrites legacy rows into canonical storage form.
// If multiple legacy rows collapse to the same normalized key, the newest row is
// treated as authoritative. "Newest" is defined deterministically as the row
// that sorts first by updated_at_ms DESC, granted_at_ms DESC, then raw key ASC;
// later collisions are discarded.
func normalizeEntitlementsTable(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE entitlements_v2 (
			principal_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			source TEXT NOT NULL,
			granted_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER,
			updated_at_ms INTEGER NOT NULL,
			PRIMARY KEY (principal_id, scope, source)
		);
	`); err != nil {
		return err
	}

	rows, err := tx.Query(`
		SELECT principal_id, scope, source, granted_at_ms, expires_at_ms, updated_at_ms
		FROM entitlements
		ORDER BY updated_at_ms DESC, granted_at_ms DESC, principal_id ASC, scope ASC, source ASC
	`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	for rows.Next() {
		var (
			principalID string
			scope       string
			source      string
			grantedAtMS int64
			expiresAtMS sql.NullInt64
			updatedAtMS int64
		)
		if err := rows.Scan(&principalID, &scope, &source, &grantedAtMS, &expiresAtMS, &updatedAtMS); err != nil {
			return err
		}

		normalizedPrincipalID := normalizePrincipalID(principalID)
		normalizedScope := normalizeScope(scope)
		normalizedSource := normalizeSource(source)
		if normalizedPrincipalID == "" {
			return fmt.Errorf("normalize entitlement row: principal id must not be empty")
		}
		if normalizedScope == "" {
			return fmt.Errorf("normalize entitlement row for principal %q: scope must not be empty", normalizedPrincipalID)
		}
		if normalizedSource == "" {
			return fmt.Errorf("normalize entitlement row for principal %q scope %q: source must not be empty", normalizedPrincipalID, normalizedScope)
		}

		key := grantKey(normalizedPrincipalID, normalizedScope, normalizedSource)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		var expiresValue any
		if expiresAtMS.Valid {
			expiresValue = expiresAtMS.Int64
		}
		if _, err := tx.Exec(`
			INSERT INTO entitlements_v2 (
				principal_id,
				scope,
				source,
				granted_at_ms,
				expires_at_ms,
				updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?)
		`, normalizedPrincipalID, normalizedScope, normalizedSource, grantedAtMS, expiresValue, updatedAtMS); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		DROP TABLE entitlements;
		ALTER TABLE entitlements_v2 RENAME TO entitlements;
		CREATE INDEX IF NOT EXISTS idx_entitlements_principal ON entitlements(principal_id, scope);
		CREATE INDEX IF NOT EXISTS idx_entitlements_expires ON entitlements(expires_at_ms);
	`); err != nil {
		return err
	}
	return nil
}

func nullableUnixMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	ts := time.UnixMilli(value.Int64).UTC()
	return &ts
}
