package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

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
