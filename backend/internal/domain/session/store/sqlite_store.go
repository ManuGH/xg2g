package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 5 // Incremented for playback trace persistence
	SchemaVersion = schemaVersion
)

const sessionListOrderBy = " ORDER BY updated_at_ms DESC, created_at_ms DESC, session_id ASC"

// sessionColumns lists the sessions columns in the exact order scanSession scans
// them. Reads MUST enumerate columns explicitly instead of using SELECT *:
// migrate() upgrades legacy databases with ALTER TABLE ADD COLUMN, which appends
// columns at the physical end of the table. SELECT * returns columns in physical
// order, so on an upgraded DB the appended columns (reason_detail_code,
// reason_detail_debug, playback_trace_json) would arrive out of scanSession's
// positional order and corrupt every read. An explicit list is order-stable.
const sessionColumns = "session_id, service_ref, profile_json, state, pipeline_state, reason, " +
	"reason_detail, reason_detail_code, reason_detail_debug, fallback_reason, fallback_at_ms, " +
	"correlation_id, created_at_ms, updated_at_ms, last_access_ms, expires_at_ms, " +
	"lease_expires_at_ms, heartbeat_interval, last_heartbeat_ms, stop_reason, " +
	"latest_segment_at, last_playlist_access_at, playlist_published_at, " +
	"context_data_json, playback_trace_json"

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
	return sqlite.RunMigration(s.DB, schemaVersion, func(tx *sql.Tx, currentVersion int) error {
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
		context_data_json TEXT,
		playback_trace_json TEXT
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

	CREATE TABLE IF NOT EXISTS input_claims (
		input_id TEXT PRIMARY KEY,
		active_plane TEXT NOT NULL,
		expires_at_ms INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS input_claim_owners (
		input_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		generation_token TEXT NOT NULL DEFAULT '',
		expires_at_ms INTEGER NOT NULL,
		PRIMARY KEY (input_id, session_id)
	);

	CREATE TABLE IF NOT EXISTS mux_allocations (
		multiplex_id TEXT PRIMARY KEY,
		input_id TEXT NOT NULL,
		demod_id TEXT NOT NULL,
		required_plane TEXT,
		scr_slot INTEGER,
		expires_at_ms INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS mux_members (
		multiplex_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		generation_token TEXT NOT NULL DEFAULT '',
		joined_at_ms INTEGER NOT NULL,
		expires_at_ms INTEGER NOT NULL,
		PRIMARY KEY (multiplex_id, session_id)
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

		// Keep these unguarded by currentVersion>0 to handle legacy databases with
		// user_version=0 but an existing sessions table missing v4/v5 columns.
		// CREATE TABLE IF NOT EXISTS above is a no-op when the table exists, so the
		// ALTER TABLE is needed to upgrade the legacy schema. On a fresh database
		// the ALTER TABLE is a harmless no-op (error is silently discarded with _,_).
		if currentVersion < 4 {
			_, _ = tx.Exec("ALTER TABLE sessions ADD COLUMN reason_detail_code TEXT")
			_, _ = tx.Exec("ALTER TABLE sessions ADD COLUMN reason_detail_debug TEXT")
		}
		if currentVersion < 5 {
			_, _ = tx.Exec("ALTER TABLE sessions ADD COLUMN playback_trace_json TEXT")
		}
		_, _ = tx.Exec("ALTER TABLE input_claim_owners ADD COLUMN generation_token TEXT NOT NULL DEFAULT ''")
		_, _ = tx.Exec("ALTER TABLE mux_members ADD COLUMN generation_token TEXT NOT NULL DEFAULT ''")
		return nil
	})
}

// --- Session CRUD ---

func (s *SqliteStore) PutSession(ctx context.Context, rec *model.SessionRecord) error {
	profileJSON, _ := json.Marshal(rec.Profile)
	contextJSON, _ := json.Marshal(rec.ContextData)
	playbackTraceJSON, _ := json.Marshal(rec.PlaybackTrace)

	query := `
	INSERT INTO sessions (
		session_id, service_ref, profile_json, state, pipeline_state, reason, reason_detail, reason_detail_code, reason_detail_debug,
		fallback_reason, fallback_at_ms, correlation_id, created_at_ms, updated_at_ms,
		last_access_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval,
		last_heartbeat_ms, stop_reason, latest_segment_at, last_playlist_access_at,
		playlist_published_at, context_data_json, playback_trace_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		context_data_json = excluded.context_data_json,
		playback_trace_json = excluded.playback_trace_json
	`

	_, err := s.DB.ExecContext(ctx, query,
		rec.SessionID, rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, sql.NullString{}, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.CreatedAtUnix), s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON, playbackTraceJSON,
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
	playbackTraceJSON, _ := json.Marshal(rec.PlaybackTrace)
	query := `
	INSERT INTO sessions (
		session_id, service_ref, profile_json, state, pipeline_state, reason, reason_detail, reason_detail_code, reason_detail_debug,
		fallback_reason, fallback_at_ms, correlation_id, created_at_ms, updated_at_ms,
		last_access_ms, expires_at_ms, lease_expires_at_ms, heartbeat_interval,
		last_heartbeat_ms, stop_reason, latest_segment_at, last_playlist_access_at,
		playlist_published_at, context_data_json, playback_trace_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		context_data_json = excluded.context_data_json,
		playback_trace_json = excluded.playback_trace_json
	`

	_, err = tx.ExecContext(ctx, query,
		rec.SessionID, rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, sql.NullString{}, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.CreatedAtUnix), s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON, playbackTraceJSON,
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
	query := "SELECT " + sessionColumns + " FROM sessions WHERE session_id = ?"
	row := s.DB.QueryRowContext(ctx, query, id)
	return scanSession(row)
}

func (s *SqliteStore) GetDiagnosticMetadata(ctx context.Context, id string) (ports.DiagnosticMetadata, bool) {
	rec, err := s.GetSession(ctx, id)
	if err != nil || rec == nil {
		return ports.DiagnosticMetadata{}, false
	}
	return ports.DiagnosticMetadata{
		GenerationID:          rec.GenerationID,
		CorrelationID:         rec.CorrelationID,
		Reason:                string(rec.Reason),
		StopRequestedAtUnixMs: rec.StopRequestedAtUnixMs,
	}, true
}

func (s *SqliteStore) QuerySessions(ctx context.Context, filter SessionFilter) ([]*model.SessionRecord, error) {
	query := "SELECT " + sessionColumns + " FROM sessions WHERE 1=1"
	args := []any{}

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
	query += sessionListOrderBy

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
	return results, rows.Err()
}

func (s *SqliteStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rec, err := scanSession(tx.QueryRowContext(ctx, "SELECT "+sessionColumns+" FROM sessions WHERE session_id = ?", id))
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
	playbackTraceJSON, _ := json.Marshal(rec.PlaybackTrace)

	updateQuery := `
		UPDATE sessions SET
			service_ref = ?, profile_json = ?, state = ?, pipeline_state = ?, reason = ?,
			reason_detail = ?, reason_detail_code = ?, reason_detail_debug = ?, fallback_reason = ?, fallback_at_ms = ?, correlation_id = ?,
			updated_at_ms = ?, last_access_ms = ?, expires_at_ms = ?, lease_expires_at_ms = ?,
			heartbeat_interval = ?, last_heartbeat_ms = ?, stop_reason = ?, latest_segment_at = ?,
			last_playlist_access_at = ?, playlist_published_at = ?, context_data_json = ?, playback_trace_json = ?
		WHERE session_id = ?
		`
	_, err = tx.ExecContext(ctx, updateQuery,
		rec.ServiceRef, profileJSON, rec.State, rec.PipelineState, rec.Reason, sql.NullString{}, rec.ReasonDetailCode, rec.ReasonDetailDebug,
		rec.FallbackReason, s2ms(rec.FallbackAtUnix), rec.CorrelationID, s2ms(rec.UpdatedAtUnix),
		s2ms(rec.LastAccessUnix), s2ms(rec.ExpiresAtUnix), s2ms(rec.LeaseExpiresAtUnix), rec.HeartbeatInterval,
		s2ms(rec.LastHeartbeatUnix), rec.StopReason, timeToNullString(rec.LatestSegmentAt),
		timeToNullString(rec.LastPlaylistAccessAt), timeToNullString(rec.PlaylistPublishedAt), contextJSON, playbackTraceJSON,
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
	rows, err := s.DB.QueryContext(ctx, "SELECT "+sessionColumns+" FROM sessions")
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
	return rows.Err()
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

func (s *SqliteStore) DeleteIdempotencyIfMatch(ctx context.Context, idemKey, sessionID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM idempotency WHERE key = ? AND session_id = ?", idemKey, sessionID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
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

// RenewLease extends a lease the caller still holds. Unlike TryAcquireLease it is
// fail-closed: if the lease is gone (released, swept, or never held) or has been
// taken over by another owner, it returns ok=false WITHOUT recreating it. The old
// implementation delegated to TryAcquireLease, whose INSERT OR REPLACE silently
// re-created a lost lease and always returned ok=true — defeating the heartbeat
// loop's lease-loss abort, so a session whose tuner lease was revoked kept running
// and re-grabbed the slot (zombie session / split-brain risk). Mirrors
// MemoryStore.RenewLease (existence + ownership check, then extend).
func (s *SqliteStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (Lease, bool, error) {
	if ttl <= 0 {
		return nil, false, errors.New("invalid ttl")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var currentOwner string
	var currentExpires int64
	err = tx.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", key).Scan(&currentOwner, &currentExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil // lease lost — do NOT recreate
	}
	if err != nil {
		return nil, false, err
	}
	if currentOwner != owner {
		// Held by someone else now — fail closed, surface the current holder.
		return &sqliteLease{key: key, owner: currentOwner, expires: time.UnixMilli(currentExpires)}, false, nil
	}

	newExpires := time.Now().Add(ttl).UnixMilli()
	res, err := tx.ExecContext(ctx, "UPDATE leases SET expires_at_ms = ? WHERE key = ? AND owner = ?", newExpires, key, owner)
	if err != nil {
		return nil, false, err
	}
	// Fail closed if the row changed owner (or vanished) between the SELECT and the
	// UPDATE: a 0-row update means we no longer hold the lease, so report loss
	// instead of a successful renewal.
	if affected, err := res.RowsAffected(); err != nil {
		return nil, false, err
	} else if affected == 0 {
		return nil, false, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &sqliteLease{key: key, owner: owner, expires: time.UnixMilli(newExpires)}, true, nil
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

func (s *SqliteStore) ListLeases(ctx context.Context) ([]Lease, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	nowMs := time.Now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx, "SELECT key, owner, expires_at_ms FROM leases WHERE expires_at_ms > ?", nowMs)
	if err != nil {
		return nil, fmt.Errorf("list leases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var leases []Lease
	for rows.Next() {
		var key, owner string
		var expiresAt int64
		if err := rows.Scan(&key, &owner, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan lease: %w", err)
		}
		leases = append(leases, &sqliteLease{key: key, owner: owner, expires: time.UnixMilli(expiresAt)})
	}
	return leases, rows.Err()
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
	Scan(dest ...any) error
}) (*model.SessionRecord, error) {
	var rec model.SessionRecord
	var profileJSON, contextJSON, playbackTraceJSON []byte
	var fallbackAt, createdAt, updatedAt, lastAccess, expiresAt, leaseExpires, lastHB sql.NullInt64
	var latestSeg, lastAccessAt, published sql.NullString
	var reasonDetailLegacy, reasonDetailCode, reasonDetailDebug sql.NullString

	err := scanner.Scan(
		&rec.SessionID, &rec.ServiceRef, &profileJSON, &rec.State, &rec.PipelineState, &rec.Reason,
		&reasonDetailLegacy, &reasonDetailCode, &reasonDetailDebug,
		&rec.FallbackReason, &fallbackAt, &rec.CorrelationID, &createdAt, &updatedAt,
		&lastAccess, &expiresAt, &leaseExpires, &rec.HeartbeatInterval,
		&lastHB, &rec.StopReason, &latestSeg, &lastAccessAt, &published, &contextJSON, &playbackTraceJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	_ = json.Unmarshal(profileJSON, &rec.Profile)
	_ = json.Unmarshal(contextJSON, &rec.ContextData)
	_ = json.Unmarshal(playbackTraceJSON, &rec.PlaybackTrace)
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

// --- Multi-Resource Transactional Claim Engine (Phase 3) ---

func (s *SqliteStore) TryAcquireClaimSet(ctx context.Context, req model.ClaimSetRequest) (model.ClaimSetResult, error) {
	if req.SessionID == "" {
		return model.ClaimSetResult{Success: false, ConflictType: model.ConflictCapacityExhausted, ConflictDesc: "session_id required"}, nil
	}
	if req.TTL <= 0 {
		req.TTL = 30 * time.Second
	}
	genToken := req.GenerationToken
	if genToken == "" {
		genToken = uuid.New().String()
	}
	maxMembers := req.MaxMuxMembers
	if maxMembers <= 0 {
		maxMembers = 8
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return model.ClaimSetResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	expiresAt := time.Now().Add(req.TTL).UnixMilli()

	// 1. Multiplex-Reuse: check if active multiplex allocation already exists
	if req.MultiplexID != "" {
		var existingInput, existingDemod string
		var existingPlane sql.NullString
		var existingSCR sql.NullInt64
		var muxExpires int64

		err = tx.QueryRowContext(ctx,
			"SELECT input_id, demod_id, required_plane, scr_slot, expires_at_ms FROM mux_allocations WHERE multiplex_id = ?",
			req.MultiplexID,
		).Scan(&existingInput, &existingDemod, &existingPlane, &existingSCR, &muxExpires)

		if err == nil && muxExpires > now {
			// Invariant: verify parent hardware claims are still held and not expired
			var demodOwner string
			var demodExpires int64
			demodKey := model.LeaseKeyDemod(existingDemod)
			errDemod := tx.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", demodKey).Scan(&demodOwner, &demodExpires)
			if errDemod == nil && demodExpires > now {
				// Check member count against MaxMuxMembers
				var activeMemberCount int
				_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mux_members WHERE multiplex_id = ? AND expires_at_ms > ?", req.MultiplexID, now).Scan(&activeMemberCount)

				if activeMemberCount < maxMembers {
					// Parent demod is valid and capacity allows -> join multiplex as member!
					_, err = tx.ExecContext(ctx,
						"INSERT OR REPLACE INTO mux_members (multiplex_id, session_id, generation_token, joined_at_ms, expires_at_ms) VALUES (?, ?, ?, ?, ?)",
						req.MultiplexID, req.SessionID, genToken, now, expiresAt,
					)
					if err != nil {
						return model.ClaimSetResult{}, err
					}

					// Extend mux_allocations expiry if member extends past it
					if expiresAt > muxExpires {
						_, _ = tx.ExecContext(ctx, "UPDATE mux_allocations SET expires_at_ms = ? WHERE multiplex_id = ?", expiresAt, req.MultiplexID)
						_, _ = tx.ExecContext(ctx, "UPDATE leases SET expires_at_ms = ? WHERE key = ?", expiresAt, demodKey)
						if existingInput != "" {
							_, _ = tx.ExecContext(ctx, "UPDATE input_claims SET expires_at_ms = ? WHERE input_id = ?", expiresAt, existingInput)
							_, _ = tx.ExecContext(ctx, "UPDATE input_claim_owners SET expires_at_ms = ? WHERE input_id = ?", expiresAt, existingInput)
						}
					}

					if err := tx.Commit(); err != nil {
						return model.ClaimSetResult{}, err
					}

					return model.ClaimSetResult{
						Success:         true,
						GenerationToken: genToken,
						ReusedMux:       true,
						DemodID:         existingDemod,
						InputID:         existingInput,
						ExpiresAt:       time.UnixMilli(expiresAt),
					}, nil
				}
				// If multiplex is at capacity (activeMemberCount >= maxMembers), fall through
				// to attempt independent demod allocation or return ConflictDemuxExhausted if hardware unavailable.
			}
		}
	}

	// 2. Compatible-Shared Input Check
	if req.InputID != "" && req.RequiredPlane != "" {
		var activePlane string
		var inputExpires int64
		err = tx.QueryRowContext(ctx, "SELECT active_plane, expires_at_ms FROM input_claims WHERE input_id = ?", req.InputID).Scan(&activePlane, &inputExpires)

		if err == nil && inputExpires > now {
			if activePlane != req.RequiredPlane {
				// Plane conflict on same physical coaxial input
				return model.ClaimSetResult{
					Success:      false,
					ConflictType: model.ConflictPlaneConflict,
					ConflictDesc: fmt.Sprintf("input %s is locked to plane %s (requested %s)", req.InputID, activePlane, req.RequiredPlane),
				}, nil
			}
			// Compatible: extend input expiry if needed
			if expiresAt > inputExpires {
				_, _ = tx.ExecContext(ctx, "UPDATE input_claims SET expires_at_ms = ? WHERE input_id = ?", expiresAt, req.InputID)
			}
		} else {
			// Input is free or expired -> claim with requested plane
			_, err = tx.ExecContext(ctx,
				"INSERT OR REPLACE INTO input_claims (input_id, active_plane, expires_at_ms) VALUES (?, ?, ?)",
				req.InputID, req.RequiredPlane, expiresAt,
			)
			if err != nil {
				return model.ClaimSetResult{}, err
			}
		}

		// Register session as owner of this input
		_, err = tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO input_claim_owners (input_id, session_id, generation_token, expires_at_ms) VALUES (?, ?, ?, ?)",
			req.InputID, req.SessionID, genToken, expiresAt,
		)
		if err != nil {
			return model.ClaimSetResult{}, err
		}
	}

	// 3. Exclusive Demod Check
	if req.DemodID != "" {
		demodKey := model.LeaseKeyDemod(req.DemodID)
		var currentOwner string
		var currentExpires int64
		err = tx.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", demodKey).Scan(&currentOwner, &currentExpires)
		if err == nil && currentExpires > now && currentOwner != req.SessionID {
			return model.ClaimSetResult{
				Success:      false,
				ConflictType: model.ConflictDemodOccupied,
				ConflictDesc: fmt.Sprintf("demod %s is occupied by session %s", req.DemodID, currentOwner),
			}, nil
		}

		_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO leases (key, owner, expires_at_ms) VALUES (?, ?, ?)", demodKey, req.SessionID, expiresAt)
		if err != nil {
			return model.ClaimSetResult{}, err
		}
	}

	// 4. Exclusive SCR Slot Check
	if req.SCRSlot != nil && req.InputID != "" {
		scrKey := model.LeaseKeySCR(req.InputID, *req.SCRSlot)
		var currentOwner string
		var currentExpires int64
		err = tx.QueryRowContext(ctx, "SELECT owner, expires_at_ms FROM leases WHERE key = ?", scrKey).Scan(&currentOwner, &currentExpires)
		if err == nil && currentExpires > now && currentOwner != req.SessionID {
			return model.ClaimSetResult{
				Success:      false,
				ConflictType: model.ConflictSCROccupied,
				ConflictDesc: fmt.Sprintf("scr slot %d on input %s is occupied by %s", *req.SCRSlot, req.InputID, currentOwner),
			}, nil
		}

		_, err = tx.ExecContext(ctx, "INSERT OR REPLACE INTO leases (key, owner, expires_at_ms) VALUES (?, ?, ?)", scrKey, req.SessionID, expiresAt)
		if err != nil {
			return model.ClaimSetResult{}, err
		}
	}

	// 5. Multiplex Creation (Parent Hardware + Initial Member)
	if req.MultiplexID != "" {
		var planeVal *string
		if req.RequiredPlane != "" {
			planeVal = &req.RequiredPlane
		}
		_, err = tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO mux_allocations (multiplex_id, input_id, demod_id, required_plane, scr_slot, expires_at_ms) VALUES (?, ?, ?, ?, ?, ?)",
			req.MultiplexID, req.InputID, req.DemodID, planeVal, req.SCRSlot, expiresAt,
		)
		if err != nil {
			return model.ClaimSetResult{}, err
		}

		_, err = tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO mux_members (multiplex_id, session_id, generation_token, joined_at_ms, expires_at_ms) VALUES (?, ?, ?, ?, ?)",
			req.MultiplexID, req.SessionID, genToken, now, expiresAt,
		)
		if err != nil {
			return model.ClaimSetResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.ClaimSetResult{}, err
	}

	return model.ClaimSetResult{
		Success:         true,
		GenerationToken: genToken,
		ReusedMux:       false,
		DemodID:         req.DemodID,
		InputID:         req.InputID,
		ExpiresAt:       time.UnixMilli(expiresAt),
	}, nil
}

func (s *SqliteStore) ReleaseClaimSet(ctx context.Context, sessionID string, generationToken string) error {
	if sessionID == "" {
		return nil
	}
	if generationToken == "" {
		return errors.New("generation_token is required for ReleaseClaimSet; use ForceAdminReleaseClaimSet for admin override")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()

	// 1. Find multiplexes where this session is a member matching generationToken
	rows, err := tx.QueryContext(ctx, "SELECT multiplex_id FROM mux_members WHERE session_id = ? AND generation_token = ?", sessionID, generationToken)
	if err == nil {
		var muxIDs []string
		for rows.Next() {
			var mID string
			if err := rows.Scan(&mID); err == nil {
				muxIDs = append(muxIDs, mID)
			}
		}
		_ = rows.Close()

		// Remove session from mux_members matching generationToken
		resDel, _ := tx.ExecContext(ctx, "DELETE FROM mux_members WHERE session_id = ? AND generation_token = ?", sessionID, generationToken)
		rowsAffected, _ := resDel.RowsAffected()

		if rowsAffected > 0 {
			// Check if any affected mux now has 0 remaining active members
			for _, mID := range muxIDs {
				var activeMembers int
				_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mux_members WHERE multiplex_id = ? AND expires_at_ms > ?", mID, now).Scan(&activeMembers)
				if activeMembers == 0 {
					// Free parent hardware
					var inID, dID string
					var scrSlot sql.NullInt64
					errMux := tx.QueryRowContext(ctx, "SELECT input_id, demod_id, scr_slot FROM mux_allocations WHERE multiplex_id = ?", mID).Scan(&inID, &dID, &scrSlot)
					if errMux == nil {
						_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeyDemod(dID))
						if scrSlot.Valid {
							_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeySCR(inID, int(scrSlot.Int64)))
						}
					}
					_, _ = tx.ExecContext(ctx, "DELETE FROM mux_allocations WHERE multiplex_id = ?", mID)
				}
			}
		}
	}

	// 2. Remove session from input_claim_owners matching generationToken
	rowsIn, errIn := tx.QueryContext(ctx, "SELECT input_id FROM input_claim_owners WHERE session_id = ? AND generation_token = ?", sessionID, generationToken)
	if errIn == nil {
		var inIDs []string
		for rowsIn.Next() {
			var inID string
			if err := rowsIn.Scan(&inID); err == nil {
				inIDs = append(inIDs, inID)
			}
		}
		_ = rowsIn.Close()

		_, _ = tx.ExecContext(ctx, "DELETE FROM input_claim_owners WHERE session_id = ? AND generation_token = ?", sessionID, generationToken)

		for _, inID := range inIDs {
			var activeOwners int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM input_claim_owners WHERE input_id = ? AND expires_at_ms > ?", inID, now).Scan(&activeOwners)
			if activeOwners == 0 {
				_, _ = tx.ExecContext(ctx, "DELETE FROM input_claims WHERE input_id = ?", inID)
			}
		}
	}

	// 3. Delete standalone leases owned by this session (excluding demod/scr leases held by active mux allocations)
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM leases
		WHERE owner = ?
		  AND key NOT IN (SELECT 'demod:' || demod_id FROM mux_allocations WHERE expires_at_ms > ?)
		  AND key NOT IN (SELECT 'scr:' || input_id || ':' || scr_slot FROM mux_allocations WHERE scr_slot IS NOT NULL AND expires_at_ms > ?)
	`, sessionID, now, now)

	return tx.Commit()
}

func (s *SqliteStore) ForceAdminReleaseClaimSet(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()

	// Find all multiplexes where this session is a member
	rows, err := tx.QueryContext(ctx, "SELECT multiplex_id FROM mux_members WHERE session_id = ?", sessionID)
	if err == nil {
		var muxIDs []string
		for rows.Next() {
			var mID string
			if err := rows.Scan(&mID); err == nil {
				muxIDs = append(muxIDs, mID)
			}
		}
		_ = rows.Close()

		_, _ = tx.ExecContext(ctx, "DELETE FROM mux_members WHERE session_id = ?", sessionID)

		for _, mID := range muxIDs {
			var activeMembers int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mux_members WHERE multiplex_id = ? AND expires_at_ms > ?", mID, now).Scan(&activeMembers)
			if activeMembers == 0 {
				var inID, dID string
				var scrSlot sql.NullInt64
				errMux := tx.QueryRowContext(ctx, "SELECT input_id, demod_id, scr_slot FROM mux_allocations WHERE multiplex_id = ?", mID).Scan(&inID, &dID, &scrSlot)
				if errMux == nil {
					_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeyDemod(dID))
					if scrSlot.Valid {
						_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeySCR(inID, int(scrSlot.Int64)))
					}
				}
				_, _ = tx.ExecContext(ctx, "DELETE FROM mux_allocations WHERE multiplex_id = ?", mID)
			}
		}
	}

	// Remove session from input_claim_owners
	rowsIn, errIn := tx.QueryContext(ctx, "SELECT input_id FROM input_claim_owners WHERE session_id = ?", sessionID)
	if errIn == nil {
		var inIDs []string
		for rowsIn.Next() {
			var inID string
			if err := rowsIn.Scan(&inID); err == nil {
				inIDs = append(inIDs, inID)
			}
		}
		_ = rowsIn.Close()

		_, _ = tx.ExecContext(ctx, "DELETE FROM input_claim_owners WHERE session_id = ?", sessionID)

		for _, inID := range inIDs {
			var activeOwners int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM input_claim_owners WHERE input_id = ? AND expires_at_ms > ?", inID, now).Scan(&activeOwners)
			if activeOwners == 0 {
				_, _ = tx.ExecContext(ctx, "DELETE FROM input_claims WHERE input_id = ?", inID)
			}
		}
	}

	_, _ = tx.ExecContext(ctx, `
		DELETE FROM leases
		WHERE owner = ?
		  AND key NOT IN (SELECT 'demod:' || demod_id FROM mux_allocations WHERE expires_at_ms > ?)
		  AND key NOT IN (SELECT 'scr:' || input_id || ':' || scr_slot FROM mux_allocations WHERE scr_slot IS NOT NULL AND expires_at_ms > ?)
	`, sessionID, now, now)

	return tx.Commit()
}

func (s *SqliteStore) ReapExpiredClaimMembers(ctx context.Context) (int, int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()

	// 1. Delete expired mux members
	resMembers, err := tx.ExecContext(ctx, "DELETE FROM mux_members WHERE expires_at_ms <= ?", now)
	if err != nil {
		return 0, 0, err
	}
	reapedMembersCount, _ := resMembers.RowsAffected()

	// 2. Delete expired input owners
	_, _ = tx.ExecContext(ctx, "DELETE FROM input_claim_owners WHERE expires_at_ms <= ?", now)

	// Clean up input claims with no active owners
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM input_claims
		WHERE input_id NOT IN (SELECT DISTINCT input_id FROM input_claim_owners WHERE expires_at_ms > ?)
		   OR expires_at_ms <= ?
	`, now, now)

	// 3. Find mux allocations with 0 active members
	rows, err := tx.QueryContext(ctx, `
		SELECT multiplex_id, input_id, demod_id, scr_slot
		FROM mux_allocations
		WHERE multiplex_id NOT IN (SELECT DISTINCT multiplex_id FROM mux_members WHERE expires_at_ms > ?)
		   OR expires_at_ms <= ?
	`, now, now)

	reapedMuxCount := 0
	if err == nil {
		type orphanedMux struct {
			muxID   string
			inputID string
			demodID string
			scrSlot sql.NullInt64
		}
		var orphans []orphanedMux
		for rows.Next() {
			var o orphanedMux
			if err := rows.Scan(&o.muxID, &o.inputID, &o.demodID, &o.scrSlot); err == nil {
				orphans = append(orphans, o)
			}
		}
		_ = rows.Close()

		for _, o := range orphans {
			reapedMuxCount++
			_, _ = tx.ExecContext(ctx, "DELETE FROM mux_allocations WHERE multiplex_id = ?", o.muxID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeyDemod(o.demodID))
			if o.scrSlot.Valid {
				_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeySCR(o.inputID, int(o.scrSlot.Int64)))
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	return int(reapedMembersCount), reapedMuxCount, nil
}

func (s *SqliteStore) ApplyReconciliationPlan(ctx context.Context, plan model.ReconciliationPlan) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()

	// 1. Reap specific sessions identified in plan
	for _, sessionID := range plan.SessionsToReap {
		// Delete from mux_members
		_, _ = tx.ExecContext(ctx, "DELETE FROM mux_members WHERE session_id = ?", sessionID)
		// Delete from input_claim_owners
		_, _ = tx.ExecContext(ctx, "DELETE FROM input_claim_owners WHERE session_id = ?", sessionID)
		// Delete standalone leases
		_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE owner = ?", sessionID)
	}

	// 2. Delete explicitly expired mux allocations identified in plan
	for _, muxID := range plan.ExpiredMuxes {
		var inID, dID string
		var scrSlot sql.NullInt64
		errMux := tx.QueryRowContext(ctx, "SELECT input_id, demod_id, scr_slot FROM mux_allocations WHERE multiplex_id = ?", muxID).Scan(&inID, &dID, &scrSlot)
		if errMux == nil {
			_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeyDemod(dID))
			if scrSlot.Valid {
				_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeySCR(inID, int(scrSlot.Int64)))
			}
		}
		_, _ = tx.ExecContext(ctx, "DELETE FROM mux_members WHERE multiplex_id = ?", muxID)
		_, _ = tx.ExecContext(ctx, "DELETE FROM mux_allocations WHERE multiplex_id = ?", muxID)
	}

	// 3. Clean up any remaining orphaned input claims and mux allocations
	_, _ = tx.ExecContext(ctx, `
		DELETE FROM input_claims
		WHERE input_id NOT IN (SELECT DISTINCT input_id FROM input_claim_owners WHERE expires_at_ms > ?)
		   OR expires_at_ms <= ?
	`, now, now)

	rows, err := tx.QueryContext(ctx, `
		SELECT multiplex_id, input_id, demod_id, scr_slot
		FROM mux_allocations
		WHERE multiplex_id NOT IN (SELECT DISTINCT multiplex_id FROM mux_members WHERE expires_at_ms > ?)
		   OR expires_at_ms <= ?
	`, now, now)

	if err == nil {
		type orphanedMux struct {
			muxID   string
			inputID string
			demodID string
			scrSlot sql.NullInt64
		}
		var orphans []orphanedMux
		for rows.Next() {
			var o orphanedMux
			if err := rows.Scan(&o.muxID, &o.inputID, &o.demodID, &o.scrSlot); err == nil {
				orphans = append(orphans, o)
			}
		}
		_ = rows.Close()

		for _, o := range orphans {
			_, _ = tx.ExecContext(ctx, "DELETE FROM mux_allocations WHERE multiplex_id = ?", o.muxID)
			_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeyDemod(o.demodID))
			if o.scrSlot.Valid {
				_, _ = tx.ExecContext(ctx, "DELETE FROM leases WHERE key = ?", model.LeaseKeySCR(o.inputID, int(o.scrSlot.Int64)))
			}
		}
	}

	return tx.Commit()
}
