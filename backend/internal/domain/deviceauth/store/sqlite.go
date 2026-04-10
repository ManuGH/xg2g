// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	sqliteSchemaVersion = 2
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
		return nil, fmt.Errorf("device auth store: migration failed: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	if err := s.DB.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
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

	schema := `
	CREATE TABLE IF NOT EXISTS pairings (
		pairing_id TEXT PRIMARY KEY,
		pairing_secret_hash TEXT NOT NULL,
		user_code TEXT NOT NULL UNIQUE,
		qr_payload TEXT NOT NULL,
		device_name TEXT NOT NULL,
		device_type TEXT NOT NULL,
		requested_policy_profile TEXT NOT NULL,
		approved_policy_profile TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		approved_at_ms INTEGER,
		consumed_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_pairings_user_code ON pairings(user_code);
	CREATE INDEX IF NOT EXISTS idx_pairings_status ON pairings(status);

	CREATE TABLE IF NOT EXISTS devices (
		device_id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL,
		device_name TEXT NOT NULL,
		device_type TEXT NOT NULL,
		policy_profile TEXT NOT NULL,
		capabilities_json TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_devices_owner_id ON devices(owner_id);

	CREATE TABLE IF NOT EXISTS device_grants (
		grant_id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL,
		grant_hash TEXT NOT NULL UNIQUE,
		issued_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		rotate_after_ms INTEGER,
		last_used_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_device_grants_device_id ON device_grants(device_id);

	CREATE TABLE IF NOT EXISTS access_sessions (
		session_id TEXT PRIMARY KEY,
		subject_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		policy_version TEXT NOT NULL,
		scopes_json TEXT NOT NULL,
		auth_strength TEXT NOT NULL,
		issued_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_access_sessions_device_id ON access_sessions(device_id);
	CREATE INDEX IF NOT EXISTS idx_access_sessions_token_hash ON access_sessions(token_hash);

	CREATE TABLE IF NOT EXISTS web_bootstraps (
		bootstrap_id TEXT PRIMARY KEY,
		bootstrap_secret_hash TEXT NOT NULL,
		source_access_session_id TEXT NOT NULL,
		target_path TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		consumed_at_ms INTEGER,
		revoked_at_ms INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_web_bootstraps_source_session_id ON web_bootstraps(source_access_session_id);
	`
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", sqliteSchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

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
	defer rows.Close()
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
	defer rows.Close()
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
	defer rows.Close()
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

func clonePairingValue(record *model.PairingRecord) (model.PairingRecord, error) {
	if record == nil {
		return model.PairingRecord{}, model.ErrInvalidPairingID
	}
	return model.PreparePairingRecord(*record)
}

func cloneDeviceValue(record *model.DeviceRecord) (model.DeviceRecord, error) {
	if record == nil {
		return model.DeviceRecord{}, model.ErrInvalidDeviceID
	}
	return model.PrepareDeviceRecord(*record)
}

func cloneGrantValue(record *model.DeviceGrantRecord) (model.DeviceGrantRecord, error) {
	if record == nil {
		return model.DeviceGrantRecord{}, model.ErrInvalidGrantID
	}
	return model.PrepareDeviceGrantRecord(*record)
}

func cloneSessionValue(record *model.AccessSessionRecord) (model.AccessSessionRecord, error) {
	if record == nil {
		return model.AccessSessionRecord{}, model.ErrInvalidSessionID
	}
	return model.PrepareAccessSessionRecord(*record)
}

func cloneWebBootstrapValue(record *model.WebBootstrapRecord) (model.WebBootstrapRecord, error) {
	if record == nil {
		return model.WebBootstrapRecord{}, model.ErrInvalidWebBootstrapID
	}
	return model.PrepareWebBootstrapRecord(*record)
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

func getPairingTx(ctx context.Context, tx *sql.Tx, pairingID string) (model.PairingRecord, error) {
	return scanPairing(tx.QueryRowContext(ctx, `
		SELECT pairing_id, pairing_secret_hash, user_code, qr_payload, device_name, device_type,
			requested_policy_profile, approved_policy_profile, owner_id, status, created_at_ms,
			expires_at_ms, approved_at_ms, consumed_at_ms, revoked_at_ms
		FROM pairings WHERE pairing_id = ?`, pairingID))
}

func getDeviceTx(ctx context.Context, tx *sql.Tx, deviceID string) (model.DeviceRecord, error) {
	return scanDevice(tx.QueryRowContext(ctx, `
		SELECT device_id, owner_id, device_name, device_type, policy_profile, capabilities_json,
			created_at_ms, last_seen_at_ms, revoked_at_ms
		FROM devices WHERE device_id = ?`, deviceID))
}

func getGrantTx(ctx context.Context, tx *sql.Tx, grantID string) (model.DeviceGrantRecord, error) {
	return scanGrant(tx.QueryRowContext(ctx, `
		SELECT grant_id, device_id, grant_hash, issued_at_ms, expires_at_ms, rotate_after_ms,
			last_used_at_ms, revoked_at_ms
		FROM device_grants WHERE grant_id = ?`, grantID))
}

func getAccessSessionTx(ctx context.Context, tx *sql.Tx, sessionID string) (model.AccessSessionRecord, error) {
	return scanAccessSession(tx.QueryRowContext(ctx, `
		SELECT session_id, subject_id, device_id, token_hash, policy_version, scopes_json,
			auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms
		FROM access_sessions WHERE session_id = ?`, sessionID))
}

func getWebBootstrapTx(ctx context.Context, tx *sql.Tx, bootstrapID string) (model.WebBootstrapRecord, error) {
	return scanWebBootstrap(tx.QueryRowContext(ctx, `
		SELECT bootstrap_id, bootstrap_secret_hash, source_access_session_id, target_path,
			created_at_ms, expires_at_ms, consumed_at_ms, revoked_at_ms
		FROM web_bootstraps WHERE bootstrap_id = ?`, bootstrapID))
}

type sqliteExecContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type sqlRowScanner interface {
	Scan(dest ...any) error
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

func marshalCapabilities(capabilities map[string]any) ([]byte, error) {
	if capabilities == nil {
		return []byte(`{}`), nil
	}
	return json.Marshal(capabilities)
}

func unmarshalCapabilities(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var capabilities map[string]any
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return nil, err
	}
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	return capabilities, nil
}

func toMillis(value time.Time) int64 {
	return value.UTC().UnixMilli()
}

func toNullableMillis(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().UnixMilli()
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func fromNullableMillis(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.UnixMilli(value.Int64).UTC()
	return &t
}

func normalizeSQLError(err error) error {
	if err == nil {
		return nil
	}
	if sqlitepkg.IsBusyRetryable(err) {
		return err
	}
	return err
}
