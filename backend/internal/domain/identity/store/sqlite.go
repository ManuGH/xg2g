// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(dbPath string, cfg sqlite.Config) (*SQLiteStore, error) {
	db, err := sqlite.Open(dbPath, cfg)
	if err != nil {
		return nil, err
	}
	s := &SQLiteStore{db: db}
	if err := s.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("identity sqlite init schema failed: %w", err)
	}
	return s, nil
}

// NewSQLiteStore opens an SQLite identity store with default config.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	return OpenSQLite(dbPath, sqlite.DefaultConfig())
}

// OpenStateStore opens an identity store given backend name and database path.
func OpenStateStore(backend, path string) (identity.Store, error) {
	if backend == "memory" || path == ":memory:" {
		return NewSQLiteStore(":memory:")
	}
	return NewSQLiteStore(path)
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLiteStore) initSchema(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		display_name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'viewer',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS passkey_credentials (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		public_key BLOB NOT NULL,
		attestation_type TEXT NOT NULL,
		aaguid TEXT NOT NULL,
		sign_count INTEGER NOT NULL DEFAULT 0,
		backup_eligible BOOLEAN NOT NULL DEFAULT 0,
		backup_state BOOLEAN NOT NULL DEFAULT 0,
		transports TEXT,
		nickname TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		last_used_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS recovery_codes (
		code_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL,
		consumed_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS web_sessions (
		session_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		user_agent TEXT,
		last_ip TEXT,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		last_active_at TIMESTAMP NOT NULL,
		revoked_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bootstrap_tokens (
		token_hash TEXT PRIMARY KEY,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		consumed_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bootstrap_meta (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		initial_admin_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		recovery_codes_acknowledged_at TIMESTAMP,
		bootstrap_closed BOOLEAN NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		device_name TEXT NOT NULL,
		platform TEXT NOT NULL,
		public_key_jwk TEXT NOT NULL,
		jwk_thumbprint TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP NOT NULL,
		last_seen_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS device_grants (
		id TEXT PRIMARY KEY,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		family_id TEXT NOT NULL,
		grant_type TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		revoked_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS refresh_token_families (
		token_hash TEXT PRIMARY KEY,
		family_id TEXT NOT NULL,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		generation INTEGER NOT NULL,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		rotated_at TIMESTAMP,
		revoked_at TIMESTAMP,
		replay_detected_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS access_tokens (
		token_hash TEXT PRIMARY KEY,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		bound_jkt TEXT NOT NULL,
		scopes TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		revoked_at TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_passkey_user_id ON passkey_credentials(user_id);
	CREATE INDEX IF NOT EXISTS idx_recovery_user_id ON recovery_codes(user_id);
	CREATE INDEX IF NOT EXISTS idx_web_sessions_user_id ON web_sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_web_sessions_expires_at ON web_sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_expires_at ON bootstrap_tokens(expires_at);
	CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices(user_id);
	CREATE INDEX IF NOT EXISTS idx_devices_jwk_thumbprint ON devices(jwk_thumbprint);
	CREATE INDEX IF NOT EXISTS idx_device_grants_family_id ON device_grants(family_id);
	CREATE INDEX IF NOT EXISTS idx_refresh_family_id ON refresh_token_families(family_id);
	CREATE INDEX IF NOT EXISTS idx_access_tokens_bound_jkt ON access_tokens(bound_jkt);
	`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// ----------------- UserStore -----------------

func (s *SQLiteStore) PutUser(ctx context.Context, user *identity.User) error {
	norm, err := user.Normalize()
	if err != nil {
		return err
	}
	query := `
	INSERT INTO users (id, username, display_name, role, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		username = excluded.username,
		display_name = excluded.display_name,
		role = excluded.role,
		updated_at = excluded.updated_at
	`
	_, err = s.db.ExecContext(ctx, query, norm.ID, norm.Username, norm.DisplayName, string(norm.Role), norm.CreatedAt.UTC(), norm.UpdatedAt.UTC())
	return err
}

func (s *SQLiteStore) GetUser(ctx context.Context, id string) (*identity.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, display_name, role, created_at, updated_at FROM users WHERE id = ?`, id)
	var u identity.User
	var roleStr string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &roleStr, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	u.Role = identity.Role(roleStr)
	return &u, nil
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*identity.User, error) {
	normUser := strings.ToLower(strings.TrimSpace(username))
	row := s.db.QueryRowContext(ctx, `SELECT id, username, display_name, role, created_at, updated_at FROM users WHERE username = ?`, normUser)
	var u identity.User
	var roleStr string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &roleStr, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	u.Role = identity.Role(roleStr)
	return &u, nil
}

func (s *SQLiteStore) ListUsers(ctx context.Context) ([]identity.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, display_name, role, created_at, updated_at FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identity.User
	for rows.Next() {
		var u identity.User
		var roleStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &roleStr, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		u.Role = identity.Role(roleStr)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateUser(ctx context.Context, id string, fn func(*identity.User) error) (*identity.User, error) {
	user, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := fn(user); err != nil {
		return nil, err
	}
	user.UpdatedAt = time.Now().UTC()
	if err := s.PutUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}

// ----------------- PasskeyStore -----------------

func (s *SQLiteStore) PutPasskey(ctx context.Context, passkey *identity.PasskeyCredential) error {
	if err := passkey.Validate(); err != nil {
		return err
	}

	transportsJSON, _ := json.Marshal(passkey.Transports)
	query := `
	INSERT INTO passkey_credentials (
		id, user_id, public_key, attestation_type, aaguid, sign_count,
		backup_eligible, backup_state, transports, nickname, created_at, last_used_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		sign_count = excluded.sign_count,
		backup_state = excluded.backup_state,
		last_used_at = excluded.last_used_at,
		nickname = excluded.nickname
	`
	_, err := s.db.ExecContext(ctx, query,
		passkey.ID,
		passkey.UserID,
		passkey.PublicKey,
		passkey.AttestationType,
		passkey.AAGUID,
		passkey.SignCount,
		passkey.BackupEligible,
		passkey.BackupState,
		string(transportsJSON),
		passkey.Nickname,
		passkey.CreatedAt.UTC(),
		passkey.LastUsedAt,
	)
	return err
}

func (s *SQLiteStore) GetPasskey(ctx context.Context, id string) (*identity.PasskeyCredential, error) {
	query := `
	SELECT id, user_id, public_key, attestation_type, aaguid, sign_count,
	       backup_eligible, backup_state, transports, nickname, created_at, last_used_at
	FROM passkey_credentials WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	var p identity.PasskeyCredential
	var transportsStr string
	if err := row.Scan(
		&p.ID,
		&p.UserID,
		&p.PublicKey,
		&p.AttestationType,
		&p.AAGUID,
		&p.SignCount,
		&p.BackupEligible,
		&p.BackupState,
		&transportsStr,
		&p.Nickname,
		&p.CreatedAt,
		&p.LastUsedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	if transportsStr != "" {
		_ = json.Unmarshal([]byte(transportsStr), &p.Transports)
	}
	return &p, nil
}

func (s *SQLiteStore) ListPasskeysByUser(ctx context.Context, userID string) ([]identity.PasskeyCredential, error) {
	query := `
	SELECT id, user_id, public_key, attestation_type, aaguid, sign_count,
	       backup_eligible, backup_state, transports, nickname, created_at, last_used_at
	FROM passkey_credentials WHERE user_id = ? ORDER BY created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identity.PasskeyCredential
	for rows.Next() {
		var p identity.PasskeyCredential
		var transportsStr string
		if err := rows.Scan(
			&p.ID,
			&p.UserID,
			&p.PublicKey,
			&p.AttestationType,
			&p.AAGUID,
			&p.SignCount,
			&p.BackupEligible,
			&p.BackupState,
			&transportsStr,
			&p.Nickname,
			&p.CreatedAt,
			&p.LastUsedAt,
		); err != nil {
			return nil, err
		}
		if transportsStr != "" {
			_ = json.Unmarshal([]byte(transportsStr), &p.Transports)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdatePasskey(ctx context.Context, id string, signCount uint32, backupState bool, lastUsedAt time.Time) error {
	query := `
	UPDATE passkey_credentials
	SET sign_count = ?, backup_state = ?, last_used_at = ?
	WHERE id = ?
	`
	res, err := s.db.ExecContext(ctx, query, signCount, backupState, lastUsedAt.UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}

func (s *SQLiteStore) DeletePasskey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkey_credentials WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}

// ----------------- RecoveryStore -----------------

func (s *SQLiteStore) PutRecoveryCodes(ctx context.Context, codes []identity.RecoveryCode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO recovery_codes (code_hash, user_id, created_at, consumed_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range codes {
		if _, err := stmt.ExecContext(ctx, c.CodeHash, c.UserID, c.CreatedAt.UTC(), c.ConsumedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ReplaceRecoveryCodesForUser(ctx context.Context, userID string, codes []identity.RecoveryCode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("failed to delete old recovery codes: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO recovery_codes (code_hash, user_id, created_at, consumed_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range codes {
		if _, err := stmt.ExecContext(ctx, c.CodeHash, c.UserID, c.CreatedAt.UTC(), c.ConsumedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListRecoveryCodesByUser(ctx context.Context, userID string) ([]identity.RecoveryCode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code_hash, user_id, created_at, consumed_at FROM recovery_codes WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identity.RecoveryCode
	for rows.Next() {
		var c identity.RecoveryCode
		if err := rows.Scan(&c.CodeHash, &c.UserID, &c.CreatedAt, &c.ConsumedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) error {
	query := `
	UPDATE recovery_codes
	SET consumed_at = ?
	WHERE user_id = ? AND code_hash = ? AND consumed_at IS NULL
	`
	res, err := s.db.ExecContext(ctx, query, now.UTC(), userID, codeHash)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrRecoveryCodeNotFound
	}
	return nil
}

// ----------------- WebSessionStore -----------------

func (s *SQLiteStore) PutSession(ctx context.Context, session *identity.WebSession) error {
	if strings.TrimSpace(session.SessionID) == "" {
		return identity.ErrInvalidSessionID
	}
	query := `
	INSERT INTO web_sessions (session_id, user_id, user_agent, last_ip, created_at, expires_at, last_active_at, revoked_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		user_agent = excluded.user_agent,
		last_ip = excluded.last_ip,
		expires_at = excluded.expires_at,
		last_active_at = excluded.last_active_at,
		revoked_at = excluded.revoked_at
	`
	_, err := s.db.ExecContext(ctx, query,
		session.SessionID,
		session.UserID,
		session.UserAgent,
		session.LastIP,
		session.CreatedAt.UTC(),
		session.ExpiresAt.UTC(),
		session.LastActiveAt.UTC(),
		session.RevokedAt,
	)
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, sessionID string) (*identity.WebSession, error) {
	query := `
	SELECT session_id, user_id, user_agent, last_ip, created_at, expires_at, last_active_at, revoked_at
	FROM web_sessions WHERE session_id = ?
	`
	row := s.db.QueryRowContext(ctx, query, sessionID)
	var ws identity.WebSession
	if err := row.Scan(
		&ws.SessionID,
		&ws.UserID,
		&ws.UserAgent,
		&ws.LastIP,
		&ws.CreatedAt,
		&ws.ExpiresAt,
		&ws.LastActiveAt,
		&ws.RevokedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	return &ws, nil
}

func (s *SQLiteStore) TouchSession(ctx context.Context, sessionID string, now time.Time, userAgent, ip string) error {
	query := `
	UPDATE web_sessions
	SET last_active_at = ?,
	    user_agent = CASE WHEN ? != '' THEN ? ELSE user_agent END,
	    last_ip = CASE WHEN ? != '' THEN ? ELSE last_ip END
	WHERE session_id = ? AND revoked_at IS NULL AND expires_at > ?
	`
	res, err := s.db.ExecContext(ctx, query, now.UTC(), userAgent, userAgent, ip, ip, sessionID, now.UTC())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}

func (s *SQLiteStore) RevokeSession(ctx context.Context, sessionID string, now time.Time) error {
	query := `
	UPDATE web_sessions
	SET revoked_at = ?
	WHERE session_id = ? AND revoked_at IS NULL
	`
	res, err := s.db.ExecContext(ctx, query, now.UTC(), sessionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}

func (s *SQLiteStore) RevokeOtherSessions(ctx context.Context, userID, currentSessionID string, now time.Time) error {
	query := `
	UPDATE web_sessions
	SET revoked_at = ?
	WHERE user_id = ? AND session_id != ? AND revoked_at IS NULL
	`
	_, err := s.db.ExecContext(ctx, query, now.UTC(), userID, currentSessionID)
	return err
}

func (s *SQLiteStore) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	query := `DELETE FROM web_sessions WHERE expires_at <= ? OR revoked_at IS NOT NULL`
	res, err := s.db.ExecContext(ctx, query, now.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ----------------- BootstrapStore -----------------

func (s *SQLiteStore) PutBootstrapToken(ctx context.Context, tokenHash string, createdAt, expiresAt time.Time) error {
	if tokenHash == "" {
		return identity.ErrBootstrapTokenInvalid
	}
	query := `
	INSERT INTO bootstrap_tokens (token_hash, created_at, expires_at)
	VALUES (?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, tokenHash, createdAt.UTC(), expiresAt.UTC())
	return err
}

func (s *SQLiteStore) ConsumeBootstrapToken(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	if tokenHash == "" {
		return false, identity.ErrBootstrapTokenInvalid
	}
	query := `
	UPDATE bootstrap_tokens
	SET consumed_at = ?
	WHERE token_hash = ? AND expires_at > ? AND consumed_at IS NULL
	`
	res, err := s.db.ExecContext(ctx, query, now.UTC(), tokenHash, now.UTC())
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *SQLiteStore) CommitInitialAdminBootstrap(ctx context.Context, user *identity.User, cred *identity.PasskeyCredential, recoveryCodes []identity.RecoveryCode, session *identity.WebSession) error {
	if user == nil {
		return identity.ErrUserNotFound
	}
	normUser, err := user.Normalize()
	if err != nil {
		return err
	}
	if cred == nil {
		return identity.ErrCredentialNotFound
	}
	if err := cred.Validate(); err != nil {
		return err
	}
	if len(recoveryCodes) == 0 {
		return identity.ErrRecoveryCodeNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin bootstrap tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// 1. Atomic Check: Verify exactly 0 users exist
	var userCount int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&userCount); err != nil {
		return fmt.Errorf("failed to check existing user count: %w", err)
	}
	if userCount > 0 {
		return identity.ErrAdminAlreadyExists
	}

	// 2. Insert Initial Admin User
	insertUserQuery := `
	INSERT INTO users (id, username, display_name, role, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	`
	if _, err := tx.ExecContext(ctx, insertUserQuery, normUser.ID, normUser.Username, normUser.DisplayName, string(normUser.Role), normUser.CreatedAt.UTC(), normUser.UpdatedAt.UTC()); err != nil {
		return fmt.Errorf("failed to insert initial admin user: %w", err)
	}

	// 3. Insert Passkey Credential
	transportsJSON, _ := json.Marshal(cred.Transports)
	insertCredQuery := `
	INSERT INTO passkey_credentials (
		id, user_id, public_key, attestation_type, aaguid,
		sign_count, backup_eligible, backup_state, transports,
		nickname, created_at, last_used_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var lastUsed *time.Time
	if cred.LastUsedAt != nil {
		t := cred.LastUsedAt.UTC()
		lastUsed = &t
	}
	if _, err := tx.ExecContext(ctx, insertCredQuery,
		cred.ID, normUser.ID, cred.PublicKey, cred.AttestationType, cred.AAGUID,
		cred.SignCount, cred.BackupEligible, cred.BackupState, string(transportsJSON),
		cred.Nickname, cred.CreatedAt.UTC(), lastUsed,
	); err != nil {
		return fmt.Errorf("failed to insert initial admin passkey: %w", err)
	}

	// 4. Insert Recovery Codes
	insertCodeQuery := `
	INSERT INTO recovery_codes (code_hash, user_id, created_at, consumed_at)
	VALUES (?, ?, ?, NULL)
	`
	for _, code := range recoveryCodes {
		if _, err := tx.ExecContext(ctx, insertCodeQuery, code.CodeHash, normUser.ID, code.CreatedAt.UTC()); err != nil {
			return fmt.Errorf("failed to insert recovery code: %w", err)
		}
	}

	// 5. Insert Active Web Session if provided
	if session != nil && session.SessionID != "" {
		insertSessQuery := `
		INSERT INTO web_sessions (session_id, user_id, user_agent, last_ip, created_at, expires_at, last_active_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		`
		if _, err := tx.ExecContext(ctx, insertSessQuery, session.SessionID, normUser.ID, session.UserAgent, session.LastIP, session.CreatedAt.UTC(), session.ExpiresAt.UTC(), session.LastActiveAt.UTC()); err != nil {
			return fmt.Errorf("failed to insert initial web session: %w", err)
		}
	}

	// 6. Record Bootstrap Meta (bootstrap_closed=0 until recovery codes acknowledged)
	metaQuery := `
	INSERT INTO bootstrap_meta (id, initial_admin_id, recovery_codes_acknowledged_at, bootstrap_closed, created_at, updated_at)
	VALUES (1, ?, NULL, 0, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		initial_admin_id = excluded.initial_admin_id,
		recovery_codes_acknowledged_at = NULL,
		bootstrap_closed = 0,
		updated_at = excluded.updated_at
	`
	now := normUser.CreatedAt.UTC()
	if _, err := tx.ExecContext(ctx, metaQuery, normUser.ID, now, now); err != nil {
		return fmt.Errorf("failed to insert bootstrap metadata: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) AcknowledgeRecoveryCodes(ctx context.Context, userID string, now time.Time) error {
	query := `
	INSERT INTO bootstrap_meta (id, initial_admin_id, recovery_codes_acknowledged_at, bootstrap_closed, created_at, updated_at)
	VALUES (1, ?, ?, 1, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		recovery_codes_acknowledged_at = excluded.recovery_codes_acknowledged_at,
		bootstrap_closed = 1,
		updated_at = excluded.updated_at
	`
	t := now.UTC()
	_, err := s.db.ExecContext(ctx, query, userID, t, t, t)
	return err
}

func (s *SQLiteStore) GetBootstrapMeta(ctx context.Context) (*identity.BootstrapMeta, error) {
	query := `
	SELECT initial_admin_id, recovery_codes_acknowledged_at, bootstrap_closed, created_at, updated_at
	FROM bootstrap_meta
	WHERE id = 1
	`
	var (
		initialAdminID sql.NullString
		ackAt          sql.NullTime
		closed         bool
		createdAt      time.Time
		updatedAt      time.Time
	)
	err := s.db.QueryRowContext(ctx, query).Scan(&initialAdminID, &ackAt, &closed, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &identity.BootstrapMeta{
				BootstrapClosed: false,
			}, nil
		}
		return nil, err
	}

	meta := &identity.BootstrapMeta{
		BootstrapClosed: closed,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
	if initialAdminID.Valid {
		meta.InitialAdminID = initialAdminID.String
	}
	if ackAt.Valid {
		t := ackAt.Time
		meta.RecoveryCodesAcknowledgedAt = &t
	}
	return meta, nil
}

// ----------------- DeviceStore -----------------

func (s *SQLiteStore) PutDevice(ctx context.Context, dev *identity.Device) error {
	if strings.TrimSpace(dev.ID) == "" {
		return identity.ErrInvalidUserID
	}
	query := `
	INSERT INTO devices (id, user_id, device_name, platform, public_key_jwk, jwk_thumbprint, created_at, last_seen_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		device_name = excluded.device_name,
		platform = excluded.platform,
		public_key_jwk = excluded.public_key_jwk,
		jwk_thumbprint = excluded.jwk_thumbprint,
		last_seen_at = excluded.last_seen_at
	`
	_, err := s.db.ExecContext(ctx, query,
		dev.ID, dev.UserID, dev.DeviceName, dev.Platform,
		dev.PublicKeyJWK, dev.JWKThumbprint,
		dev.CreatedAt.UTC(), dev.LastSeenAt.UTC(),
	)
	return err
}

func (s *SQLiteStore) GetDevice(ctx context.Context, id string) (*identity.Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id, device_name, platform, public_key_jwk, jwk_thumbprint, created_at, last_seen_at FROM devices WHERE id = ?`, id)
	var d identity.Device
	if err := row.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Platform, &d.PublicKeyJWK, &d.JWKThumbprint, &d.CreatedAt, &d.LastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *SQLiteStore) GetDeviceByThumbprint(ctx context.Context, thumbprint string) (*identity.Device, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id, device_name, platform, public_key_jwk, jwk_thumbprint, created_at, last_seen_at FROM devices WHERE jwk_thumbprint = ?`, thumbprint)
	var d identity.Device
	if err := row.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Platform, &d.PublicKeyJWK, &d.JWKThumbprint, &d.CreatedAt, &d.LastSeenAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *SQLiteStore) ListDevicesByUser(ctx context.Context, userID string) ([]identity.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, device_name, platform, public_key_jwk, jwk_thumbprint, created_at, last_seen_at FROM devices WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []identity.Device
	for rows.Next() {
		var d identity.Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Platform, &d.PublicKeyJWK, &d.JWKThumbprint, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) PutDeviceGrant(ctx context.Context, grant *identity.DeviceGrant, initialToken *identity.RefreshTokenFamily) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	grantQuery := `
	INSERT INTO device_grants (id, device_id, user_id, family_id, grant_type, created_at, revoked_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	var revokedAt *time.Time
	if grant.RevokedAt != nil {
		t := grant.RevokedAt.UTC()
		revokedAt = &t
	}
	if _, err := tx.ExecContext(ctx, grantQuery, grant.ID, grant.DeviceID, grant.UserID, grant.FamilyID, grant.GrantType, grant.CreatedAt.UTC(), revokedAt); err != nil {
		return fmt.Errorf("failed to insert device grant: %w", err)
	}

	tokenQuery := `
	INSERT INTO refresh_token_families (token_hash, family_id, device_id, generation, created_at, expires_at, rotated_at, revoked_at, replay_detected_at)
	VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL)
	`
	if _, err := tx.ExecContext(ctx, tokenQuery, initialToken.TokenHash, initialToken.FamilyID, initialToken.DeviceID, initialToken.Generation, initialToken.CreatedAt.UTC(), initialToken.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("failed to insert initial refresh token family: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetDeviceGrant(ctx context.Context, grantID string) (*identity.DeviceGrant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, device_id, user_id, family_id, grant_type, created_at, revoked_at FROM device_grants WHERE id = ?`, grantID)
	var g identity.DeviceGrant
	var revokedAt sql.NullTime
	if err := row.Scan(&g.ID, &g.DeviceID, &g.UserID, &g.FamilyID, &g.GrantType, &g.CreatedAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		g.RevokedAt = &t
	}
	return &g, nil
}

func (s *SQLiteStore) GetRefreshTokenFamily(ctx context.Context, tokenHash string) (*identity.RefreshTokenFamily, error) {
	row := s.db.QueryRowContext(ctx, `SELECT token_hash, family_id, device_id, generation, created_at, expires_at, rotated_at, revoked_at, replay_detected_at FROM refresh_token_families WHERE token_hash = ?`, tokenHash)
	var f identity.RefreshTokenFamily
	var rotatedAt, revokedAt, replayDetectedAt sql.NullTime
	if err := row.Scan(&f.TokenHash, &f.FamilyID, &f.DeviceID, &f.Generation, &f.CreatedAt, &f.ExpiresAt, &rotatedAt, &revokedAt, &replayDetectedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	if rotatedAt.Valid {
		t := rotatedAt.Time
		f.RotatedAt = &t
	}
	if revokedAt.Valid {
		t := revokedAt.Time
		f.RevokedAt = &t
	}
	if replayDetectedAt.Valid {
		t := replayDetectedAt.Time
		f.ReplayDetectedAt = &t
	}
	return &f, nil
}

func (s *SQLiteStore) RotateRefreshToken(ctx context.Context, oldTokenHash, newTokenHash string, newExpiresAt, now time.Time) (*identity.RefreshTokenFamily, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		familyID         string
		deviceID         string
		generation       int
		expiresAt        time.Time
		rotatedAt        sql.NullTime
		revokedAt        sql.NullTime
		replayDetectedAt sql.NullTime
	)

	row := tx.QueryRowContext(ctx, `SELECT family_id, device_id, generation, expires_at, rotated_at, revoked_at, replay_detected_at FROM refresh_token_families WHERE token_hash = ?`, oldTokenHash)
	if err := row.Scan(&familyID, &deviceID, &generation, &expiresAt, &rotatedAt, &revokedAt, &replayDetectedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}

	nowUTC := now.UTC()

	// REPLAY DETECTED or EXPIRED/REVOKED TOKEN USED!
	if rotatedAt.Valid || revokedAt.Valid || replayDetectedAt.Valid {
		// Mark replay detected and revoke entire family for this DEVICE ONLY
		_, _ = tx.ExecContext(ctx, `UPDATE refresh_token_families SET replay_detected_at = ?, revoked_at = ? WHERE family_id = ?`, nowUTC, nowUTC, familyID)
		_, _ = tx.ExecContext(ctx, `UPDATE device_grants SET revoked_at = ? WHERE family_id = ?`, nowUTC, familyID)
		_ = tx.Commit()
		return nil, identity.ErrRefreshTokenReplay
	}

	if nowUTC.After(expiresAt) {
		return nil, identity.ErrSessionExpired
	}

	// 1. Mark old token rotated
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_token_families SET rotated_at = ? WHERE token_hash = ?`, nowUTC, oldTokenHash); err != nil {
		return nil, fmt.Errorf("failed to mark refresh token rotated: %w", err)
	}

	// 2. Insert new token generation
	newToken := &identity.RefreshTokenFamily{
		TokenHash:  newTokenHash,
		FamilyID:   familyID,
		DeviceID:   deviceID,
		Generation: generation + 1,
		CreatedAt:  nowUTC,
		ExpiresAt:  newExpiresAt.UTC(),
	}

	insertQuery := `
	INSERT INTO refresh_token_families (token_hash, family_id, device_id, generation, created_at, expires_at, rotated_at, revoked_at, replay_detected_at)
	VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, NULL)
	`
	if _, err := tx.ExecContext(ctx, insertQuery, newToken.TokenHash, newToken.FamilyID, newToken.DeviceID, newToken.Generation, newToken.CreatedAt, newToken.ExpiresAt); err != nil {
		return nil, fmt.Errorf("failed to insert new refresh token generation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return newToken, nil
}

func (s *SQLiteStore) RevokeDeviceGrantFamily(ctx context.Context, familyID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	nowUTC := now.UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_token_families SET revoked_at = ? WHERE family_id = ?`, nowUTC, familyID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_grants SET revoked_at = ? WHERE family_id = ?`, nowUTC, familyID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) PutDPoPAccessToken(ctx context.Context, token *identity.DPoPAccessToken) error {
	query := `
	INSERT INTO access_tokens (token_hash, device_id, user_id, bound_jkt, scopes, created_at, expires_at, revoked_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	var revokedAt *time.Time
	if token.RevokedAt != nil {
		t := token.RevokedAt.UTC()
		revokedAt = &t
	}
	_, err := s.db.ExecContext(ctx, query,
		token.TokenHash, token.DeviceID, token.UserID,
		token.BoundJKT, token.Scopes,
		token.CreatedAt.UTC(), token.ExpiresAt.UTC(), revokedAt,
	)
	return err
}

func (s *SQLiteStore) GetDPoPAccessToken(ctx context.Context, tokenHash string) (*identity.DPoPAccessToken, error) {
	row := s.db.QueryRowContext(ctx, `SELECT token_hash, device_id, user_id, bound_jkt, scopes, created_at, expires_at, revoked_at FROM access_tokens WHERE token_hash = ?`, tokenHash)
	var t identity.DPoPAccessToken
	var revokedAt sql.NullTime
	if err := row.Scan(&t.TokenHash, &t.DeviceID, &t.UserID, &t.BoundJKT, &t.Scopes, &t.CreatedAt, &t.ExpiresAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	if revokedAt.Valid {
		r := revokedAt.Time
		t.RevokedAt = &r
	}
	return &t, nil
}

func (s *SQLiteStore) RevokeDPoPAccessToken(ctx context.Context, tokenHash string, now time.Time) error {
	query := `UPDATE access_tokens SET revoked_at = ? WHERE token_hash = ?`
	res, err := s.db.ExecContext(ctx, query, now.UTC(), tokenHash)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrStoreNotFound
	}
	return nil
}
