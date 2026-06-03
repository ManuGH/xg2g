package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

func openOptionalReadOnlySQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=2000", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func loadSQLiteColumnSet(db *sql.DB, table string) (map[string]bool, error) {
	if db == nil || strings.TrimSpace(table) == "" {
		return nil, nil
	}
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns[strings.ToLower(strings.TrimSpace(name))] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func sqliteSelectExpr(columns map[string]bool, name string) string {
	if columns != nil && columns[strings.ToLower(strings.TrimSpace(name))] {
		return name
	}
	return fmt.Sprintf("NULL AS %s", name)
}

func queryCapability(db *sql.DB, columns map[string]bool, serviceRef string) (scan.Capability, bool, error) {
	if db == nil || serviceRef == "" {
		return scan.Capability{}, false, nil
	}

	// #nosec G201 -- select expressions are derived from a fixed internal allowlist and only expand to column names or NULL aliases.
	query := fmt.Sprintf(`
		SELECT %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s
		FROM capabilities
		WHERE RTRIM(service_ref, ':') = ?
		ORDER BY CASE WHEN service_ref = ? THEN 0 ELSE 1 END
		LIMIT 1
		`,
		sqliteSelectExpr(columns, "service_ref"),
		sqliteSelectExpr(columns, "interlaced"),
		sqliteSelectExpr(columns, "scan_state"),
		sqliteSelectExpr(columns, "failure_reason"),
		sqliteSelectExpr(columns, "resolution"),
		sqliteSelectExpr(columns, "codec"),
		sqliteSelectExpr(columns, "container"),
		sqliteSelectExpr(columns, "video_codec"),
		sqliteSelectExpr(columns, "audio_codec"),
		sqliteSelectExpr(columns, "width"),
		sqliteSelectExpr(columns, "height"),
		sqliteSelectExpr(columns, "fps"),
	)
	var cap scan.Capability
	var storedRef string
	var interlaced sql.NullBool
	var scanState sql.NullString
	var failureReason sql.NullString
	var resolution sql.NullString
	var codec sql.NullString
	var container sql.NullString
	var videoCodec sql.NullString
	var audioCodec sql.NullString
	var width sql.NullInt64
	var height sql.NullInt64
	var fps sql.NullFloat64
	err := db.QueryRow(query, serviceRef, serviceRef).Scan(
		&storedRef,
		&interlaced,
		&scanState,
		&failureReason,
		&resolution,
		&codec,
		&container,
		&videoCodec,
		&audioCodec,
		&width,
		&height,
		&fps,
	)
	if err == sql.ErrNoRows {
		return scan.Capability{}, false, nil
	}
	if err != nil {
		return scan.Capability{}, false, err
	}
	cap.ServiceRef = storedRef
	if interlaced.Valid {
		cap.Interlaced = interlaced.Bool
	}
	if scanState.Valid {
		cap.State = scan.CapabilityState(scanState.String)
	}
	if failureReason.Valid {
		cap.FailureReason = failureReason.String
	}
	if resolution.Valid {
		cap.Resolution = resolution.String
	}
	if codec.Valid {
		cap.Codec = codec.String
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
	return cap.Normalized(), true, nil
}

func queryDecisionCurrentRows(db *sql.DB, columns map[string]bool, serviceRef string, clientFamily string, intent string, origin string) ([]storageDecisionAuditRow, error) {
	if db == nil || serviceRef == "" {
		return nil, nil
	}
	if origin != "" && columns != nil && !columns["origin"] && origin != decisionaudit.OriginRuntime {
		return nil, nil
	}

	var args []any
	// #nosec G202 -- dynamic fragments are fixed internal column expressions selected from an allowlist, not user input.
	query := `
	SELECT service_ref, ` + sqliteSelectExpr(columns, "origin") + `, client_family, requested_intent, resolved_intent, ` + sqliteSelectExpr(columns, "client_caps_source") + `, ` + sqliteSelectExpr(columns, "host_fingerprint") + `, mode, target_profile_json, reasons_json, basis_hash, changed_at_ms, last_seen_at_ms
	FROM decision_current
	WHERE service_ref = ? AND subject_kind = 'live'
	`
	args = append(args, serviceRef)
	if origin != "" && columns != nil && columns["origin"] {
		query += " AND origin = ?"
		args = append(args, origin)
	}
	if clientFamily != "" {
		query += " AND client_family = ?"
		args = append(args, clientFamily)
	}
	if intent != "" {
		query += " AND requested_intent = ?"
		args = append(args, intent)
	}
	query += " ORDER BY origin, client_family, requested_intent"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []storageDecisionAuditRow
	for rows.Next() {
		var row storageDecisionAuditRow
		var storedRef string
		var decisionOrigin sql.NullString
		var effectiveIntent sql.NullString
		var clientCapsSource sql.NullString
		var hostFingerprint sql.NullString
		var targetProfileJSON sql.NullString
		var reasonsJSON string
		var changedAtMS int64
		var lastSeenAtMS int64
		if err := rows.Scan(
			&storedRef,
			&decisionOrigin,
			&row.ClientFamily,
			&row.RequestedIntent,
			&effectiveIntent,
			&clientCapsSource,
			&hostFingerprint,
			&row.Mode,
			&targetProfileJSON,
			&reasonsJSON,
			&row.BasisHash,
			&changedAtMS,
			&lastSeenAtMS,
		); err != nil {
			return nil, err
		}
		row.ServiceRef = normalize.ServiceRef(storedRef)
		if decisionOrigin.Valid {
			row.Origin = normalizeDecisionReportOrigin(decisionOrigin.String)
		}
		if row.Origin == "" {
			row.Origin = decisionaudit.OriginRuntime
		}
		if effectiveIntent.Valid {
			row.EffectiveIntent = effectiveIntent.String
		}
		if clientCapsSource.Valid {
			row.ClientCapsSource = clientCapsSource.String
		}
		if hostFingerprint.Valid {
			row.HostFingerprint = hostFingerprint.String
		}
		if targetProfileJSON.Valid && strings.TrimSpace(targetProfileJSON.String) != "" {
			var targetProfile playbackprofile.TargetPlaybackProfile
			if err := json.Unmarshal([]byte(targetProfileJSON.String), &targetProfile); err != nil {
				return nil, fmt.Errorf("decode target profile for %s: %w", row.ServiceRef, err)
			}
			row.TargetProfile = &targetProfile
		}
		if strings.TrimSpace(reasonsJSON) != "" {
			if err := json.Unmarshal([]byte(reasonsJSON), &row.Reasons); err != nil {
				return nil, fmt.Errorf("decode reasons for %s: %w", row.ServiceRef, err)
			}
		}
		if changedAtMS > 0 {
			ts := time.UnixMilli(changedAtMS).UTC()
			row.ChangedAt = &ts
		}
		if lastSeenAtMS > 0 {
			ts := time.UnixMilli(lastSeenAtMS).UTC()
			row.LastSeenAt = &ts
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
