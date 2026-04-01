package capreg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const sqliteSchemaVersion = 5

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
		return nil, fmt.Errorf("capability registry: migration failed: %w", err)
	}
	return store, nil
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	if err := s.DB.QueryRow(`PRAGMA user_version`).Scan(&currentVersion); err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	switch {
	case currentVersion <= 0:
		if err := createSchemaV5(tx); err != nil {
			return err
		}
	case currentVersion == 1:
		if err := migrateSchemaV1ToV5(tx); err != nil {
			return err
		}
	case currentVersion == 2:
		if err := migrateSchemaV2ToV5(tx); err != nil {
			return err
		}
	case currentVersion == 4:
		if err := migrateSchemaV4ToV5(tx); err != nil {
			return err
		}
	default:
		if err := createSchemaV5(tx); err != nil {
			return err
		}
	}
	if err := ensureSchemaV5Columns(tx); err != nil {
		return err
	}

	if currentVersion < sqliteSchemaVersion {
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, sqliteSchemaVersion)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func createSchemaV5(tx *sql.Tx) error {
	if _, err := tx.Exec(`
	CREATE TABLE IF NOT EXISTS capability_hosts (
		host_fingerprint TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		os_name TEXT NOT NULL,
		os_version TEXT NOT NULL,
		architecture TEXT NOT NULL,
		runtime_json TEXT NOT NULL,
		encoder_caps_json TEXT NOT NULL,
		updated_at_ms INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_capability_hosts_updated_at ON capability_hosts(updated_at_ms DESC);

		CREATE TABLE IF NOT EXISTS capability_devices (
			device_fingerprint TEXT PRIMARY KEY,
			client_family TEXT NOT NULL,
			client_caps_source TEXT NOT NULL,
			device_type TEXT NOT NULL,
			brand TEXT NOT NULL,
			product TEXT NOT NULL,
			device_name TEXT NOT NULL,
			platform TEXT NOT NULL,
			manufacturer TEXT NOT NULL,
			model TEXT NOT NULL,
		os_name TEXT NOT NULL,
		os_version TEXT NOT NULL,
		sdk_int INTEGER NOT NULL,
		capabilities_json TEXT NOT NULL,
		capabilities_hash TEXT NOT NULL,
		network_json TEXT,
		updated_at_ms INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_capability_devices_family ON capability_devices(client_family, device_type, updated_at_ms DESC);

	CREATE TABLE IF NOT EXISTS capability_sources (
		source_fingerprint TEXT PRIMARY KEY,
		subject_kind TEXT NOT NULL,
		origin TEXT NOT NULL,
		container TEXT NOT NULL,
		video_codec TEXT NOT NULL,
		audio_codec TEXT NOT NULL,
		width INTEGER NOT NULL,
		height INTEGER NOT NULL,
		fps REAL NOT NULL,
		interlaced INTEGER NOT NULL,
		problem_flags_json TEXT NOT NULL,
		receiver_context_json TEXT,
		updated_at_ms INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_capability_sources_kind_origin ON capability_sources(subject_kind, origin, updated_at_ms DESC);

	CREATE TABLE IF NOT EXISTS capability_observations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		observed_at_ms INTEGER NOT NULL,
		request_id TEXT NOT NULL,
		observation_kind TEXT NOT NULL DEFAULT 'decision',
		outcome TEXT NOT NULL DEFAULT 'predicted',
		session_id TEXT NOT NULL DEFAULT '',
		source_ref TEXT NOT NULL,
		source_fingerprint TEXT NOT NULL DEFAULT '',
		subject_kind TEXT NOT NULL,
		requested_intent TEXT NOT NULL,
		resolved_intent TEXT NOT NULL,
		mode TEXT NOT NULL,
		selected_container TEXT NOT NULL,
		selected_video_codec TEXT NOT NULL,
		selected_audio_codec TEXT NOT NULL,
		source_width INTEGER NOT NULL,
		source_height INTEGER NOT NULL,
		source_fps REAL NOT NULL,
		host_fingerprint TEXT NOT NULL,
		device_fingerprint TEXT NOT NULL,
		client_caps_hash TEXT NOT NULL,
		feedback_event TEXT NOT NULL DEFAULT '',
		feedback_code INTEGER NOT NULL DEFAULT 0,
		feedback_message TEXT NOT NULL DEFAULT '',
		network_kind TEXT NOT NULL,
		network_metered INTEGER,
		network_downlink_kbps INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_capability_observations_time ON capability_observations(observed_at_ms DESC);
	CREATE INDEX IF NOT EXISTS idx_capability_observations_device ON capability_observations(device_fingerprint, observed_at_ms DESC);
	`); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV1ToV5(tx *sql.Tx) error {
	if err := createSchemaV5(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV2ToV5(tx *sql.Tx) error {
	if err := createSchemaV5(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV4ToV5(tx *sql.Tx) error {
	if err := createSchemaV5(tx); err != nil {
		return err
	}
	return nil
}

func ensureSchemaV5Columns(tx *sql.Tx) error {
	hasSourceFingerprint, err := tableHasColumn(tx, "capability_observations", "source_fingerprint")
	if err != nil {
		return err
	}
	if !hasSourceFingerprint {
		if _, err := tx.Exec(`ALTER TABLE capability_observations ADD COLUMN source_fingerprint TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_capability_observations_source ON capability_observations(source_fingerprint, observed_at_ms DESC)`); err != nil {
		return err
	}
	for _, column := range []struct {
		name        string
		columnType  string
		defaultExpr string
	}{
		{name: "observation_kind", columnType: "TEXT", defaultExpr: "'decision'"},
		{name: "outcome", columnType: "TEXT", defaultExpr: "'predicted'"},
		{name: "session_id", columnType: "TEXT", defaultExpr: "''"},
		{name: "feedback_event", columnType: "TEXT", defaultExpr: "''"},
		{name: "feedback_code", columnType: "INTEGER", defaultExpr: "0"},
		{name: "feedback_message", columnType: "TEXT", defaultExpr: "''"},
	} {
		hasColumn, err := tableHasColumn(tx, "capability_observations", column.name)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE capability_observations ADD COLUMN %s %s NOT NULL DEFAULT %s`, column.name, column.columnType, column.defaultExpr)); err != nil {
			return err
		}
	}
	hasReceiverContext, err := tableHasColumn(tx, "capability_sources", "receiver_context_json")
	if err != nil {
		return err
	}
	if !hasReceiverContext {
		if _, err := tx.Exec(`ALTER TABLE capability_sources ADD COLUMN receiver_context_json TEXT`); err != nil {
			return err
		}
	}
	for _, column := range []string{"brand", "product", "device_name"} {
		hasColumn, err := tableHasColumn(tx, "capability_devices", column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE capability_devices ADD COLUMN %s TEXT NOT NULL DEFAULT ''`, column)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_capability_observations_request_kind ON capability_observations(request_id, observation_kind, observed_at_ms DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_capability_observations_session ON capability_observations(session_id, observed_at_ms DESC)`); err != nil {
		return err
	}
	return nil
}

func (s *SqliteStore) RememberHost(ctx context.Context, snapshot HostSnapshot) error {
	snapshot = canonicalHostSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	runtimeJSON, err := json.Marshal(snapshot.Runtime)
	if err != nil {
		return err
	}
	encoderJSON, err := json.Marshal(snapshot.EncoderCapabilities)
	if err != nil {
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
	INSERT INTO capability_hosts (
		host_fingerprint, hostname, os_name, os_version, architecture, runtime_json, encoder_caps_json, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(host_fingerprint) DO UPDATE SET
		hostname = excluded.hostname,
		os_name = excluded.os_name,
		os_version = excluded.os_version,
		architecture = excluded.architecture,
		runtime_json = excluded.runtime_json,
		encoder_caps_json = excluded.encoder_caps_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.Identity.Hostname,
		snapshot.Identity.OSName,
		snapshot.Identity.OSVersion,
		snapshot.Identity.Architecture,
		string(runtimeJSON),
		string(encoderJSON),
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) RememberDevice(ctx context.Context, snapshot DeviceSnapshot) error {
	snapshot = canonicalDeviceSnapshot(snapshot)
	fingerprint := snapshot.Identity.Fingerprint()
	if fingerprint == "" || snapshot.Capabilities.CapabilitiesVersion == 0 {
		return nil
	}
	capsJSON, err := json.Marshal(snapshot.Capabilities)
	if err != nil {
		return err
	}
	var networkJSON any
	if snapshot.Network != nil {
		payload, err := json.Marshal(snapshot.Network)
		if err != nil {
			return err
		}
		networkJSON = string(payload)
	}

	deviceCtx := snapshot.Identity.DeviceContext
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO capability_devices (
			device_fingerprint, client_family, client_caps_source, device_type, brand, product, device_name, platform, manufacturer, model, os_name, os_version, sdk_int,
			capabilities_json, capabilities_hash, network_json, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_fingerprint) DO UPDATE SET
			client_family = excluded.client_family,
			client_caps_source = excluded.client_caps_source,
			device_type = excluded.device_type,
			brand = excluded.brand,
			product = excluded.product,
			device_name = excluded.device_name,
			platform = excluded.platform,
			manufacturer = excluded.manufacturer,
			model = excluded.model,
		os_name = excluded.os_name,
		os_version = excluded.os_version,
		sdk_int = excluded.sdk_int,
		capabilities_json = excluded.capabilities_json,
		capabilities_hash = excluded.capabilities_hash,
		network_json = excluded.network_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.Identity.ClientFamily,
		snapshot.Identity.ClientCapsSource,
		snapshot.Identity.DeviceType,
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Brand }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Product }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Device }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Platform }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Manufacturer }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.Model }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.OSName }),
		deviceContextString(deviceCtx, func(v *capabilities.DeviceContext) string { return v.OSVersion }),
		deviceContextInt(deviceCtx, func(v *capabilities.DeviceContext) int { return v.SDKInt }),
		string(capsJSON),
		HashCapabilitiesSnapshot(snapshot.Capabilities),
		networkJSON,
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) RememberSource(ctx context.Context, snapshot SourceSnapshot) error {
	snapshot = canonicalSourceSnapshot(snapshot)
	fingerprint := snapshot.Fingerprint()
	if fingerprint == "" {
		return nil
	}
	problemFlagsJSON, err := json.Marshal(snapshot.ProblemFlags)
	if err != nil {
		return err
	}
	var receiverContextJSON any
	if snapshot.ReceiverContext != nil {
		payload, err := json.Marshal(snapshot.ReceiverContext)
		if err != nil {
			return err
		}
		receiverContextJSON = string(payload)
	}

	interlaced := 0
	if snapshot.Interlaced {
		interlaced = 1
	}

	_, err = s.DB.ExecContext(ctx, `
	INSERT INTO capability_sources (
		source_fingerprint, subject_kind, origin, container, video_codec, audio_codec, width, height, fps, interlaced, problem_flags_json, receiver_context_json, updated_at_ms
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source_fingerprint) DO UPDATE SET
		subject_kind = excluded.subject_kind,
		origin = excluded.origin,
		container = excluded.container,
		video_codec = excluded.video_codec,
		audio_codec = excluded.audio_codec,
		width = excluded.width,
		height = excluded.height,
		fps = excluded.fps,
		interlaced = excluded.interlaced,
		problem_flags_json = excluded.problem_flags_json,
		receiver_context_json = excluded.receiver_context_json,
		updated_at_ms = excluded.updated_at_ms
	`,
		fingerprint,
		snapshot.SubjectKind,
		snapshot.Origin,
		snapshot.Container,
		snapshot.VideoCodec,
		snapshot.AudioCodec,
		snapshot.Width,
		snapshot.Height,
		snapshot.FPS,
		interlaced,
		string(problemFlagsJSON),
		receiverContextJSON,
		snapshot.UpdatedAt.UnixMilli(),
	)
	return err
}

func (s *SqliteStore) LookupCapabilities(ctx context.Context, identity DeviceIdentity) (capabilities.PlaybackCapabilities, bool, error) {
	fingerprint := identity.Fingerprint()
	if fingerprint == "" {
		return capabilities.PlaybackCapabilities{}, false, nil
	}

	var capsJSON string
	err := s.DB.QueryRowContext(ctx, `
	SELECT capabilities_json
	FROM capability_devices
	WHERE device_fingerprint = ?
	`, fingerprint).Scan(&capsJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return capabilities.PlaybackCapabilities{}, false, nil
		}
		return capabilities.PlaybackCapabilities{}, false, err
	}

	var caps capabilities.PlaybackCapabilities
	if err := json.Unmarshal([]byte(capsJSON), &caps); err != nil {
		return capabilities.PlaybackCapabilities{}, false, err
	}
	return capabilities.CanonicalizeCapabilities(caps), true, nil
}

func (s *SqliteStore) LookupDecisionObservation(ctx context.Context, requestID string) (PlaybackObservation, bool, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return PlaybackObservation{}, false, nil
	}

	row := s.DB.QueryRowContext(ctx, `
	SELECT
		observed_at_ms,
		request_id,
		observation_kind,
		outcome,
		session_id,
		source_ref,
		source_fingerprint,
		subject_kind,
		requested_intent,
		resolved_intent,
		mode,
		selected_container,
		selected_video_codec,
		selected_audio_codec,
		source_width,
		source_height,
		source_fps,
		host_fingerprint,
		device_fingerprint,
		client_caps_hash,
		feedback_event,
		feedback_code,
		feedback_message,
		network_kind,
		network_metered,
		network_downlink_kbps
	FROM capability_observations
	WHERE request_id = ? AND observation_kind = 'decision'
	ORDER BY observed_at_ms DESC, id DESC
	LIMIT 1
	`, requestID)

	observation, err := scanObservation(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return PlaybackObservation{}, false, nil
		}
		return PlaybackObservation{}, false, err
	}
	return observation, true, nil
}

func (s *SqliteStore) RecordObservation(ctx context.Context, observation PlaybackObservation) error {
	observation = canonicalObservation(observation)
	_, err := s.DB.ExecContext(ctx, `
	INSERT INTO capability_observations (
		observed_at_ms, request_id, observation_kind, outcome, session_id, source_ref, source_fingerprint, subject_kind, requested_intent, resolved_intent, mode,
		selected_container, selected_video_codec, selected_audio_codec, source_width, source_height, source_fps,
		host_fingerprint, device_fingerprint, client_caps_hash, feedback_event, feedback_code, feedback_message, network_kind, network_metered, network_downlink_kbps
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		observation.ObservedAt.UnixMilli(),
		observation.RequestID,
		observation.ObservationKind,
		observation.Outcome,
		observation.SessionID,
		observation.SourceRef,
		observation.SourceFingerprint,
		observation.SubjectKind,
		observation.RequestedIntent,
		observation.ResolvedIntent,
		observation.Mode,
		observation.SelectedContainer,
		observation.SelectedVideoCodec,
		observation.SelectedAudioCodec,
		observation.SourceWidth,
		observation.SourceHeight,
		observation.SourceFPS,
		observation.HostFingerprint,
		observation.DeviceFingerprint,
		observation.ClientCapsHash,
		observation.FeedbackEvent,
		observation.FeedbackCode,
		observation.FeedbackMessage,
		networkKind(observation.Network),
		networkMetered(observation.Network),
		networkDownlink(observation.Network),
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanObservation(row scanner) (PlaybackObservation, error) {
	var (
		observedAtMS         int64
		networkKindValue     string
		networkMeteredValue  sql.NullInt64
		networkDownlinkValue int
	)
	observation := PlaybackObservation{}
	if err := row.Scan(
		&observedAtMS,
		&observation.RequestID,
		&observation.ObservationKind,
		&observation.Outcome,
		&observation.SessionID,
		&observation.SourceRef,
		&observation.SourceFingerprint,
		&observation.SubjectKind,
		&observation.RequestedIntent,
		&observation.ResolvedIntent,
		&observation.Mode,
		&observation.SelectedContainer,
		&observation.SelectedVideoCodec,
		&observation.SelectedAudioCodec,
		&observation.SourceWidth,
		&observation.SourceHeight,
		&observation.SourceFPS,
		&observation.HostFingerprint,
		&observation.DeviceFingerprint,
		&observation.ClientCapsHash,
		&observation.FeedbackEvent,
		&observation.FeedbackCode,
		&observation.FeedbackMessage,
		&networkKindValue,
		&networkMeteredValue,
		&networkDownlinkValue,
	); err != nil {
		return PlaybackObservation{}, err
	}
	observation.ObservedAt = time.UnixMilli(observedAtMS).UTC()
	if networkKindValue != "" || networkDownlinkValue > 0 || networkMeteredValue.Valid {
		network := &capabilities.NetworkContext{
			Kind:         networkKindValue,
			DownlinkKbps: networkDownlinkValue,
		}
		if networkMeteredValue.Valid {
			metered := networkMeteredValue.Int64 != 0
			network.Metered = &metered
		}
		observation.Network = network
	}
	return canonicalObservation(observation), nil
}

func deviceContextString(ctx *capabilities.DeviceContext, pick func(*capabilities.DeviceContext) string) string {
	if ctx == nil {
		return ""
	}
	return pick(ctx)
}

func deviceContextInt(ctx *capabilities.DeviceContext, pick func(*capabilities.DeviceContext) int) int {
	if ctx == nil {
		return 0
	}
	return pick(ctx)
}

func networkKind(ctx *capabilities.NetworkContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.Kind
}

func networkMetered(ctx *capabilities.NetworkContext) any {
	if ctx == nil || ctx.Metered == nil {
		return nil
	}
	if *ctx.Metered {
		return 1
	}
	return 0
}

func networkDownlink(ctx *capabilities.NetworkContext) int {
	if ctx == nil {
		return 0
	}
	return ctx.DownlinkKbps
}

func (s *SqliteStore) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
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
