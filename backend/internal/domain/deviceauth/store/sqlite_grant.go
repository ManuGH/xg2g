package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) PutDeviceGrant(ctx context.Context, record *model.DeviceGrantRecord) error {
	prepared, err := cloneGrantValue(record)
	if err != nil {
		return err
	}
	return putGrant(ctx, s.DB, prepared)
}

func (s *SqliteStore) GetDeviceGrant(ctx context.Context, grantID string) (*model.DeviceGrantRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		FROM device_grants WHERE grant_id = ?`, grantID)
	record, err := scanGrant(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) GetActiveDeviceGrantByDevice(ctx context.Context, deviceID string) (*model.DeviceGrantRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		FROM device_grants
		WHERE device_id = ? AND revoked_at_ms IS NULL
		ORDER BY issued_at_ms DESC, grant_id ASC
		LIMIT 1`, deviceID)
	record, err := scanGrant(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) ListDeviceGrantsByDevice(ctx context.Context, deviceID string) ([]model.DeviceGrantRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		FROM device_grants WHERE device_id = ?
		ORDER BY issued_at_ms ASC, grant_id ASC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]model.DeviceGrantRecord, 0)
	for rows.Next() {
		record, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SqliteStore) UpdateDeviceGrant(ctx context.Context, grantID string, fn func(*model.DeviceGrantRecord) error) (*model.DeviceGrantRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := getGrantTx(ctx, tx, grantID)
	if err != nil {
		return nil, err
	}
	if err := fn(&record); err != nil {
		return nil, err
	}
	record, err = model.PrepareDeviceGrantRecord(record)
	if err != nil {
		return nil, err
	}
	if err := putGrant(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func cloneGrantValue(record *model.DeviceGrantRecord) (model.DeviceGrantRecord, error) {
	if record == nil {
		return model.DeviceGrantRecord{}, model.ErrInvalidGrantID
	}
	return model.PrepareDeviceGrantRecord(*record)
}

func putGrant(ctx context.Context, exec sqliteExecContext, record model.DeviceGrantRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO device_grants (
			grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(grant_id) DO UPDATE SET
			device_id = excluded.device_id,
			grant_hash = excluded.grant_hash,
			issued_at_ms = excluded.issued_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			rotate_after_ms = excluded.rotate_after_ms,
			last_used_at_ms = excluded.last_used_at_ms,
			revoked_at_ms = excluded.revoked_at_ms`,
		record.GrantID, record.DeviceID, record.GrantHash, toMillis(record.IssuedAt),
		toMillis(record.ExpiresAt), toNullableMillis(record.RotateAfter),
		toNullableMillis(record.LastUsedAt), toNullableMillis(record.RevokedAt),
	)
	return normalizeSQLError(err)
}

func getGrantTx(ctx context.Context, tx *sql.Tx, grantID string) (model.DeviceGrantRecord, error) {
	return scanGrant(tx.QueryRowContext(ctx, `
		SELECT grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		FROM device_grants WHERE grant_id = ?`, grantID))
}

func scanGrant(row sqlRowScanner) (model.DeviceGrantRecord, error) {
	var record model.DeviceGrantRecord
	var issuedAt, expiresAt int64
	var rotateAfter, lastUsedAt, revokedAt sql.NullInt64
	if err := row.Scan(
		&record.GrantID, &record.DeviceID, &record.GrantHash, &issuedAt, &expiresAt,
		&rotateAfter, &lastUsedAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DeviceGrantRecord{}, ErrNotFound
		}
		return model.DeviceGrantRecord{}, err
	}
	record.IssuedAt = fromMillis(issuedAt)
	record.ExpiresAt = fromMillis(expiresAt)
	record.RotateAfter = fromNullableMillis(rotateAfter)
	record.LastUsedAt = fromNullableMillis(lastUsedAt)
	record.RevokedAt = fromNullableMillis(revokedAt)
	return model.PrepareDeviceGrantRecord(record)
}
