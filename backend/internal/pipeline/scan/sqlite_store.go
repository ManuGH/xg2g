package scan

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
)

const (
	schemaVersion = 5 // Includes persisted media truth for live decision reuse.
	SchemaVersion = schemaVersion
)

// SqliteStore implements Capability storage using SQLite.
type SqliteStore struct {
	DB *sql.DB
}

// NewSqliteStore initializes a new SQLite capability store.
func NewSqliteStore(dbPath string) (*SqliteStore, error) {
	db, err := sqlite.Open(dbPath, sqlite.DefaultConfig())
	if err != nil {
		return nil, err
	}

	s := &SqliteStore{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("capability store: migration failed: %w", err)
	}

	return s, nil
}

func (s *SqliteStore) migrate() error {
	var currentVersion int
	err := s.DB.QueryRow("PRAGMA user_version").Scan(&currentVersion)
	if err != nil {
		return err
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	schema := `
	CREATE TABLE IF NOT EXISTS capabilities (
		service_ref TEXT PRIMARY KEY,
		interlaced BOOLEAN NOT NULL DEFAULT 0,
		last_scan TEXT NOT NULL,
		last_attempt TEXT,
		last_success TEXT,
		scan_state TEXT,
		failure_reason TEXT,
		next_retry_at TEXT,
		resolution TEXT NOT NULL,
		codec TEXT NOT NULL,
		container TEXT,
		video_codec TEXT,
		audio_codec TEXT,
		width INTEGER NOT NULL DEFAULT 0,
		height INTEGER NOT NULL DEFAULT 0,
		fps REAL NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_capabilities_scan ON capabilities(last_scan);

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

	for _, stmt := range []string{
		`ALTER TABLE capabilities ADD COLUMN last_attempt TEXT`,
		`ALTER TABLE capabilities ADD COLUMN last_success TEXT`,
		`ALTER TABLE capabilities ADD COLUMN scan_state TEXT`,
		`ALTER TABLE capabilities ADD COLUMN failure_reason TEXT`,
		`ALTER TABLE capabilities ADD COLUMN next_retry_at TEXT`,
		`ALTER TABLE capabilities ADD COLUMN container TEXT`,
		`ALTER TABLE capabilities ADD COLUMN video_codec TEXT`,
		`ALTER TABLE capabilities ADD COLUMN audio_codec TEXT`,
		`ALTER TABLE capabilities ADD COLUMN width INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE capabilities ADD COLUMN height INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE capabilities ADD COLUMN fps REAL NOT NULL DEFAULT 0`,
	} {
		if err := execIfMissingColumn(tx, "capabilities", stmt); err != nil {
			return err
		}
	}

	if currentVersion < schemaVersion {
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SqliteStore) Update(cap Capability) {
	cap = cap.Normalized()
	if cap.ServiceRef != "" {
		_, _ = s.DB.Exec(
			`DELETE FROM capabilities WHERE RTRIM(service_ref, ':') = ? AND service_ref <> ?`,
			cap.ServiceRef,
			cap.ServiceRef,
		)
	}
	query := `
	INSERT INTO capabilities (
		service_ref, interlaced, last_scan, last_attempt, last_success,
		scan_state, failure_reason, next_retry_at, resolution, codec,
		container, video_codec, audio_codec, width, height, fps
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(service_ref) DO UPDATE SET
		interlaced = excluded.interlaced,
		last_scan = excluded.last_scan,
		last_attempt = excluded.last_attempt,
		last_success = excluded.last_success,
		scan_state = excluded.scan_state,
		failure_reason = excluded.failure_reason,
		next_retry_at = excluded.next_retry_at,
		resolution = excluded.resolution,
		codec = excluded.codec,
		container = excluded.container,
		video_codec = excluded.video_codec,
		audio_codec = excluded.audio_codec,
		width = excluded.width,
		height = excluded.height,
		fps = excluded.fps
	`
	_, _ = s.DB.Exec(query,
		cap.ServiceRef,
		cap.Interlaced,
		dbTimeOrEmpty(cap.LastScan),
		dbNullableTime(cap.LastAttempt),
		dbNullableTime(cap.LastSuccess),
		string(cap.State),
		dbNullableString(cap.FailureReason),
		dbNullableTime(cap.NextRetryAt),
		cap.Resolution,
		cap.Codec,
		dbNullableString(cap.Container),
		dbNullableString(cap.VideoCodec),
		dbNullableString(cap.AudioCodec),
		cap.Width,
		cap.Height,
		cap.FPS,
	)
}

func (s *SqliteStore) Get(serviceRef string) (Capability, bool) {
	normalizedRef := normalize.ServiceRef(serviceRef)
	query := `
	SELECT service_ref, interlaced, last_scan, last_attempt, last_success, scan_state, failure_reason, next_retry_at, resolution, codec,
		container, video_codec, audio_codec, width, height, fps
	FROM capabilities
	WHERE RTRIM(service_ref, ':') = ?
	ORDER BY CASE WHEN service_ref = ? THEN 0 ELSE 1 END
	LIMIT 1
	`
	var cap Capability
	var storedRef string
	var lastScanStr string
	var lastAttemptStr sql.NullString
	var lastSuccessStr sql.NullString
	var scanState sql.NullString
	var failureReason sql.NullString
	var nextRetryAt sql.NullString
	var container sql.NullString
	var videoCodec sql.NullString
	var audioCodec sql.NullString
	var width sql.NullInt64
	var height sql.NullInt64
	var fps sql.NullFloat64
	err := s.DB.QueryRow(query, normalizedRef, normalizedRef).Scan(
		&storedRef,
		&cap.Interlaced,
		&lastScanStr,
		&lastAttemptStr,
		&lastSuccessStr,
		&scanState,
		&failureReason,
		&nextRetryAt,
		&cap.Resolution,
		&cap.Codec,
		&container,
		&videoCodec,
		&audioCodec,
		&width,
		&height,
		&fps,
	)
	if err != nil {
		return Capability{}, false
	}
	cap.ServiceRef = normalize.ServiceRef(storedRef)
	if lastScanStr != "" {
		cap.LastScan, err = time.Parse(time.RFC3339, lastScanStr)
		if err != nil {
			return Capability{}, false
		}
	}
	cap.LastAttempt = parseNullableTime(lastAttemptStr)
	cap.LastSuccess = parseNullableTime(lastSuccessStr)
	if scanState.Valid {
		cap.State = CapabilityState(scanState.String)
	}
	if failureReason.Valid {
		cap.FailureReason = failureReason.String
	}
	if container.Valid {
		cap.Container = container.String
	}
	if videoCodec.Valid {
		cap.VideoCodec = videoCodec.String
	}
	if audioCodec.Valid {
		cap.AudioCodec = audioCodec.String
	}
	if width.Valid {
		cap.Width = int(width.Int64)
	}
	if height.Valid {
		cap.Height = int(height.Int64)
	}
	if fps.Valid {
		cap.FPS = fps.Float64
	}
	cap.NextRetryAt = parseNullableTime(nextRetryAt)
	return cap.Normalized(), true
}

func (s *SqliteStore) Close() error {
	return s.DB.Close()
}

func execIfMissingColumn(tx *sql.Tx, tableName, stmt string) error {
	columnName, err := columnNameFromAlter(stmt)
	if err != nil {
		return err
	}
	exists, err := hasColumn(tx, tableName, columnName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = tx.Exec(stmt)
	return err
}

func columnNameFromAlter(stmt string) (string, error) {
	var tableName, columnName, columnType string
	if _, err := fmt.Sscanf(stmt, "ALTER TABLE %s ADD COLUMN %s %s", &tableName, &columnName, &columnType); err != nil {
		return "", fmt.Errorf("parse alter statement %q: %w", stmt, err)
	}
	return columnName, nil
}

func hasColumn(tx *sql.Tx, tableName, columnName string) (bool, error) {
	rows, err := tx.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func dbTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func dbNullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func dbNullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseNullableTime(value sql.NullString) time.Time {
	if !value.Valid || value.String == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value.String)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
