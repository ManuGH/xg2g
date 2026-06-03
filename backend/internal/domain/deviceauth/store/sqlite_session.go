package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func (s *SqliteStore) PutAccessSession(ctx context.Context, record *model.AccessSessionRecord) error {
	prepared, err := cloneSessionValue(record)
	if err != nil {
		return err
	}
	return putAccessSession(ctx, s.DB, prepared)
}

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

func (s *SqliteStore) UpdateAccessSession(ctx context.Context, sessionID string, fn func(*model.AccessSessionRecord) error) (*model.AccessSessionRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := getAccessSessionTx(ctx, tx, sessionID)
	if err != nil {
		return nil, err
	}
	if err := fn(&record); err != nil {
		return nil, err
	}
	record, err = model.PrepareAccessSessionRecord(record)
	if err != nil {
		return nil, err
	}
	if err := putAccessSession(ctx, tx, record); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SqliteStore) DeleteAccessSession(ctx context.Context, sessionID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM access_sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SqliteStore) DeleteAccessSessionsByDevice(ctx context.Context, deviceID string) (int, error) {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM access_sessions WHERE device_id = ?`, deviceID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func cloneSessionValue(record *model.AccessSessionRecord) (model.AccessSessionRecord, error) {
	if record == nil {
		return model.AccessSessionRecord{}, model.ErrInvalidSessionID
	}
	return model.PrepareAccessSessionRecord(*record)
}

func putAccessSession(ctx context.Context, exec sqliteExecContext, record model.AccessSessionRecord) error {
	scopesJSON, err := json.Marshal(record.Scopes)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO access_sessions (
			session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			subject_id = excluded.subject_id,
			device_id = excluded.device_id,
			token_hash = excluded.token_hash,
			policy_version = excluded.policy_version,
			scopes_json = excluded.scopes_json,
			auth_strength = excluded.auth_strength,
			issued_at_ms = excluded.issued_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			revoked_at_ms = excluded.revoked_at_ms`,
		record.SessionID, record.SubjectID, record.DeviceID, record.TokenHash,
		record.PolicyVersion, scopesJSON, record.AuthStrength, toMillis(record.IssuedAt),
		toMillis(record.ExpiresAt), toNullableMillis(record.RevokedAt),
	)
	return normalizeSQLError(err)
}

func getAccessSessionTx(ctx context.Context, tx *sql.Tx, sessionID string) (model.AccessSessionRecord, error) {
	return scanAccessSession(tx.QueryRowContext(ctx, `
		SELECT session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		FROM access_sessions WHERE session_id = ?`, sessionID))
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
