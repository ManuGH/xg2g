package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 4 // Incremented for migration_history
)

// SqliteStore implements StateStore using SQLite.
type SqliteStore struct {
	DB *sql.DB
}

// NewSqliteStore initializes a new SQLite session store.
func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	if err != nil {
		return nil, err
	}

	s := &SqliteStore{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("session store: migration failed: %w", err)
	}

	return s, nil
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	err := s.DB.QueryRow("PRAGMA user_version").Scan(&currentVersion)
	if err != nil {
		return err
	}

	if currentVersion >= schemaVersion {
		return nil
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Drop existing if version mismatch (it's shadow impl, so we can be destructive during dev)
	if currentVersion > 0 && currentVersion < 2 {
		_, _ = tx.Exec("DROP TABLE IF EXISTS sessions")
		_, _ = tx.Exec("DROP TABLE IF EXISTS idempotency")
		_, _ = tx.Exec("DROP TABLE IF EXISTS leases")
	}

	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		session_id TEXT PRIMARY KEY,
		service_ref TEXT NOT NULL,
		profile_json TEXT NOT NULL,
		state TEXT NOT NULL,
		pipeline_state TEXT NOT NULL,
		reason TEXT NOT NULL,
	reason_detail TEXT,
	reason_detail_code TEXT,
	reason_detail_debug TEXT,
		fallback_reason TEXT,
		fallback_at_ms INTEGER,
		correlation_id TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		updated_at_ms INTEGER NOT NULL,
		last_access_ms INTEGER,
		expires_at_ms INTEGER NOT NULL,
		lease_expires_at_ms INTEGER NOT NULL,
		heartbeat_interval INTEGER NOT NULL,
		last_heartbeat_ms INTEGER,
		stop_reason TEXT,
		latest_segment_at TEXT,
		last_playlist_access_at TEXT,
		playlist_published_at TEXT,
		context_data_json TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at_ms);
	CREATE INDEX IF NOT EXISTS idx_sessions_state_lease ON sessions(state, lease_expires_at_ms);

	CREATE TABLE IF NOT EXISTS idempotency (
		key TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		expires_at_ms INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency(expires_at_ms);

	CREATE TABLE IF NOT EXISTS leases (
		key TEXT PRIMARY KEY,
		owner TEXT NOT NULL,
		expires_at_ms INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS migration_history (
		module TEXT PRIMARY KEY,
		source_type TEXT NOT NULL,
		source_path TEXT NOT NULL,
		migrated_at_ms INTEGER NOT NULL,
		record_count INTEGER NOT NULL,
		checksum TEXT
	);
	`

	if _, err := tx.Exec(schema); err != nil {
		return err
	}

	if currentVersion < 4 {
		_, _ = tx.Exec("ALTER TABLE sessions ADD COLUMN reason_detail_code TEXT")
		_, _ = tx.Exec("ALTER TABLE sessions ADD COLUMN reason_detail_debug TEXT")
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}

	return tx.Commit()
}

// --- Session CRUD ---

func (s *SqliteStore) PutSession(ctx context.Context, rec *model.SessionRecord) error {
	profileJSON, _ := json.Marshal(rec.Profile)
	contextJSON, _ := json.Marshal(rec.ContextData)

	query := `
	INSERT INTO sessions (
		session_id, service_ref, profile_json, state, pipeline_state, reason, reason_detail, reason_detail_code, reason_detail_debug,
		fallback_reason, fallback_at_ms, correlation_id, created_at_ms, updated_at_ms,
		last_access_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval,
		last_heartbeat_ms, stop_reason, latest_segment_at, last_playlist_access_at,
		playlist_published_at, context_data_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		service_ref = excluded.service_ref,
		profile_json = excluded.profile_json,
		state = excluded.state,
		pipeline_state = excluded.pipeline_state,
		reason = excluded.reason,
		reason_detail = excluded.reason_detail,
		reason_detail_code = excluded.reason_detail_code,
		reason_detail_debug = excluded.reason_detail_debug,
		fallback_reason = excluded.fallback_reason,
		fallback_at_ms = excluded.fallback_at_ms,
		correlation_id = excluded.correlation_id,
		updated_at_ms = excluded.updated_at_ms,
		last_access_ms = excluded.last_access_ms,
		expires_at_ms = excluded.expires_at_ms,
		lease_expires_at_ms = excluded.lease_expires_at_ms,
		heartbeat_interval = excluded.heartbeat_interval,
		last_heartbeat_ms = excluded.last_heartbeat_ms,
		stop_reason = excluded.stop_reason,
		latest_segment_at = excluded.latest_segment_at,
		last_playlist_access_at = excluded.last_playlist_access_at,
		playlist_published_at = excluded.playlist_published_at,
		context_data_json = excluded.context_data_json
	`

	_, err := s.DB.ExecContext(ctx, query,
		rec.SessionID, rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, rec.ReasonDetailDebug, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.CreatedAtUnix), s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON,
	)
	return err
}

func (s *SqliteStore) PutSessionWithIdempotency(ctx context.Context, rec *model.SessionRecord, idemKey string, ttl time.Duration) (string, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Check idempotency
	var existingID string
	var expiresAt int64
	err = tx.QueryRowContext(ctx, "SELECT session_id, expires_at_ms FROM idempotency WHERE key = ?", idemKey).Scan(&existingID, &expiresAt)
	if err == nil {
		if expiresAt > time.Now().UnixMilli() {
			return existingID, true, nil
		}
		// Expired, delete it
		_, _ = tx.ExecContext(ctx, "DELETE FROM idempotency WHERE key = ?", idemKey)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}

	// 2. Put Session
	profileJSON, _ := json.Marshal(rec.Profile)
	contextJSON, _ := json.Marshal(rec.ContextData)
	query := `
	INSERT INTO sessions (
		session_id, service_ref, profile_json, state, pipeline_state, reason, reason_detail, reason_detail_code, reason_detail_debug,
		fallback_reason, fallback_at_ms, correlation_id, created_at_ms, updated_at_ms,
		last_access_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval,
		last_heartbeat_ms, stop_reason, latest_segment_at, last_playlist_access_at,
		playlist_published_at, context_data_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(session_id) DO UPDATE SET
		service_ref = excluded.service_ref,
		profile_json = excluded.profile_json,
		state = excluded.state,
		pipeline_state = excluded.pipeline_state,
		reason = excluded.reason,
		reason_detail = excluded.reason_detail,
		reason_detail_code = excluded.reason_detail_code,
		reason_detail_debug = excluded.reason_detail_debug,
		fallback_reason = excluded.fallback_reason,
		fallback_at_ms = excluded.fallback_at_ms,
		correlation_id = excluded.correlation_id,
		updated_at_ms = excluded.updated_at_ms,
		last_access_ms = excluded.last_access_ms,
		expires_at_ms = excluded.expires_at_ms,
		lease_expires_at_ms = excluded.lease_expires_at_ms,
		heartbeat_interval = excluded.heartbeat_interval,
		last_heartbeat_ms = excluded.last_heartbeat_ms,
		stop_reason = excluded.stop_reason,
		latest_segment_at = excluded.latest_segment_at,
		last_playlist_access_at = excluded.last_playlist_access_at,
		playlist_published_at = excluded.playlist_published_at,
		context_data_json = excluded.context_data_json
	`

	_, err = tx.ExecContext(ctx, query,
		rec.SessionID, rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, rec.ReasonDetailDebug, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.CreatedAtUnix), s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON,
	)
	if err != nil {
		return "", false, err
	}

	// 3. Put Idempotency
	idemExpires := time.Now().Add(ttl).UnixMilli()
	_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO idempotency (key, session_id, expires_at_ms) VALUES (?, ?, ?)", idemKey, rec.SessionID, idemExpires)
	if err != nil {
		return "", false, err
	}

	return rec.SessionID, false, tx.Commit()
}

func (s *SqliteStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	query := `SELECT * FROM sessions WHERE session_id = ?`
	row := s.DB.QueryRowContext(ctx, query, id)
	return scanSession(row)
}

func (s *SqliteStore) QuerySessions(ctx context.Context, filter SessionFilter) ([]*model.SessionRecord, error) {
	query := "SELECT * FROM sessions WHERE 1=1"
	args := []interface{}{}

	if len(filter.States) > 0 {
		query += " AND state IN ("
		for i, st := range filter.States {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, st)
		}
		query += ")"
	}

	if filter.LeaseExpiresBefore > 0 {
		query += " AND lease_expires_at_ms < ?"
		args = append(args, s2ms(filter.LeaseExpiresBefore))
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*model.SessionRecord
	for rows.Next() {
		rec, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, nil
}

func (s *SqliteStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rec, err := scanSession(tx.QueryRowContext(ctx, "SELECT * FROM sessions WHERE session_id = ?", id))
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errors.New("not found")
	}

	if err := fn(rec); err != nil {
		return nil, err
	}

	rec.UpdatedAtUnix = time.Now().Unix()

	profileJSON, _ := json.Marshal(rec.Profile)
	contextJSON, _ := json.Marshal(rec.ContextData)

	updateQuery := `
		UPDATE sessions SET
			service_ref = ?, profile_json = ?, state = ?, pipeline_state = ?, reason = ?,
			reason_detail = ?, reason_detail_code = ?, reason_detail_debug = ?, fallback_reason = ?, fallback_at_ms = ?, correlation_id = ?,
			updated_at_ms = ?, last_access_ms = ?, expires_at_ms = ?, lease_expires_at_ms = ?,
			heartbeat_interval = ?, last_heartbeat_ms = ?, stop_reason = ?, latest_segment_at = ?,
			last_playlist_access_at = ?, playlist_published_at = ?, context_data_json = ?
		WHERE session_id = ?
		`
	_, err = tx.ExecContext(ctx, updateQuery,
		rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, rec.ReasonDetailDebug, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON,
		rec.SessionID,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *SqliteStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return s.QuerySessions(ctx, SessionFilter{})
}

func (s *SqliteStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT * FROM sessions")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		rec, err := scanSession(rows)
		if err != nil {
			return err
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	return nil
}

func (s *SqliteStore) DeleteSession(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE session_id = ?", id)
	return err
}

func (s *SqliteStore) PutIdempotency(ctx context.Context, key, sessionID string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl).UnixMilli()
	_, err := s.DB.ExecContext(ctx, "INSERT OR REPLACE INTO idempotency (key, session_id, expires_at_ms) VALUES (?, ?, ?)", key, sessionID, expiresAt)
	return err
}

func (s *SqliteStore) GetIdempotency(ctx context.Context, key string) (string, bool, error) {
	var sessionID string
	var expiresAt int64
	err := s.DB.QueryRowContext(ctx, "SELECT session_id, expires_at_ms FROM idempotency WHERE key = ?", key).Scan(&sessionID, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if expiresAt < time.Now().UnixMilli() {
		return "", false, nil
	}
	return sessionID, true, nil
}

func (s *SqliteStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	expiresAt := time.Now().Add(ttl).UnixMilli()

	var currentOwner string
	var currentExpires int64
	err = tx.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", key).Scan(&currentOwner, &currentExpires)

	if err == nil {
		if currentExpires > now && currentOwner != owner {
			return &sqliteLease{key: key, owner: currentOwner, expires: time.UnixMilli(currentExpires)}, false, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}

	_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO leases (key, owner, expires_at_ms) VALUES (?, ?, ?)", key, owner, expiresAt)
	if err != nil {
		return nil, false, err
	}

	if err := tx.Commit(); err != nil {
		return nil, false, err
	}

	return &sqliteLease{key: key, owner: owner, expires: time.UnixMilli(expiresAt)}, true, nil
}

func (s *SqliteStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	return s.TryAcquireLease(ctx, key, owner, ttl)
}

func (s *SqliteStore) GetLease(ctx context.Context, key string) (Lease, bool, error) {
	var owner string
	var expiresAt int64
	err := s.DB.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", key).Scan(&owner, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &sqliteLease{key: key, owner: owner, expires: time.UnixMilli(expiresAt)}, true, nil
}

func (s *SqliteStore) ReleaseLease(ctx context.Context, key, owner string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM leases WHERE key = ? AND owner = ?", key, owner)
	return err
}

func (s *SqliteStore) DeleteAllLeases(ctx context.Context) (int, error) {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM leases")
	if err != nil {
		return 0, err
	}
	count, _ := res.RowsAffected()
	return int(count), nil
}

// --- Helpers ---

type sqliteLease struct {
	key     string
	owner   string
	expires time.Time
}

func (l *sqliteLease) Key() string          { return l.key }
func (l *sqliteLease) Owner() string        { return l.owner }
func (l *sqliteLease) ExpiresAt() time.Time { return l.expires }

func scanSession(scanner interface {
	Scan(dest ...interface{}) error
}) (*model.SessionRecord, error) {
	var rec model.SessionRecord
	var profileJSON, contextJSON []byte
	var fallbackAt, createdAt, updatedAt, lastAccess, expiresAt, leaseExpires, lastHB sql.NullInt64
	var latestSeg, lastAccessAt, published sql.NullString
	var reasonDetailLegacy, reasonDetailCode, reasonDetailDebug sql.NullString

	err := scanner.Scan(
		&rec.SessionID, &rec.ServiceRef, &profileJSON, &rec.State, &rec.PipelineState, &rec.Reason,
		&reasonDetailLegacy, &reasonDetailCode, &reasonDetailDebug,
		&rec.FallbackReason, &fallbackAt, &rec.CorrelationID, &createdAt, &updatedAt,
		&lastAccess, &expiresAt, &leaseExpires, &rec.HeartbeatInterval,
		&lastHB, &rec.StopReason, &latestSeg, &lastAccessAt, &published, &contextJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	_ = json.Unmarshal(profileJSON, &rec.Profile)
	_ = json.Unmarshal(contextJSON, &rec.ContextData)
	rec.FallbackAtUnix = ms2s(fallbackAt)
	rec.CreatedAtUnix = ms2s(createdAt)
	rec.UpdatedAtUnix = ms2s(updatedAt)
	rec.LastAccessUnix = ms2s(lastAccess)
	rec.ExpiresAtUnix = ms2s(expiresAt)
	rec.LeaseExpiresAtUnix = ms2s(leaseExpires)
	rec.LastHeartbeatUnix = ms2s(lastHB)
	rec.LatestSegmentAt, err = nullStringToTime(latestSeg)
	if err != nil {
		return nil, fmt.Errorf("parse latest_segment_at for session %s: %w", rec.SessionID, err)
	}
	rec.LastPlaylistAccessAt, err = nullStringToTime(lastAccessAt)
	if err != nil {
		return nil, fmt.Errorf("parse last_playlist_access_at for session %s: %w", rec.SessionID, err)
	}
	rec.PlaylistPublishedAt, err = nullStringToTime(published)
	if err != nil {
		return nil, fmt.Errorf("parse playlist_published_at for session %s: %w", rec.SessionID, err)
	}
	if reasonDetailCode.Valid {
		rec.ReasonDetailCode = model.ReasonDetailCode(reasonDetailCode.String)
	}
	if reasonDetailDebug.Valid {
		rec.ReasonDetailDebug = reasonDetailDebug.String
	} else if reasonDetailLegacy.Valid {
		rec.ReasonDetailDebug = reasonDetailLegacy.String
	}

	return &rec, nil
}

func s2ms(s int64) int64 { return s * 1000 }
func ms2s(ms sql.NullInt64) int64 {
	if !ms.Valid {
		return 0
	}
	return ms.Int64 / 1000
}

func timeToNullString(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: t.Format(time.RFC3339), Valid: true}
}

func nullStringToTime(ns sql.NullString) (time.Time, error) {
	if !ns.Valid {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, ns.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid RFC3339 timestamp %q: %w", ns.String, err)
	}
	return t, nil
}
