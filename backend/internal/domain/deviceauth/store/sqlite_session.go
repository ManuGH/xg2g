package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) GetAccessSession(ctx context.Context, sessionID string) (*model.AccessSessionRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		FROM access_sessions WHERE session_id = ?`, sessionID)
	record, err := scanAccessSession(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) GetAccessSessionByTokenHash(ctx context.Context, tokenHash string) (*model.AccessSessionRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		FROM access_sessions WHERE token_hash = ?`, tokenHash)
	record, err := scanAccessSession(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) ListAccessSessionsByDevice(ctx context.Context, deviceID string) ([]model.AccessSessionRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		FROM access_sessions WHERE device_id = ?
		ORDER BY issued_at_ms ASC, session_id ASC`, deviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]model.AccessSessionRecord, 0)
	for rows.Next() {
		record, err := scanAccessSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func scanAccessSession(row sqlRowScanner) (model.AccessSessionRecord, error) {
	var record model.AccessSessionRecord
	var scopesJSON []byte
	var issuedAt, expiresAt int64
	var revokedAt sql.NullInt64
	if err := row.Scan(
		&record.SessionID, &record.SubjectID, &record.DeviceID, &record.TokenHash, &record.PolicyVersion,
		&scopesJSON, &record.AuthStrength, &issuedAt, &expiresAt, &revokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.AccessSessionRecord{}, ErrNotFound
		}
		return model.AccessSessionRecord{}, err
	}
	if err := json.Unmarshal(scopesJSON, &record.Scopes); err != nil {
		return model.AccessSessionRecord{}, err
	}
	record.IssuedAt = fromMillis(issuedAt)
	record.ExpiresAt = fromMillis(expiresAt)
	record.RevokedAt = fromNullableMillis(revokedAt)
	return model.PrepareAccessSessionRecord(record)
}
