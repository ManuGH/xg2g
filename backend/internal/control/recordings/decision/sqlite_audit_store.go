package decision

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	auditSchemaVersion       = 3
	historyRetention         = 30 * 24 * time.Hour
	historyEntriesPerKey     = 20
	defaultUnknownEventValue = "unknown"
)

type SqliteAuditStore struct {
	DB *sql.DB
}

type currentAuditRow struct {
	OutputHash   string
	ChangedAtMS  int64
	LastSeenAtMS int64
}

func NewSqliteAuditStore(dbPath string) (*SqliteAuditStore, error) {
	db, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	if err != nil {
		return nil, err
	}

	s := &SqliteAuditStore{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("decision audit store: migration failed: %w", err)
	}

	return s, nil
}

func (s *SqliteAuditStore) migrate() error {
	var currentVersion int
	if err := s.DB.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	switch {
	case currentVersion <= 0:
		if err := createAuditSchemaV3(tx); err != nil {
			return err
		}
	case currentVersion == 1:
		if err := migrateAuditSchemaV1ToV2(tx); err != nil {
			return err
		}
		if err := migrateAuditSchemaV2ToV3(tx); err != nil {
			return err
		}
	case currentVersion == 2:
		if err := migrateAuditSchemaV2ToV3(tx); err != nil {
			return err
		}
	default:
		if err := createAuditSchemaV3(tx); err != nil {
			return err
		}
	}

	if currentVersion < auditSchemaVersion {
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", auditSchemaVersion)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func createAuditSchemaV3(tx *sql.Tx) error {
	if _, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS decision_current (
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		shadow_json TEXT,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		changed_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER NOT NULL,
		PRIMARY KEY (service_ref, subject_kind, origin, client_family, requested_intent)
	);
	CREATE INDEX IF NOT EXISTS idx_decision_current_last_seen ON decision_current(last_seen_at_ms);

	CREATE TABLE IF NOT EXISTS decision_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		shadow_json TEXT,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		decided_at_ms INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_decision_history_key_time
		ON decision_history(service_ref, subject_kind, origin, client_family, requested_intent, decided_at_ms DESC, id DESC);
	CREATE INDEX IF NOT EXISTS idx_decision_history_decided_at ON decision_history(decided_at_ms);
	`); err != nil {
		return err
	}
	return nil
}

func migrateAuditSchemaV1ToV2(tx *sql.Tx) error {
	currentHasOrigin, err := tableHasColumn(tx, "decision_current", "origin")
	if err != nil {
		return err
	}
	historyHasOrigin, err := tableHasColumn(tx, "decision_history", "origin")
	if err != nil {
		return err
	}
	if currentHasOrigin && historyHasOrigin {
		return createAuditSchemaV3(tx)
	}

	if _, err := tx.Exec(`
	CREATE TABLE decision_current_v2 (
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		changed_at_ms INTEGER NOT NULL,
		last_seen_at_ms INTEGER NOT NULL,
		PRIMARY KEY (service_ref, subject_kind, origin, client_family, requested_intent)
	);
	CREATE TABLE decision_history_v2 (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		service_ref TEXT NOT NULL,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		client_family TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		basis_hash TEXT NOT NULL,
		truth_hash TEXT NOT NULL,
		output_hash TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		target_profile_json TEXT,
		reasons_json TEXT NOT NULL,
		resolved_intent TEXT,
		host_pressure_band TEXT,
		client_caps_source TEXT,
		device_type TEXT,
		decided_at_ms INTEGER NOT NULL
	);
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
	INSERT INTO decision_current_v2 (
		service_ref, subject_kind, origin, client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, changed_at_ms, last_seen_at_ms
	)
	SELECT
		service_ref, subject_kind, 'runtime', client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, changed_at_ms, last_seen_at_ms
	FROM decision_current
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
	INSERT INTO decision_history_v2 (
		id, service_ref, subject_kind, origin, client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, decided_at_ms
	)
	SELECT
		id, service_ref, subject_kind, 'runtime', client_family, requested_intent, basis_hash, truth_hash, output_hash,
		mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
		resolved_intent, host_pressure_band, client_caps_source, device_type, decided_at_ms
	FROM decision_history
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
	DROP TABLE decision_current;
	ALTER TABLE decision_current_v2 RENAME TO decision_current;
	DROP TABLE decision_history;
	ALTER TABLE decision_history_v2 RENAME TO decision_history;
	`); err != nil {
		return err
	}

	return createAuditSchemaV3(tx)
}

func migrateAuditSchemaV2ToV3(tx *sql.Tx) error {
	currentHasShadow, err := tableHasColumn(tx, "decision_current", "shadow_json")
	if err != nil {
		return err
	}
	if !currentHasShadow {
		if _, err := tx.Exec(`ALTER TABLE decision_current ADD COLUMN shadow_json TEXT`); err != nil {
			return err
		}
	}

	historyHasShadow, err := tableHasColumn(tx, "decision_history", "shadow_json")
	if err != nil {
		return err
	}
	if !historyHasShadow {
		if _, err := tx.Exec(`ALTER TABLE decision_history ADD COLUMN shadow_json TEXT`); err != nil {
			return err
		}
	}

	return createAuditSchemaV3(tx)
}

func tableHasColumn(tx *sql.Tx, table string, column string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *SqliteAuditStore) Record(ctx context.Context, event Event) error {
	if ctx == nil {
		ctx = context.Background()
	}
	event = event.Normalized()
	if err := event.Valid(); err != nil {
		return err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	current, found, err := loadCurrentAuditRow(ctx, tx, event)
	if err != nil {
		return err
	}

	eventAtMS := event.DecidedAt.UnixMilli()
	outputChanged := !found || current.OutputHash != event.OutputHash

	if !found || eventAtMS >= current.LastSeenAtMS {
		changedAtMS := eventAtMS
		if found && !outputChanged {
			changedAtMS = current.ChangedAtMS
		}
		if err := upsertCurrentAuditRow(ctx, tx, event, changedAtMS, eventAtMS); err != nil {
			return err
		}
	}

	if outputChanged || shouldAlwaysInsertAuditHistory(event) {
		if err := insertAuditHistory(ctx, tx, event, eventAtMS); err != nil {
			return err
		}
	}
	if err := pruneAuditHistory(ctx, tx, event, time.Now().UTC().Add(-historyRetention).UnixMilli()); err != nil {
		return err
	}

	return tx.Commit()
}

func loadCurrentAuditRow(ctx context.Context, tx *sql.Tx, event Event) (currentAuditRow, bool, error) {
	var row currentAuditRow
	err := tx.QueryRowContext(
		ctx,
		`SELECT output_hash, changed_at_ms, last_seen_at_ms
		FROM decision_current
		WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?`,
		event.ServiceRef,
		normalizeSubjectKind(event.SubjectKind),
		normalizeEventOrigin(event.Origin),
		normalizeClientFamily(event.ClientFamily),
		normalizeRequestedIntent(event.RequestedIntent),
	).Scan(&row.OutputHash, &row.ChangedAtMS, &row.LastSeenAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		return currentAuditRow{}, false, nil
	}
	if err != nil {
		return currentAuditRow{}, false, err
	}
	return row, true, nil
}

func upsertCurrentAuditRow(ctx context.Context, tx *sql.Tx, event Event, changedAtMS, lastSeenAtMS int64) error {
	targetProfileJSON, reasonsJSON, shadowJSON, err := auditPayloadJSON(event)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO decision_current (
			service_ref, subject_kind, origin, client_family, requested_intent, basis_hash, truth_hash, output_hash,
			mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
			shadow_json, resolved_intent, host_pressure_band, client_caps_source, device_type, changed_at_ms, last_seen_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(service_ref, subject_kind, origin, client_family, requested_intent) DO UPDATE SET
			basis_hash = excluded.basis_hash,
			truth_hash = excluded.truth_hash,
			output_hash = excluded.output_hash,
			mode = excluded.mode,
			selected_container = excluded.selected_container,
			selected_video_codec = excluded.selected_video_codec,
			selected_audio_codec = excluded.selected_audio_codec,
			target_profile_json = excluded.target_profile_json,
			reasons_json = excluded.reasons_json,
			shadow_json = excluded.shadow_json,
			resolved_intent = excluded.resolved_intent,
			host_pressure_band = excluded.host_pressure_band,
			client_caps_source = excluded.client_caps_source,
			device_type = excluded.device_type,
			changed_at_ms = excluded.changed_at_ms,
			last_seen_at_ms = excluded.last_seen_at_ms`,
		event.ServiceRef,
		normalizeSubjectKind(event.SubjectKind),
		normalizeEventOrigin(event.Origin),
		normalizeClientFamily(event.ClientFamily),
		normalizeRequestedIntent(event.RequestedIntent),
		event.BasisHash,
		event.TruthHash,
		event.OutputHash,
		string(event.Mode),
		event.Selected.Container,
		event.Selected.VideoCodec,
		event.Selected.AudioCodec,
		targetProfileJSON,
		reasonsJSON,
		shadowJSON,
		normalizeNullableToken(event.ResolvedIntent),
		normalizeNullableToken(event.HostPressureBand),
		normalizeNullableToken(event.ClientCapsSource),
		normalizeNullableToken(event.DeviceType),
		changedAtMS,
		lastSeenAtMS,
	)
	return err
}

