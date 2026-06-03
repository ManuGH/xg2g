package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) PutPairing(ctx context.Context, record *model.PairingRecord) error {
	prepared, err := clonePairingValue(record)
	if err != nil {
		return err
	}
	return putPairing(ctx, s.DB, prepared)
}

func (s *SqliteStore) GetPairing(ctx context.Context, pairingID string) (*model.PairingRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT pairing_id, pairing_secret_hash, user_code, qr_payload, device_name, device_type,
			requested_policy_profile, approved_policy_profile, owner_id, status, created_at_ms,
			expires_at_ms, approved_at_ms, consumed_at_ms, revoked_at_ms
		FROM pairings WHERE pairing_id = ?`, pairingID)
	record, err := scanPairing(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) GetPairingByUserCode(ctx context.Context, userCode string) (*model.PairingRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT pairing_id, pairing_secret_hash, user_code, qr_payload, device_name, device_type,
			requested_policy_profile, approved_policy_profile, owner_id, status, created_at_ms,
			expires_at_ms, approved_at_ms, consumed_at_ms, revoked_at_ms
		FROM pairings WHERE user_code = ?`, userCode)
	record, err := scanPairing(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) UpdatePairing(ctx context.Context, pairingID string, fn func(*model.PairingRecord) error) (*model.PairingRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	record, err := getPairingTx(ctx, tx, pairingID)
	if err != nil {
		return nil, err
	}
	if err := fn(&record); err != nil {
		return nil, err
	}
	record, err = model.PreparePairingRecord(record)
	if err != nil {
		return nil, err
	}
	if err := putPairing(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func clonePairingValue(record *model.PairingRecord) (model.PairingRecord, error) {
	if record == nil {
		return model.PairingRecord{}, model.ErrInvalidPairingID
	}
	return model.PreparePairingRecord(*record)
}

func putPairing(ctx context.Context, exec sqliteExecContext, record model.PairingRecord) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO pairings (
			pairing_id, pairing_secret_hash, user_code, qr_payload, device_name, device_type,
			requested_policy_profile, approved_policy_profile, owner_id, status, created_at_ms,
			expires_at_ms, approved_at_ms, consumed_at_ms, revoked_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pairing_id) DO UPDATE SET
			pairing_secret_hash = excluded.pairing_secret_hash,
			user_code = excluded.user_code,
			qr_payload = excluded.qr_payload,
			device_name = excluded.device_name,
			device_type = excluded.device_type,
			requested_policy_profile = excluded.requested_policy_profile,
			approved_policy_profile = excluded.approved_policy_profile,
			owner_id = excluded.owner_id,
			status = excluded.status,
			created_at_ms = excluded.created_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			approved_at_ms = excluded.approved_at_ms,
			consumed_at_ms = excluded.consumed_at_ms,
			revoked_at_ms = excluded.revoked_at_ms`,
		record.PairingID, record.PairingSecretHash, record.UserCode, record.QRPayload, record.DeviceName,
		string(record.DeviceType), record.RequestedPolicyProfile, record.ApprovedPolicyProfile,
		record.OwnerID, string(record.Status), toMillis(record.CreatedAt), toMillis(record.ExpiresAt),
		toNullableMillis(record.ApprovedAt), toNullableMillis(record.ConsumedAt), toNullableMillis(record.RevokedAt),
	)
	return normalizeSQLError(err)
}

func getPairingTx(ctx context.Context, tx *sql.Tx, pairingID string) (model.PairingRecord, error) {
	return scanPairing(tx.QueryRowContext(ctx, `
		SELECT pairing_id, pairing_secret_hash, user_code, qr_payload, device_name, device_type,
			requested_policy_profile, approved_policy_profile, owner_id, status, created_at_ms,
			expires_at_ms, approved_at_ms, consumed_at_ms, revoked_at_ms
		FROM pairings WHERE pairing_id = ?`, pairingID))
}

func scanPairing(row sqlRowScanner) (model.PairingRecord, error) {
	var record model.PairingRecord
	var deviceType, status string
	var approvedAt, consumedAt, revokedAt sql.NullInt64
	var createdAt, expiresAt int64
	if err := row.Scan(
		&record.PairingID, &record.PairingSecretHash, &record.UserCode, &record.QRPayload,
		&record.DeviceName, &deviceType, &record.RequestedPolicyProfile, &record.ApprovedPolicyProfile,
		&record.OwnerID, &status, &createdAt, &expiresAt, &approvedAt, &consumedAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PairingRecord{}, ErrNotFound
		}
		return model.PairingRecord{}, err
	}
	record.DeviceType = model.DeviceType(deviceType)
	record.Status = model.PairingStatus(status)
	record.CreatedAt = fromMillis(createdAt)
	record.ExpiresAt = fromMillis(expiresAt)
	record.ApprovedAt = fromNullableMillis(approvedAt)
	record.ConsumedAt = fromNullableMillis(consumedAt)
	record.RevokedAt = fromNullableMillis(revokedAt)
	return model.PreparePairingRecord(record)
}
