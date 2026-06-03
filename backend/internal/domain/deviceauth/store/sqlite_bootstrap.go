package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) PutWebBootstrap(ctx context.Context, record *model.WebBootstrapRecord) error {
	prepared, err := cloneWebBootstrapValue(record)
	if err != nil {
		return err
	}
	return putWebBootstrap(ctx, s.DB, prepared)
}

func (s *SqliteStore) GetWebBootstrap(ctx context.Context, bootstrapID string) (*model.WebBootstrapRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT bootstrap_id, bootstrap_secret_hash, source_access_session_id, target_path,
			created_at_ms, expires_at_ms, consumed_at_ms, revoked_at_ms
		FROM web_bootstraps WHERE bootstrap_id = ?`, bootstrapID)
	record, err := scanWebBootstrap(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) UpdateWebBootstrap(ctx context.Context, bootstrapID string, fn func(*model.WebBootstrapRecord) error) (*model.WebBootstrapRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := getWebBootstrapTx(ctx, tx, bootstrapID)
	if err != nil {
		return nil, err
	}
	if err := fn(&record); err != nil {
		return nil, err
	}
	record, err = model.PrepareWebBootstrapRecord(record)
	if err != nil {
		return nil, err
	}
	if err := putWebBootstrap(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func cloneWebBootstrapValue(record *model.WebBootstrapRecord) (model.WebBootstrapRecord, error) {
	if record == nil {
		return model.WebBootstrapRecord{}, model.ErrInvalidWebBootstrapID
	}
	return model.PrepareWebBootstrapRecord(*record)
}

func putWebBootstrap(ctx context.Context, exec sqliteExecContext, record model.WebBootstrapRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO web_bootstraps (
			bootstrap_id, bootstrap_secret_hash, source_access_session_id, target_path,
			created_at_ms, expires_at_ms, consumed_at_ms, revoked_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(bootstrap_id) DO UPDATE SET
			bootstrap_secret_hash = excluded.bootstrap_secret_hash,
			source_access_session_id = excluded.source_access_session_id,
			target_path = excluded.target_path,
			created_at_ms = excluded.created_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			consumed_at_ms = excluded.consumed_at_ms,
			revoked_at_ms = excluded.revoked_at_ms`,
		record.BootstrapID, record.BootstrapSecretHash, record.SourceAccessSessionID, record.TargetPath,
		toMillis(record.CreatedAt), toMillis(record.ExpiresAt), toNullableMillis(record.ConsumedAt),
		toNullableMillis(record.RevokedAt),
	)
	return normalizeSQLError(err)
}

func getWebBootstrapTx(ctx context.Context, tx *sql.Tx, bootstrapID string) (model.WebBootstrapRecord, error) {
	return scanWebBootstrap(tx.QueryRowContext(ctx, `
		SELECT bootstrap_id, bootstrap_secret_hash, source_access_session_id, target_path,
			created_at_ms, expires_at_ms, consumed_at_ms, revoked_at_ms
		FROM web_bootstraps WHERE bootstrap_id = ?`, bootstrapID))
}

func scanWebBootstrap(row sqlRowScanner) (model.WebBootstrapRecord, error) {
	var record model.WebBootstrapRecord
	var createdAt, expiresAt int64
	var consumedAt, revokedAt sql.NullInt64
	if err := row.Scan(
		&record.BootstrapID, &record.BootstrapSecretHash, &record.SourceAccessSessionID,
		&record.TargetPath, &createdAt, &expiresAt, &consumedAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.WebBootstrapRecord{}, ErrNotFound
		}
		return model.WebBootstrapRecord{}, err
	}
	record.CreatedAt = fromMillis(createdAt)
	record.ExpiresAt = fromMillis(expiresAt)
	record.ConsumedAt = fromNullableMillis(consumedAt)
	record.RevokedAt = fromNullableMillis(revokedAt)
	return model.PrepareWebBootstrapRecord(record)
}