func insertAuditHistory(ctx context.Context, tx *sql.Tx, event Event, decidedAtMS int64) error {
	targetProfileJSON, reasonsJSON, shadowJSON, err := auditPayloadJSON(event)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO decision_history (
			service_ref, subject_kind, origin, client_family, requested_intent, basis_hash, truth_hash, output_hash,
			mode, selected_container, selected_video_codec, selected_audio_codec, target_profile_json, reasons_json,
			shadow_json, resolved_intent, host_pressure_band, client_caps_source, device_type, decided_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ServiceRef,
		normalizeSubjectKind(event.SubjectKind),
		normalizeEventOrigin(event.Origin),
		normalizeClientFamily(event.ClientFamily),
		normalizeRequestedIntent(event.RequestedIntent),
		event.BasisHash,
		event.TruthHash,
		event.OutputHash,
		string(event.Mode),
		event.Selected.Container,
		event.Selected.VideoCodec,
		event.Selected.AudioCodec,
		targetProfileJSON,
		reasonsJSON,
		shadowJSON,
		normalizeNullableToken(event.ResolvedIntent),
		normalizeNullableToken(event.HostPressureBand),
		normalizeNullableToken(event.ClientCapsSource),
		normalizeNullableToken(event.DeviceType),
		decidedAtMS,
	)
	return err
}

