package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) PutDevice(ctx context.Context, record *model.DeviceRecord) error {
	prepared, err := cloneDeviceValue(record)
	if err != nil {
		return err
	}
	return putDevice(ctx, s.DB, prepared)
}

func (s *SqliteStore) GetDevice(ctx context.Context, deviceID string) (*model.DeviceRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT device_id, owner_id, device_name, device_type, policy_profile, capabilities_json,
			created_at_ms, last_seen_at_ms, revoked_at_ms
		FROM devices WHERE device_id = ?`, deviceID)
	record, err := scanDevice(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) ListDevicesByOwner(ctx context.Context, ownerID string) ([]model.DeviceRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT device_id, owner_id, device_name, device_type, policy_profile, capabilities_json,
			created_at_ms, last_seen_at_ms, revoked_at_ms
		FROM devices WHERE owner_id = ?
		ORDER BY created_at_ms ASC, device_id ASC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]model.DeviceRecord, 0)
	for rows.Next() {
		record, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SqliteStore) UpdateDevice(ctx context.Context, deviceID string, fn func(*model.DeviceRecord) error) (*model.DeviceRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := getDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return nil, err
	}
	if err := fn(&record); err != nil {
		return nil, err
	}
	record, err = model.PrepareDeviceRecord(record)
	if err != nil {
		return nil, err
	}
	if err := putDevice(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func cloneDeviceValue(record *model.DeviceRecord) (model.DeviceRecord, error) {
	if record == nil {
		return model.DeviceRecord{}, model.ErrInvalidDeviceID
	}
	return model.PrepareDeviceRecord(*record)
}

func putDevice(ctx context.Context, exec sqliteExecContext, record model.DeviceRecord) error {
	capabilitiesJSON, err := marshalCapabilities(record.Capabilities)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO devices (
			device_id, owner_id, device_name, device_type, policy_profile, capabilities_json,
			created_at_ms, last_seen_at_ms, revoked_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			owner_id = excluded.owner_id,
			device_name = excluded.device_name,
			device_type = excluded.device_type,
			policy_profile = excluded.policy_profile,
			capabilities_json = excluded.capabilities_json,
			created_at_ms = excluded.created_at_ms,
			last_seen_at_ms = excluded.last_seen_at_ms,
			revoked_at_ms = excluded.revoked_at_ms`,
		record.DeviceID, record.OwnerID, record.DeviceName, string(record.DeviceType),
		record.PolicyProfile, capabilitiesJSON, toMillis(record.CreatedAt),
		toNullableMillis(record.LastSeenAt), toNullableMillis(record.RevokedAt),
	)
	return normalizeSQLError(err)
}

func getDeviceTx(ctx context.Context, tx *sql.Tx, deviceID string) (model.DeviceRecord, error) {
	return scanDevice(tx.QueryRowContext(ctx, `
		SELECT device_id, owner_id, device_name, device_type, policy_profile, capabilities_json,
			created_at_ms, last_seen_at_ms, revoked_at_ms
		FROM devices WHERE device_id = ?`, deviceID))
}

func scanDevice(row sqlRowScanner) (model.DeviceRecord, error) {
	var record model.DeviceRecord
	var deviceType string
	var capabilitiesJSON []byte
	var createdAt int64
	var lastSeenAt, revokedAt sql.NullInt64
	if err := row.Scan(
		&record.DeviceID, &record.OwnerID, &record.DeviceName, &deviceType, &record.PolicyProfile,
		&capabilitiesJSON, &createdAt, &lastSeenAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DeviceRecord{}, ErrNotFound
		}
		return model.DeviceRecord{}, err
	}
	capabilities, err := unmarshalCapabilities(capabilitiesJSON)
	if err != nil {
		return model.DeviceRecord{}, err
	}
	record.DeviceType = model.DeviceType(deviceType)
	record.Capabilities = capabilities
	record.CreatedAt = fromMillis(createdAt)
	record.LastSeenAt = fromNullableMillis(lastSeenAt)
	record.RevokedAt = fromNullableMillis(revokedAt)
	return model.PrepareDeviceRecord(record)
}
