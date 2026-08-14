// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

	CREATE TABLE IF NOT EXISTS households (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS household_memberships (
		household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role TEXT NOT NULL CHECK (role IN ('admin', 'member', 'guest', 'viewer')),
		created_at TIMESTAMP NOT NULL,
		PRIMARY KEY (household_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS account_passwords (
		user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS account_invitations (
		id TEXT PRIMARY KEY,
		household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
		code_hash TEXT NOT NULL UNIQUE,
		role TEXT NOT NULL CHECK (role IN ('member', 'guest')),
		display_name TEXT,
		created_by_user_id TEXT NOT NULL REFERENCES users(id),
		expires_at TIMESTAMP NOT NULL,
		used_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS profiles (
		id TEXT PRIMARY KEY,
		household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		avatar_url TEXT,
		is_child BOOLEAN NOT NULL DEFAULT 0,
		created_by_user_id TEXT NOT NULL REFERENCES users(id),
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS profile_policies (
		profile_id TEXT PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
		allowed_bouquets TEXT,
		blocked_channels TEXT,
		maturity_level INTEGER DEFAULT 0,
		exit_pin_hash TEXT,
		dvr_allowed BOOLEAN NOT NULL DEFAULT 1,
		viewing_time_window TEXT
	);

	CREATE TABLE IF NOT EXISTS access_policies (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		valid_from TIMESTAMP,
		valid_until TIMESTAMP,
		daily_start TEXT,
		daily_end TEXT,
		timezone TEXT NOT NULL DEFAULT 'Europe/Vienna',
		allowed_days_mask INTEGER NOT NULL DEFAULT 127,
		live_tv_allowed BOOLEAN NOT NULL DEFAULT 1,
		epg_allowed BOOLEAN NOT NULL DEFAULT 1,
		dvr_allowed BOOLEAN NOT NULL DEFAULT 1,
		recordings_allowed BOOLEAN NOT NULL DEFAULT 1,
		max_devices INTEGER NOT NULL DEFAULT 3,
		revoked_at TIMESTAMP,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS approval_requests (
		id TEXT PRIMARY KEY,
		household_id TEXT NOT NULL REFERENCES households(id) ON DELETE CASCADE,
		profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		request_type TEXT NOT NULL,
		resource_id TEXT NOT NULL,
		resource_name TEXT NOT NULL,
		parental_rating INTEGER NOT NULL DEFAULT 0,
		scope TEXT NOT NULL DEFAULT 'single',
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		approved_by_user_id TEXT REFERENCES users(id),
		approved_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS household_resource_policies (
		household_id TEXT PRIMARY KEY REFERENCES households(id) ON DELETE CASCADE,
		max_concurrent_live_services INTEGER NOT NULL DEFAULT 3,
		max_concurrent_viewers INTEGER NOT NULL DEFAULT 5,
		max_parallel_recordings INTEGER NOT NULL DEFAULT 4,
		max_parallel_transcodes INTEGER NOT NULL DEFAULT 3,
		preemption_enabled BOOLEAN NOT NULL DEFAULT 0,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS recording_profile_access (
		recording_id TEXT NOT NULL,
		profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
		PRIMARY KEY (recording_id, profile_id)
	);

	CREATE TABLE IF NOT EXISTS notifications (
		id TEXT PRIMARY KEY,
		household_id TEXT NOT NULL DEFAULT 'default_household',
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		type TEXT NOT NULL,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		resource_id TEXT,
		action_required TEXT,
		created_at TIMESTAMP NOT NULL,
		read_at TIMESTAMP,
		expires_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, read_at, created_at DESC);

	CREATE TABLE IF NOT EXISTS notification_deliveries (
		id TEXT PRIMARY KEY,
		notification_id TEXT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
		channel TEXT NOT NULL,
		endpoint_id TEXT NOT NULL,
		status TEXT NOT NULL,
		attempt_count INTEGER NOT NULL DEFAULT 0,
		last_error TEXT,
		next_attempt_at TIMESTAMP,
		sent_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS push_subscriptions (
		id TEXT PRIMARY KEY,
		household_id TEXT NOT NULL DEFAULT 'default_household',
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		endpoint TEXT NOT NULL UNIQUE,
		p256dh TEXT NOT NULL,
		auth TEXT NOT NULL,
		user_agent TEXT,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_user_id TEXT NOT NULL,
		action TEXT NOT NULL,
		target_resource TEXT NOT NULL,
		details_json TEXT,
		prev_hash TEXT NOT NULL,
		hash TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS notification_endpoints (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		platform TEXT NOT NULL,
		endpoint_token TEXT NOT NULL UNIQUE,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS notification_preferences (
		user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		notify_approvals BOOLEAN NOT NULL DEFAULT 1,
		notify_security BOOLEAN NOT NULL DEFAULT 1,
		updated_at TIMESTAMP NOT NULL
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
	CREATE INDEX IF NOT EXISTS idx_invitations_code_hash ON account_invitations(code_hash);
	CREATE INDEX IF NOT EXISTS idx_profiles_household_id ON profiles(household_id);
	CREATE INDEX IF NOT EXISTS idx_access_policies_account_id ON access_policies(account_id);
	CREATE INDEX IF NOT EXISTS idx_approval_requests_household_status ON approval_requests(household_id, status);
	`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}

	s.addProfileColumns(ctx)

	return s.migrateHouseholdV1(ctx)
}

func (s *SQLiteStore) addProfileColumns(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE profiles ADD COLUMN date_of_birth TIMESTAMP;`)
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE profiles ADD COLUMN max_parental_rating INTEGER DEFAULT 18;`)
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE profiles ADD COLUMN unknown_rating_policy TEXT DEFAULT 'request_approval';`)
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE profiles ADD COLUMN storage_quota_bytes INTEGER DEFAULT 0;`)
}

func (s *SQLiteStore) migrateHouseholdV1(ctx context.Context) error {
	const defaultHouseholdID = "default_household"
	const defaultHouseholdName = "Haupt-Haushalt"

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO households (id, name, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO NOTHING;
	`, defaultHouseholdID, defaultHouseholdName, now)
	if err != nil {
		return fmt.Errorf("failed to create default household: %w", err)
	}

	var initialAdminID string
	_ = s.db.QueryRowContext(ctx, `SELECT initial_admin_id FROM bootstrap_meta WHERE id = 1 AND initial_admin_id IS NOT NULL`).Scan(&initialAdminID)

	rows, err := s.db.QueryContext(ctx, `SELECT id, role FROM users`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var uid, urole string
			if err := rows.Scan(&uid, &urole); err == nil {
				targetRole := urole
				if uid == initialAdminID || urole == "admin" {
					targetRole = "admin"
				} else if targetRole != "guest" {
					targetRole = "member"
				}

				_, _ = s.db.ExecContext(ctx, `
					INSERT INTO household_memberships (household_id, user_id, role, created_at)
					VALUES (?, ?, ?, ?)
					ON CONFLICT(household_id, user_id) DO NOTHING;
				`, defaultHouseholdID, uid, targetRole, now)
			}
		}
	}

	return nil
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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = stmt.Close() }()

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
	defer func() { _ = stmt.Close() }()

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
	defer func() { _ = rows.Close() }()

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
	// #nosec G101 -- SQL identifier describes credentials; it contains no credential value.
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

	// 7. Insert Default Household Membership for Initial Admin
	insertMemQuery := `
	INSERT INTO household_memberships (household_id, user_id, role, created_at)
	VALUES ('default_household', ?, 'admin', ?)
	ON CONFLICT(household_id, user_id) DO NOTHING
	`
	if _, err := tx.ExecContext(ctx, insertMemQuery, normUser.ID, now); err != nil {
		return fmt.Errorf("failed to insert initial admin household membership: %w", err)
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
	defer func() { _ = rows.Close() }()

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

	// #nosec G101 -- SQL identifier describes token storage; it contains no token value.
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

// ----------------- HouseholdStore -----------------

func (s *SQLiteStore) GetHousehold(ctx context.Context, id string) (*identity.Household, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM households WHERE id = ?`, id)
	var h identity.Household
	if err := row.Scan(&h.ID, &h.Name, &h.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrHouseholdNotFound
		}
		return nil, err
	}
	return &h, nil
}

func (s *SQLiteStore) GetHouseholdMembership(ctx context.Context, householdID, userID string) (*identity.HouseholdMembership, error) {
	row := s.db.QueryRowContext(ctx, `SELECT household_id, user_id, role, created_at FROM household_memberships WHERE household_id = ? AND user_id = ?`, householdID, userID)
	var m identity.HouseholdMembership
	var rStr string
	if err := row.Scan(&m.HouseholdID, &m.UserID, &rStr, &m.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrStoreNotFound
		}
		return nil, err
	}
	m.Role = identity.Role(rStr)
	return &m, nil
}

func (s *SQLiteStore) ListHouseholdMemberships(ctx context.Context, householdID string) ([]identity.HouseholdMembership, error) {
	if householdID == "" {
		householdID = "default_household"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT household_id, user_id, role, created_at FROM household_memberships WHERE household_id = ?`, householdID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []identity.HouseholdMembership
	for rows.Next() {
		var m identity.HouseholdMembership
		var rStr string
		if err := rows.Scan(&m.HouseholdID, &m.UserID, &rStr, &m.CreatedAt); err == nil {
			m.Role = identity.Role(rStr)
			list = append(list, m)
		}
	}
	return list, nil
}

func (s *SQLiteStore) PutHouseholdMembership(ctx context.Context, membership *identity.HouseholdMembership) error {
	query := `
	INSERT INTO household_memberships (household_id, user_id, role, created_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(household_id, user_id) DO UPDATE SET
		role = excluded.role;
	`
	_, err := s.db.ExecContext(ctx, query, membership.HouseholdID, membership.UserID, string(membership.Role), membership.CreatedAt.UTC())
	return err
}

func (s *SQLiteStore) PutAccountPassword(ctx context.Context, userID, passwordHash string, now time.Time) error {
	query := `
	INSERT INTO account_passwords (user_id, password_hash, created_at, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(user_id) DO UPDATE SET
		password_hash = excluded.password_hash,
		updated_at = excluded.updated_at;
	`
	_, err := s.db.ExecContext(ctx, query, userID, passwordHash, now.UTC(), now.UTC())
	return err
}

func (s *SQLiteStore) GetAccountPasswordHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM account_passwords WHERE user_id = ?`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", identity.ErrStoreNotFound
		}
		return "", err
	}
	return hash, nil
}

func (s *SQLiteStore) CreateInvitation(ctx context.Context, invite *identity.AccountInvitation) error {
	query := `
	INSERT INTO account_invitations (id, household_id, code_hash, role, display_name, created_by_user_id, expires_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, invite.ID, invite.HouseholdID, invite.CodeHash, string(invite.Role), invite.DisplayName, invite.CreatedByUserID, invite.ExpiresAt.UTC())
	return err
}

func (s *SQLiteStore) GetInvitationByCodeHash(ctx context.Context, codeHash string) (*identity.AccountInvitation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, household_id, code_hash, role, display_name, created_by_user_id, expires_at, used_at FROM account_invitations WHERE code_hash = ?`, codeHash)
	var inv identity.AccountInvitation
	var rStr string
	var uAt sql.NullTime
	if err := row.Scan(&inv.ID, &inv.HouseholdID, &inv.CodeHash, &rStr, &inv.DisplayName, &inv.CreatedByUserID, &inv.ExpiresAt, &uAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrInvitationNotFound
		}
		return nil, err
	}
	inv.Role = identity.Role(rStr)
	if uAt.Valid {
		t := uAt.Time
		inv.UsedAt = &t
	}
	return &inv, nil
}

func (s *SQLiteStore) RedeemInvitationAtomic(ctx context.Context, inviteID, codeHash string, user *identity.User, passwordHash string, passkey *identity.PasskeyCredential, now time.Time) (*identity.HouseholdMembership, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var hID, roleStr string
	var expAt time.Time
	var usedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT household_id, role, expires_at, used_at
		FROM account_invitations
		WHERE id = ? AND code_hash = ?
	`, inviteID, codeHash).Scan(&hID, &roleStr, &expAt, &usedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrInvitationNotFound
		}
		return nil, err
	}

	if usedAt.Valid {
		return nil, identity.ErrInvitationAlreadyUsed
	}
	if now.After(expAt) {
		return nil, identity.ErrInvitationNotFound
	}

	normUser, err := user.Normalize()
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, role, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, normUser.ID, normUser.Username, normUser.DisplayName, string(normUser.Role), normUser.CreatedAt.UTC(), normUser.UpdatedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to insert user during invite redeem: %w", err)
	}

	role := identity.Role(roleStr)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role, created_at)
		VALUES (?, ?, ?, ?)
	`, hID, normUser.ID, string(role), now.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to insert household membership: %w", err)
	}

	if passwordHash != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO account_passwords (user_id, password_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?)
		`, normUser.ID, passwordHash, now.UTC(), now.UTC())
		if err != nil {
			return nil, fmt.Errorf("failed to insert account password: %w", err)
		}
	} else if passkey != nil {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO passkey_credentials (id, user_id, public_key, attestation_type, aaguid, sign_count, backup_eligible, backup_state, transports, nickname, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, passkey.ID, normUser.ID, passkey.PublicKey, passkey.AttestationType, passkey.AAGUID, passkey.SignCount, passkey.BackupEligible, passkey.BackupState, strings.Join(passkey.Transports, ","), passkey.Nickname, now.UTC())
		if err != nil {
			return nil, fmt.Errorf("failed to insert passkey credential: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE account_invitations
		SET used_at = ?
		WHERE id = ? AND used_at IS NULL
	`, now.UTC(), inviteID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, identity.ErrInvitationAlreadyUsed
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &identity.HouseholdMembership{
		HouseholdID: hID,
		UserID:      normUser.ID,
		Role:        role,
		CreatedAt:   now,
	}, nil
}

func (s *SQLiteStore) PutProfile(ctx context.Context, profile *identity.Profile, policy *identity.ProfilePolicy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	dobVal := sql.NullTime{}
	if profile.DateOfBirth != nil {
		dobVal = sql.NullTime{Time: profile.DateOfBirth.UTC(), Valid: true}
	}
	maxRating := profile.MaxParentalRating
	if maxRating == 0 {
		maxRating = 18
	}
	unknownPol := profile.UnknownRatingPolicy
	if unknownPol == "" {
		unknownPol = "request_approval"
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO profiles (id, household_id, name, avatar_url, is_child, date_of_birth, max_parental_rating, unknown_rating_policy, storage_quota_bytes, created_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			avatar_url = excluded.avatar_url,
			is_child = excluded.is_child,
			date_of_birth = excluded.date_of_birth,
			max_parental_rating = excluded.max_parental_rating,
			unknown_rating_policy = excluded.unknown_rating_policy,
			storage_quota_bytes = excluded.storage_quota_bytes;
	`, profile.ID, profile.HouseholdID, profile.Name, profile.AvatarURL, profile.IsChild, dobVal, maxRating, unknownPol, profile.StorageQuotaBytes, profile.CreatedByUserID, profile.CreatedAt.UTC())
	if err != nil {
		return err
	}

	if policy != nil {
		var abJSON, bcJSON, twJSON sql.NullString
		if len(policy.AllowedBouquets) > 0 {
			b, _ := json.Marshal(policy.AllowedBouquets)
			abJSON = sql.NullString{String: string(b), Valid: true}
		}
		if len(policy.BlockedChannels) > 0 {
			b, _ := json.Marshal(policy.BlockedChannels)
			bcJSON = sql.NullString{String: string(b), Valid: true}
		}
		if policy.ViewingTimeWindow != nil {
			b, _ := json.Marshal(policy.ViewingTimeWindow)
			twJSON = sql.NullString{String: string(b), Valid: true}
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO profile_policies (profile_id, allowed_bouquets, blocked_channels, maturity_level, exit_pin_hash, dvr_allowed, viewing_time_window)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(profile_id) DO UPDATE SET
				allowed_bouquets = excluded.allowed_bouquets,
				blocked_channels = excluded.blocked_channels,
				maturity_level = excluded.maturity_level,
				exit_pin_hash = excluded.exit_pin_hash,
				dvr_allowed = excluded.dvr_allowed,
				viewing_time_window = excluded.viewing_time_window;
		`, policy.ProfileID, abJSON, bcJSON, policy.MaturityLevel, policy.ExitPINHash, policy.DVRAllowed, twJSON)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetProfile(ctx context.Context, profileID string) (*identity.Profile, *identity.ProfilePolicy, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, household_id, name, avatar_url, is_child, date_of_birth, max_parental_rating, unknown_rating_policy, storage_quota_bytes, created_by_user_id, created_at FROM profiles WHERE id = ?`, profileID)
	var p identity.Profile
	var av sql.NullString
	var dob sql.NullTime
	if err := row.Scan(&p.ID, &p.HouseholdID, &p.Name, &av, &p.IsChild, &dob, &p.MaxParentalRating, &p.UnknownRatingPolicy, &p.StorageQuotaBytes, &p.CreatedByUserID, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, identity.ErrProfileNotFound
		}
		return nil, nil, err
	}
	if av.Valid {
		p.AvatarURL = av.String
	}
	if dob.Valid {
		t := dob.Time.UTC()
		p.DateOfBirth = &t
	}

	pRow := s.db.QueryRowContext(ctx, `SELECT profile_id, allowed_bouquets, blocked_channels, maturity_level, exit_pin_hash, dvr_allowed, viewing_time_window FROM profile_policies WHERE profile_id = ?`, profileID)
	var pol identity.ProfilePolicy
	var abStr, bcStr, exitPin, twStr sql.NullString
	if err := pRow.Scan(&pol.ProfileID, &abStr, &bcStr, &pol.MaturityLevel, &exitPin, &pol.DVRAllowed, &twStr); err == nil {
		if abStr.Valid && abStr.String != "" {
			_ = json.Unmarshal([]byte(abStr.String), &pol.AllowedBouquets)
		}
		if bcStr.Valid && bcStr.String != "" {
			_ = json.Unmarshal([]byte(bcStr.String), &pol.BlockedChannels)
		}
		if exitPin.Valid {
			pol.ExitPINHash = exitPin.String
		}
		if twStr.Valid && twStr.String != "" {
			var tw identity.ViewingTimeWindow
			if err := json.Unmarshal([]byte(twStr.String), &tw); err == nil {
				pol.ViewingTimeWindow = &tw
			}
		}
		return &p, &pol, nil
	}

	return &p, nil, nil
}

func (s *SQLiteStore) ListProfilesByHousehold(ctx context.Context, householdID string) ([]identity.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, household_id, name, avatar_url, is_child, date_of_birth, max_parental_rating, unknown_rating_policy, storage_quota_bytes, created_by_user_id, created_at FROM profiles WHERE household_id = ? ORDER BY created_at ASC`, householdID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var profiles []identity.Profile
	for rows.Next() {
		var p identity.Profile
		var av sql.NullString
		var dob sql.NullTime
		if err := rows.Scan(&p.ID, &p.HouseholdID, &p.Name, &av, &p.IsChild, &dob, &p.MaxParentalRating, &p.UnknownRatingPolicy, &p.StorageQuotaBytes, &p.CreatedByUserID, &p.CreatedAt); err == nil {
			if av.Valid {
				p.AvatarURL = av.String
			}
			if dob.Valid {
				t := dob.Time.UTC()
				p.DateOfBirth = &t
			}
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

func (s *SQLiteStore) DeleteProfile(ctx context.Context, profileID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, profileID)
	return err
}

// ----------------- Access Policies -----------------

func (s *SQLiteStore) PutAccessPolicy(ctx context.Context, policy *identity.AccessPolicy) error {
	var vfVal, vuVal, revVal sql.NullTime
	if policy.ValidFrom != nil {
		vfVal = sql.NullTime{Time: policy.ValidFrom.UTC(), Valid: true}
	}
	if policy.ValidUntil != nil {
		vuVal = sql.NullTime{Time: policy.ValidUntil.UTC(), Valid: true}
	}
	if policy.RevokedAt != nil {
		revVal = sql.NullTime{Time: policy.RevokedAt.UTC(), Valid: true}
	}
	tz := policy.Timezone
	if tz == "" {
		tz = "Europe/Vienna"
	}
	days := policy.AllowedDaysMask
	if days <= 0 {
		days = 127
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_policies (id, account_id, valid_from, valid_until, daily_start, daily_end, timezone, allowed_days_mask, live_tv_allowed, epg_allowed, dvr_allowed, recordings_allowed, max_devices, revoked_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			valid_from = excluded.valid_from,
			valid_until = excluded.valid_until,
			daily_start = excluded.daily_start,
			daily_end = excluded.daily_end,
			timezone = excluded.timezone,
			allowed_days_mask = excluded.allowed_days_mask,
			live_tv_allowed = excluded.live_tv_allowed,
			epg_allowed = excluded.epg_allowed,
			dvr_allowed = excluded.dvr_allowed,
			recordings_allowed = excluded.recordings_allowed,
			max_devices = excluded.max_devices,
			revoked_at = excluded.revoked_at;
	`, policy.ID, policy.AccountID, vfVal, vuVal, policy.DailyStart, policy.DailyEnd, tz, days, policy.LiveTVAllowed, policy.EPGAllowed, policy.DVRAllowed, policy.RecordingsAllowed, policy.MaxDevices, revVal, policy.CreatedAt.UTC())
	return err
}

func (s *SQLiteStore) GetAccessPolicy(ctx context.Context, accountID string) (*identity.AccessPolicy, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, valid_from, valid_until, daily_start, daily_end, timezone, allowed_days_mask, live_tv_allowed, epg_allowed, dvr_allowed, recordings_allowed, max_devices, revoked_at, created_at
		FROM access_policies WHERE account_id = ? AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1
	`, accountID)
	var pol identity.AccessPolicy
	var vf, vu, rev sql.NullTime
	var ds, de sql.NullString
	if err := row.Scan(&pol.ID, &pol.AccountID, &vf, &vu, &ds, &de, &pol.Timezone, &pol.AllowedDaysMask, &pol.LiveTVAllowed, &pol.EPGAllowed, &pol.DVRAllowed, &pol.RecordingsAllowed, &pol.MaxDevices, &rev, &pol.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if vf.Valid {
		t := vf.Time.UTC()
		pol.ValidFrom = &t
	}
	if vu.Valid {
		t := vu.Time.UTC()
		pol.ValidUntil = &t
	}
	if rev.Valid {
		t := rev.Time.UTC()
		pol.RevokedAt = &t
	}
	if ds.Valid {
		pol.DailyStart = ds.String
	}
	if de.Valid {
		pol.DailyEnd = de.String
	}
	return &pol, nil
}

func (s *SQLiteStore) RevokeAccessPolicy(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE access_policies SET revoked_at = ? WHERE id = ?`, now.UTC(), id)
	return err
}

// ----------------- Approval Requests -----------------

func (s *SQLiteStore) CreateApprovalRequest(ctx context.Context, req *identity.ApprovalRequest) error {
	var appUser sql.NullString
	var appAt sql.NullTime
	if req.ApprovedByUserID != "" {
		appUser = sql.NullString{String: req.ApprovedByUserID, Valid: true}
	}
	if req.ApprovedAt != nil {
		appAt = sql.NullTime{Time: req.ApprovedAt.UTC(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO approval_requests (id, household_id, profile_id, request_type, resource_id, resource_name, parental_rating, scope, status, created_at, expires_at, approved_by_user_id, approved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, req.ID, req.HouseholdID, req.ProfileID, req.RequestType, req.ResourceID, req.ResourceName, req.ParentalRating, req.Scope, req.Status, req.CreatedAt.UTC(), req.ExpiresAt.UTC(), appUser, appAt)
	return err
}

func (s *SQLiteStore) GetApprovalRequest(ctx context.Context, id string) (*identity.ApprovalRequest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, household_id, profile_id, request_type, resource_id, resource_name, parental_rating, scope, status, created_at, expires_at, approved_by_user_id, approved_at
		FROM approval_requests WHERE id = ?
	`, id)
	var req identity.ApprovalRequest
	var appUser sql.NullString
	var appAt sql.NullTime
	if err := row.Scan(&req.ID, &req.HouseholdID, &req.ProfileID, &req.RequestType, &req.ResourceID, &req.ResourceName, &req.ParentalRating, &req.Scope, &req.Status, &req.CreatedAt, &req.ExpiresAt, &appUser, &appAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrApprovalNotFound
		}
		return nil, err
	}
	if appUser.Valid {
		req.ApprovedByUserID = appUser.String
	}
	if appAt.Valid {
		t := appAt.Time.UTC()
		req.ApprovedAt = &t
	}
	return &req, nil
}

func (s *SQLiteStore) ListApprovalRequests(ctx context.Context, householdID, status string) ([]identity.ApprovalRequest, error) {
	query := `SELECT id, household_id, profile_id, request_type, resource_id, resource_name, parental_rating, scope, status, created_at, expires_at, approved_by_user_id, approved_at FROM approval_requests WHERE household_id = ?`
	args := []any{householdID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var reqs []identity.ApprovalRequest
	for rows.Next() {
		var req identity.ApprovalRequest
		var appUser sql.NullString
		var appAt sql.NullTime
		if err := rows.Scan(&req.ID, &req.HouseholdID, &req.ProfileID, &req.RequestType, &req.ResourceID, &req.ResourceName, &req.ParentalRating, &req.Scope, &req.Status, &req.CreatedAt, &req.ExpiresAt, &appUser, &appAt); err == nil {
			if appUser.Valid {
				req.ApprovedByUserID = appUser.String
			}
			if appAt.Valid {
				t := appAt.Time.UTC()
				req.ApprovedAt = &t
			}
			reqs = append(reqs, req)
		}
	}
	return reqs, nil
}

func (s *SQLiteStore) SettleApprovalRequest(ctx context.Context, id, status, approvedByUserID string, approvedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = ?, approved_by_user_id = ?, approved_at = ?
		WHERE id = ? AND status = 'pending';
	`, status, approvedByUserID, approvedAt.UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return identity.ErrApprovalAlreadySettled
	}
	return nil
}

// ----------------- Household Resource Policy -----------------

func (s *SQLiteStore) PutHouseholdResourcePolicy(ctx context.Context, policy *identity.HouseholdResourcePolicy) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO household_resource_policies (household_id, max_concurrent_live_services, max_concurrent_viewers, max_parallel_recordings, max_parallel_transcodes, preemption_enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(household_id) DO UPDATE SET
			max_concurrent_live_services = excluded.max_concurrent_live_services,
			max_concurrent_viewers = excluded.max_concurrent_viewers,
			max_parallel_recordings = excluded.max_parallel_recordings,
			max_parallel_transcodes = excluded.max_parallel_transcodes,
			preemption_enabled = excluded.preemption_enabled,
			updated_at = excluded.updated_at;
	`, policy.HouseholdID, policy.MaxConcurrentLiveServices, policy.MaxConcurrentViewers, policy.MaxParallelRecordings, policy.MaxParallelTranscodes, policy.PreemptionEnabled, policy.UpdatedAt.UTC())
	return err
}

func (s *SQLiteStore) GetHouseholdResourcePolicy(ctx context.Context, householdID string) (*identity.HouseholdResourcePolicy, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT household_id, max_concurrent_live_services, max_concurrent_viewers, max_parallel_recordings, max_parallel_transcodes, preemption_enabled, updated_at
		FROM household_resource_policies WHERE household_id = ?
	`, householdID)
	var pol identity.HouseholdResourcePolicy
	if err := row.Scan(&pol.HouseholdID, &pol.MaxConcurrentLiveServices, &pol.MaxConcurrentViewers, &pol.MaxParallelRecordings, &pol.MaxParallelTranscodes, &pol.PreemptionEnabled, &pol.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &identity.HouseholdResourcePolicy{
				HouseholdID:               householdID,
				MaxConcurrentLiveServices: 3,
				MaxConcurrentViewers:      5,
				MaxParallelRecordings:     4,
				MaxParallelTranscodes:     3,
				PreemptionEnabled:         false,
				UpdatedAt:                 time.Now().UTC(),
			}, nil
		}
		return nil, err
	}
	return &pol, nil
}

// ----------------- Recording Profile Access -----------------

func (s *SQLiteStore) PutRecordingProfileAccess(ctx context.Context, recordingID string, profileIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `DELETE FROM recording_profile_access WHERE recording_id = ?`, recordingID)
	if err != nil {
		return err
	}

	for _, pid := range profileIDs {
		if pid == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recording_profile_access (recording_id, profile_id) VALUES (?, ?)`, recordingID, pid)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ----------------- Atomic Approval & Notification Creation -----------------

func (s *SQLiteStore) CreateApprovalRequestWithNotifications(ctx context.Context, req *identity.ApprovalRequest, notifs []*identity.Notification, deliveries []*identity.NotificationDelivery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var appUser sql.NullString
	var appAt sql.NullTime
	if req.ApprovedByUserID != "" {
		appUser = sql.NullString{String: req.ApprovedByUserID, Valid: true}
	}
	if req.ApprovedAt != nil {
		appAt = sql.NullTime{Time: req.ApprovedAt.UTC(), Valid: true}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_requests (id, household_id, profile_id, request_type, resource_id, resource_name, parental_rating, scope, status, created_at, expires_at, approved_by_user_id, approved_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, req.ID, req.HouseholdID, req.ProfileID, req.RequestType, req.ResourceID, req.ResourceName, req.ParentalRating, req.Scope, req.Status, req.CreatedAt.UTC(), req.ExpiresAt.UTC(), appUser, appAt)
	if err != nil {
		return fmt.Errorf("insert approval_request: %w", err)
	}

	for _, n := range notifs {
		var nExp sql.NullTime
		if n.ExpiresAt != nil {
			nExp = sql.NullTime{Time: n.ExpiresAt.UTC(), Valid: true}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notifications (id, household_id, user_id, type, title, body, resource_id, action_required, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, n.ID, n.HouseholdID, n.UserID, n.Type, n.Title, n.Body, n.ResourceID, n.ActionRequired, n.CreatedAt.UTC(), nExp)
		if err != nil {
			return fmt.Errorf("insert notification: %w", err)
		}
	}

	for _, d := range deliveries {
		var sAt sql.NullTime
		if d.SentAt != nil {
			sAt = sql.NullTime{Time: d.SentAt.UTC(), Valid: true}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notification_deliveries (id, notification_id, channel, endpoint_id, status, attempt_count, last_error, sent_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, d.ID, d.NotificationID, d.Channel, d.EndpointID, d.Status, d.AttemptCount, d.LastError, sAt)
		if err != nil {
			return fmt.Errorf("insert delivery: %w", err)
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) GetRecordingProfileAccess(ctx context.Context, recordingID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT profile_id FROM recording_profile_access WHERE recording_id = ?`, recordingID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var pids []string
	for rows.Next() {
		var pid string
		if err := rows.Scan(&pid); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// ----------------- Session Revocation -----------------

func (s *SQLiteStore) RevokeAllUserSessions(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE web_sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now.UTC(), userID)
	return err
}

// ----------------- Audit Logs -----------------

func (s *SQLiteStore) AppendAuditEntry(ctx context.Context, actorUserID, action, targetResource, detailsJSON string) (*identity.AuditLogEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var prevHash string
	_ = tx.QueryRowContext(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	if prevHash == "" {
		prevHash = "GENESIS_HASH_00000000000000000000000000000000000000000000000000000000"
	}

	now := time.Now().UTC()
	rawPayload := fmt.Sprintf("%s|%s|%s|%s|%s|%s", prevHash, actorUserID, action, targetResource, detailsJSON, now.Format(time.RFC3339))
	hashBytes := sha256.Sum256([]byte(rawPayload))
	hashStr := hex.EncodeToString(hashBytes[:])

	res, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (actor_user_id, action, target_resource, details_json, prev_hash, hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, actorUserID, action, targetResource, detailsJSON, prevHash, hashStr, now)
	if err != nil {
		return nil, err
	}

	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &identity.AuditLogEntry{
		ID:             id,
		ActorUserID:    actorUserID,
		Action:         action,
		TargetResource: targetResource,
		DetailsJSON:    detailsJSON,
		PrevHash:       prevHash,
		Hash:           hashStr,
		CreatedAt:      now,
	}, nil
}

func (s *SQLiteStore) ListAuditLogs(ctx context.Context, limit int) ([]identity.AuditLogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_user_id, action, target_resource, details_json, prev_hash, hash, created_at
		FROM audit_logs ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []identity.AuditLogEntry
	for rows.Next() {
		var e identity.AuditLogEntry
		var dj sql.NullString
		if err := rows.Scan(&e.ID, &e.ActorUserID, &e.Action, &e.TargetResource, &dj, &e.PrevHash, &e.Hash, &e.CreatedAt); err == nil {
			if dj.Valid {
				e.DetailsJSON = dj.String
			}
			logs = append(logs, e)
		}
	}
	return logs, nil
}

func (s *SQLiteStore) VerifyAuditChain(ctx context.Context) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_user_id, action, target_resource, details_json, prev_hash, hash, created_at
		FROM audit_logs ORDER BY id ASC
	`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	expectedPrev := "GENESIS_HASH_00000000000000000000000000000000000000000000000000000000"
	for rows.Next() {
		var e identity.AuditLogEntry
		var dj sql.NullString
		if err := rows.Scan(&e.ID, &e.ActorUserID, &e.Action, &e.TargetResource, &dj, &e.PrevHash, &e.Hash, &e.CreatedAt); err != nil {
			return false, err
		}
		if dj.Valid {
			e.DetailsJSON = dj.String
		}

		if e.PrevHash != expectedPrev {
			return false, fmt.Errorf("audit chain broken at id %d: expected prev %s, got %s", e.ID, expectedPrev, e.PrevHash)
		}

		rawPayload := fmt.Sprintf("%s|%s|%s|%s|%s|%s", e.PrevHash, e.ActorUserID, e.Action, e.TargetResource, e.DetailsJSON, e.CreatedAt.Format(time.RFC3339))
		hashBytes := sha256.Sum256([]byte(rawPayload))
		computedHash := hex.EncodeToString(hashBytes[:])

		if computedHash != e.Hash {
			return false, fmt.Errorf("audit entry %d tampered: expected hash %s, computed %s", e.ID, e.Hash, computedHash)
		}

		expectedPrev = e.Hash
	}
	return true, nil
}

// ----------------- Notifications & Push Subscriptions -----------------

func (s *SQLiteStore) CreateNotification(ctx context.Context, n *identity.Notification) error {
	var readAt, expAt sql.NullTime
	if n.ReadAt != nil {
		readAt = sql.NullTime{Time: n.ReadAt.UTC(), Valid: true}
	}
	if n.ExpiresAt != nil {
		expAt = sql.NullTime{Time: n.ExpiresAt.UTC(), Valid: true}
	}
	hID := n.HouseholdID
	if hID == "" {
		hID = "default_household"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notifications (id, household_id, user_id, type, title, body, resource_id, action_required, created_at, read_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, n.ID, hID, n.UserID, n.Type, n.Title, n.Body, n.ResourceID, n.ActionRequired, n.CreatedAt.UTC(), readAt, expAt)
	return err
}

func (s *SQLiteStore) ListNotifications(ctx context.Context, householdID, userID string, unreadOnly bool, limit int) ([]identity.Notification, error) {
	if householdID == "" {
		householdID = "default_household"
	}
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT id, household_id, user_id, type, title, body, resource_id, action_required, created_at, read_at, expires_at FROM notifications WHERE household_id = ? AND user_id = ?`
	args := []any{householdID, userID}
	if unreadOnly {
		query += ` AND read_at IS NULL`
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []identity.Notification
	for rows.Next() {
		var n identity.Notification
		var readAt, expAt sql.NullTime
		var resID, actReq sql.NullString
		if err := rows.Scan(&n.ID, &n.HouseholdID, &n.UserID, &n.Type, &n.Title, &n.Body, &resID, &actReq, &n.CreatedAt, &readAt, &expAt); err == nil {
			if resID.Valid {
				n.ResourceID = resID.String
			}
			if actReq.Valid {
				n.ActionRequired = actReq.String
			}
			if readAt.Valid {
				t := readAt.Time.UTC()
				n.ReadAt = &t
			}
			if expAt.Valid {
				t := expAt.Time.UTC()
				n.ExpiresAt = &t
			}
			list = append(list, n)
		}
	}
	return list, nil
}

func (s *SQLiteStore) GetNotification(ctx context.Context, id string) (*identity.Notification, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, household_id, user_id, type, title, body, resource_id, action_required, created_at, read_at, expires_at
		FROM notifications WHERE id = ?;
	`, id)
	var n identity.Notification
	var resID, actReq sql.NullString
	var readAt, expAt sql.NullTime
	if err := row.Scan(&n.ID, &n.HouseholdID, &n.UserID, &n.Type, &n.Title, &n.Body, &resID, &actReq, &n.CreatedAt, &readAt, &expAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, identity.ErrNotificationNotFound
		}
		return nil, err
	}
	if resID.Valid {
		n.ResourceID = resID.String
	}
	if actReq.Valid {
		n.ActionRequired = actReq.String
	}
	if readAt.Valid {
		t := readAt.Time.UTC()
		n.ReadAt = &t
	}
	if expAt.Valid {
		t := expAt.Time.UTC()
		n.ExpiresAt = &t
	}
	return &n, nil
}

func (s *SQLiteStore) MarkNotificationRead(ctx context.Context, id, userID string, readAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = ? WHERE id = ? AND user_id = ?;
	`, readAt.UTC(), id, userID)
	return err
}

func (s *SQLiteStore) MarkAllNotificationsRead(ctx context.Context, householdID, userID string, readAt time.Time) error {
	if householdID == "" {
		householdID = "default_household"
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE notifications SET read_at = ? WHERE household_id = ? AND user_id = ? AND read_at IS NULL;
	`, readAt.UTC(), householdID, userID)
	return err
}

func (s *SQLiteStore) DeleteNotification(ctx context.Context, id, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE id = ? AND user_id = ?;`, id, userID)
	return err
}

func (s *SQLiteStore) SavePushSubscription(ctx context.Context, sub *identity.PushSubscription) error {
	hID := sub.HouseholdID
	if hID == "" {
		hID = "default_household"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (id, household_id, user_id, endpoint, p256dh, auth, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			user_id = excluded.user_id,
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			user_agent = excluded.user_agent;
	`, sub.ID, hID, sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent, sub.CreatedAt.UTC())
	return err
}

func (s *SQLiteStore) ListPushSubscriptions(ctx context.Context, householdID, userID string) ([]identity.PushSubscription, error) {
	if householdID == "" {
		householdID = "default_household"
	}
	query := `SELECT id, household_id, user_id, endpoint, p256dh, auth, user_agent, created_at FROM push_subscriptions WHERE household_id = ?`
	args := []any{householdID}
	if userID != "" {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []identity.PushSubscription
	for rows.Next() {
		var sub identity.PushSubscription
		var ua sql.NullString
		if err := rows.Scan(&sub.ID, &sub.HouseholdID, &sub.UserID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &ua, &sub.CreatedAt); err == nil {
			if ua.Valid {
				sub.UserAgent = ua.String
			}
			list = append(list, sub)
		}
	}
	return list, nil
}

func (s *SQLiteStore) DeletePushSubscription(ctx context.Context, id, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?;`, id, userID)
	return err
}

func (s *SQLiteStore) DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?;`, endpoint)
	return err
}

func (s *SQLiteStore) RecordNotificationDelivery(ctx context.Context, delivery *identity.NotificationDelivery) error {
	var sentAt, nextAt sql.NullTime
	if delivery.SentAt != nil {
		sentAt = sql.NullTime{Time: delivery.SentAt.UTC(), Valid: true}
	}
	if delivery.NextAttemptAt != nil {
		nextAt = sql.NullTime{Time: delivery.NextAttemptAt.UTC(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (id, notification_id, channel, endpoint_id, status, attempt_count, last_error, next_attempt_at, sent_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, delivery.ID, delivery.NotificationID, delivery.Channel, delivery.EndpointID, delivery.Status, delivery.AttemptCount, delivery.LastError, nextAt, sentAt)
	return err
}

func (s *SQLiteStore) GetPendingNotificationDeliveries(ctx context.Context, limit int) ([]identity.NotificationDelivery, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, notification_id, channel, endpoint_id, status, attempt_count, last_error, next_attempt_at, sent_at
		FROM notification_deliveries
		WHERE status = 'queued' OR (status = 'failed' AND attempt_count < 5 AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
		LIMIT ?;
	`, time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []identity.NotificationDelivery
	for rows.Next() {
		var d identity.NotificationDelivery
		var le sql.NullString
		var nextAt, sentAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.Channel, &d.EndpointID, &d.Status, &d.AttemptCount, &le, &nextAt, &sentAt); err == nil {
			if le.Valid {
				d.LastError = le.String
			}
			if nextAt.Valid {
				t := nextAt.Time.UTC()
				d.NextAttemptAt = &t
			}
			if sentAt.Valid {
				t := sentAt.Time.UTC()
				d.SentAt = &t
			}
			list = append(list, d)
		}
	}
	return list, nil
}

func (s *SQLiteStore) UpdateNotificationDelivery(ctx context.Context, delivery *identity.NotificationDelivery) error {
	var sentAt, nextAt sql.NullTime
	if delivery.SentAt != nil {
		sentAt = sql.NullTime{Time: delivery.SentAt.UTC(), Valid: true}
	}
	if delivery.NextAttemptAt != nil {
		nextAt = sql.NullTime{Time: delivery.NextAttemptAt.UTC(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_deliveries
		SET status = ?, attempt_count = ?, last_error = ?, next_attempt_at = ?, sent_at = ?
		WHERE id = ?;
	`, delivery.Status, delivery.AttemptCount, delivery.LastError, nextAt, sentAt, delivery.ID)
	return err
}