func pruneAuditHistory(ctx context.Context, tx *sql.Tx, event Event, cutoffMS int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM decision_history WHERE decided_at_ms < ?`, cutoffMS); err != nil {
		return err
	}

	_, err := tx.ExecContext(
		ctx,
		`DELETE FROM decision_history
		WHERE id IN (
			SELECT id FROM (
				SELECT id
				FROM decision_history
				WHERE service_ref = ? AND subject_kind = ? AND origin = ? AND client_family = ? AND requested_intent = ?
				ORDER BY decided_at_ms DESC, id DESC
				LIMIT -1 OFFSET ?
			)
		)`,
		event.ServiceRef,
		normalizeSubjectKind(event.SubjectKind),
		normalizeEventOrigin(event.Origin),
		normalizeClientFamily(event.ClientFamily),
		normalizeRequestedIntent(event.RequestedIntent),
		historyEntriesPerKey,
	)
	return err
}

func auditPayloadJSON(event Event) (sql.NullString, string, sql.NullString, error) {
	var targetProfileJSON sql.NullString
	if event.TargetProfile != nil {
		encoded, err := json.Marshal(event.TargetProfile)
		if err != nil {
			return sql.NullString{}, "", sql.NullString{}, err
		}
		targetProfileJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	reasons := make([]string, 0, len(event.Reasons))
	for _, reason := range event.Reasons {
		reasons = append(reasons, string(reason))
	}
	encodedReasons, err := json.Marshal(reasons)
	if err != nil {
		return sql.NullString{}, "", sql.NullString{}, err
	}

	var shadowJSON sql.NullString
	if event.Shadow != nil {
		encoded, err := json.Marshal(event.Shadow)
		if err != nil {
			return sql.NullString{}, "", sql.NullString{}, err
		}
		shadowJSON = sql.NullString{String: string(encoded), Valid: true}
	}

	return targetProfileJSON, string(encodedReasons), shadowJSON, nil
}

func shouldAlwaysInsertAuditHistory(event Event) bool {
	return normalizeEventOrigin(event.Origin) == OriginShadowDivergence
}

func normalizeSubjectKind(value string) string {
	if value == "" {
		return defaultUnknownEventValue
	}
	return value
}

func normalizeClientFamily(value string) string {
	if value == "" {
		return defaultUnknownEventValue
	}
	return value
}

func normalizeRequestedIntent(value string) string {
	return value
}

func normalizeNullableToken(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
