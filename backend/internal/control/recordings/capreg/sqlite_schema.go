package capreg

import (
	"database/sql"
	"fmt"

	sqlitepkg "github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

func createSchemaV8(tx *sql.Tx) error {
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
		bitrate_confidence TEXT NOT NULL,
		bitrate_bucket TEXT NOT NULL,
		width INTEGER NOT NULL,
		height INTEGER NOT NULL,
		fps REAL NOT NULL,
		signal_fps REAL NOT NULL,
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

	CREATE TABLE IF NOT EXISTS capability_policy_state (
		subject_kind TEXT NOT NULL,
		source_fingerprint TEXT NOT NULL,
		device_fingerprint TEXT NOT NULL,
		host_fingerprint TEXT NOT NULL,
		max_quality_rung TEXT NOT NULL,
		confidence_json TEXT NOT NULL,
		updated_at_ms INTEGER NOT NULL,
		PRIMARY KEY(subject_kind, source_fingerprint, device_fingerprint, host_fingerprint)
	);
	CREATE INDEX IF NOT EXISTS idx_capability_policy_state_updated_at ON capability_policy_state(updated_at_ms DESC);
	`); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV1ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV2ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV4ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV5ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV6ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func migrateSchemaV7ToV8(tx *sql.Tx) error {
	if err := createSchemaV8(tx); err != nil {
		return err
	}
	return nil
}

func ensureSchemaV8Columns(tx *sql.Tx) error {
	hasSourceFingerprint, err := sqlitepkg.TableHasColumn(tx, "capability_observations", "source_fingerprint")
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
		hasColumn, err := sqlitepkg.TableHasColumn(tx, "capability_observations", column.name)
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
	hasReceiverContext, err := sqlitepkg.TableHasColumn(tx, "capability_sources", "receiver_context_json")
	if err != nil {
		return err
	}
	if !hasReceiverContext {
		if _, err := tx.Exec(`ALTER TABLE capability_sources ADD COLUMN receiver_context_json TEXT`); err != nil {
			return err
		}
	}
	hasBitrateConfidence, err := sqlitepkg.TableHasColumn(tx, "capability_sources", "bitrate_confidence")
	if err != nil {
		return err
	}
	if !hasBitrateConfidence {
		if _, err := tx.Exec(`ALTER TABLE capability_sources ADD COLUMN bitrate_confidence TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasBitrateBucket, err := sqlitepkg.TableHasColumn(tx, "capability_sources", "bitrate_bucket")
	if err != nil {
		return err
	}
	if !hasBitrateBucket {
		if _, err := tx.Exec(`ALTER TABLE capability_sources ADD COLUMN bitrate_bucket TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasSignalFPS, err := sqlitepkg.TableHasColumn(tx, "capability_sources", "signal_fps")
	if err != nil {
		return err
	}
	if !hasSignalFPS {
		if _, err := tx.Exec(`ALTER TABLE capability_sources ADD COLUMN signal_fps REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	for _, column := range []string{"brand", "product", "device_name"} {
		hasColumn, err := sqlitepkg.TableHasColumn(tx, "capability_devices", column)
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
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_capability_observations_feedback_path ON capability_observations(observation_kind, source_fingerprint, device_fingerprint, host_fingerprint, observed_at_ms DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS capability_policy_state (
		subject_kind TEXT NOT NULL,
		source_fingerprint TEXT NOT NULL,
		device_fingerprint TEXT NOT NULL,
		host_fingerprint TEXT NOT NULL,
		max_quality_rung TEXT NOT NULL DEFAULT '',
		confidence_json TEXT NOT NULL DEFAULT '{}',
		updated_at_ms INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(subject_kind, source_fingerprint, device_fingerprint, host_fingerprint)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_capability_policy_state_updated_at ON capability_policy_state(updated_at_ms DESC)`); err != nil {
		return err
	}
	return nil
}
