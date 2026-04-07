package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	_ "modernc.org/sqlite"
)

const (
	reportTruthComplete      = "complete"
	reportTruthIncomplete    = "incomplete"
	reportTruthMissing       = "missing"
	reportTruthEventInactive = "inactive_event_feed"

	reportTruthSourceScan          = "scan"
	reportTruthSourceFallback      = "fallback"
	reportTruthSourceUnresolved    = "unresolved"
	reportTruthSourceEventInactive = "event_inactive"
	reportUnknownHost              = "unknown_host"
)

type storageDecisionReportOptions struct {
	DataDir      string
	PlaylistName string
	Bouquet      string
	ClientFamily string
	Intent       string
	Origin       string
	Format       string
	OutPath      string
}

type storageDecisionReport struct {
	GeneratedAt time.Time                    `json:"generatedAt"`
	DataDir     string                       `json:"dataDir"`
	Playlist    string                       `json:"playlist"`
	Bouquet     string                       `json:"bouquet,omitempty"`
	Filters     storageDecisionReportFilters `json:"filters"`
	Summary     storageDecisionReportSummary `json:"summary"`
	Warnings    []string                     `json:"warnings,omitempty"`
	Rows        []storageDecisionReportRow   `json:"rows"`
}

type storageDecisionReportFilters struct {
	ClientFamily string `json:"clientFamily,omitempty"`
	Intent       string `json:"intent,omitempty"`
	Origin       string `json:"origin,omitempty"`
	SubjectKind  string `json:"subjectKind"`
}

type storageDecisionReportSummary struct {
	ServicesTotal            int `json:"servicesTotal"`
	RowsTotal                int `json:"rowsTotal"`
	ServicesWithDecision     int `json:"servicesWithDecision"`
	ServicesWithoutDecision  int `json:"servicesWithoutDecision"`
	TruthComplete            int `json:"truthComplete"`
	TruthIncomplete          int `json:"truthIncomplete"`
	TruthMissing             int `json:"truthMissing"`
	TruthEventInactive       int `json:"truthEventInactive"`
	TruthSourceScan          int `json:"truthSourceScan"`
	TruthSourceFallback      int `json:"truthSourceFallback"`
	TruthSourceUnresolved    int `json:"truthSourceUnresolved"`
	TruthSourceEventInactive int `json:"truthSourceEventInactive"`
	DistinctHostFingerprints int `json:"distinctHostFingerprints"`
	DistinctBasisHostPairs   int `json:"distinctBasisHostPairs"`
	BasisHashesWithMultiHost int `json:"basisHashesWithMultiHost"`
	UnknownHostRows          int `json:"unknownHostRows"`
}

type storageDecisionReportRow struct {
	ServiceRef           string                                 `json:"serviceRef"`
	ChannelName          string                                 `json:"channelName"`
	Bouquet              string                                 `json:"bouquet"`
	TruthSource          string                                 `json:"truthSource"`
	TruthStatus          string                                 `json:"truthStatus"`
	DecisionPresent      bool                                   `json:"decisionPresent"`
	ScanState            string                                 `json:"scanState,omitempty"`
	ScanFailureReason    string                                 `json:"scanFailureReason,omitempty"`
	ScanContainer        string                                 `json:"scanContainer,omitempty"`
	ScanVideoCodec       string                                 `json:"scanVideoCodec,omitempty"`
	ScanAudioCodec       string                                 `json:"scanAudioCodec,omitempty"`
	ScanResolution       string                                 `json:"scanResolution,omitempty"`
	DecisionOrigin       string                                 `json:"decisionOrigin,omitempty"`
	ClientFamily         string                                 `json:"clientFamily,omitempty"`
	ClientCapsSource     string                                 `json:"clientCapsSource,omitempty"`
	ClientCapsSourceCode string                                 `json:"clientCapsSourceCode,omitempty"`
	HostFingerprint      string                                 `json:"hostFingerprint,omitempty"`
	RequestedIntent      string                                 `json:"requestedIntent,omitempty"`
	EffectiveIntent      string                                 `json:"effectiveIntent,omitempty"`
	Mode                 string                                 `json:"mode,omitempty"`
	ModeCode             string                                 `json:"modeCode,omitempty"`
	TargetProfileSummary string                                 `json:"targetProfileSummary,omitempty"`
	TargetProfile        *playbackprofile.TargetPlaybackProfile `json:"targetProfile,omitempty"`
	Reasons              []string                               `json:"reasons,omitempty"`
	BasisHash            string                                 `json:"basisHash,omitempty"`
	ChangedAt            *time.Time                             `json:"changedAt,omitempty"`
	LastSeenAt           *time.Time                             `json:"lastSeenAt,omitempty"`
}

type storageDecisionAuditRow struct {
	ServiceRef       string
	Origin           string
	ClientFamily     string
	ClientCapsSource string
	HostFingerprint  string
	RequestedIntent  string
	EffectiveIntent  string
	Mode             string
	TargetProfile    *playbackprofile.TargetPlaybackProfile
	Reasons          []string
	BasisHash        string
	ChangedAt        *time.Time
	LastSeenAt       *time.Time
}

func runStorageDecisionReport(args []string) int {
	fs := flag.NewFlagSet("xg2g storage decision-report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	defaultDataDir := strings.TrimSpace(config.ResolveDataDirFromEnv())
	defaultPlaylistName := strings.TrimSpace(config.ParseString("XG2G_PLAYLIST_FILENAME", "playlist.m3u8"))

	opts := storageDecisionReportOptions{}
	fs.StringVar(&opts.DataDir, "data-dir", defaultDataDir, "Path to the xg2g data directory")
	fs.StringVar(&opts.PlaylistName, "playlist", defaultPlaylistName, "Relative playlist filename inside data dir")
	fs.StringVar(&opts.Bouquet, "bouquet", "", "Bouquet/group filter (for example Premium)")
	fs.StringVar(&opts.ClientFamily, "client-family", "", "Filter current decisions by client family")
	fs.StringVar(&opts.Intent, "intent", "", "Filter current decisions by requested intent")
	fs.StringVar(&opts.Origin, "origin", "", "Filter current decisions by origin (runtime or sweep)")
	fs.StringVar(&opts.Format, "format", "table", "Output format: table or json")
	fs.StringVar(&opts.OutPath, "out", "", "Write report to a file instead of stdout")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, err := buildStorageDecisionReport(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	var data []byte
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "json":
		data, err = json.MarshalIndent(report, "", "  ")
		if err == nil {
			data = append(data, '\n')
		}
	case "", "table":
		var rendered strings.Builder
		renderStorageDecisionReportTable(&rendered, report)
		data = []byte(rendered.String())
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid format %q. Use 'table' or 'json'.\n", opts.Format)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: render report: %v\n", err)
		return 1
	}

	if opts.OutPath != "" {
		if err := os.WriteFile(opts.OutPath, data, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "Error: write report: %v\n", err)
			return 1
		}
		return 0
	}

	_, _ = os.Stdout.Write(data)
	return 0
}

func buildStorageDecisionReport(opts storageDecisionReportOptions) (storageDecisionReport, error) {
	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		return storageDecisionReport{}, fmt.Errorf("--data-dir is required (or set XG2G_DATA_DIR / XG2G_DATA)")
	}
	playlistName := strings.TrimSpace(opts.PlaylistName)
	if playlistName == "" {
		playlistName = "playlist.m3u8"
	}

	cfg := config.AppConfig{DataDir: dataDir}
	snap := config.Snapshot{Runtime: config.RuntimeSnapshot{PlaylistFilename: playlistName}}
	playlistPath, err := paths.ValidatePlaylistPath(dataDir, playlistName)
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("resolve playlist path: %w", err)
	}
	if _, err := os.Stat(playlistPath); err != nil {
		if os.IsNotExist(err) {
			return storageDecisionReport{}, fmt.Errorf("playlist not found: %s", playlistPath)
		}
		return storageDecisionReport{}, fmt.Errorf("stat playlist: %w", err)
	}
	servicesResult, err := read.GetServices(cfg, snap, nil, read.ServicesQuery{Bouquet: strings.TrimSpace(opts.Bouquet)})
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("load playlist services: %w", err)
	}

	scanDB, err := openOptionalReadOnlySQLite(resolveStorageDBPath(dataDir, "capabilities.sqlite"))
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("open capabilities store: %w", err)
	}
	if scanDB != nil {
		defer func() { _ = scanDB.Close() }()
	}
	capabilityColumns, err := loadSQLiteColumnSet(scanDB, "capabilities")
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("inspect capabilities schema: %w", err)
	}

	decisionDB, err := openOptionalReadOnlySQLite(resolveStorageDBPath(dataDir, "decision_audit.sqlite"))
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("open decision audit store: %w", err)
	}
	if decisionDB != nil {
		defer func() { _ = decisionDB.Close() }()
	}
	decisionColumns, err := loadSQLiteColumnSet(decisionDB, "decision_current")
	if err != nil {
		return storageDecisionReport{}, fmt.Errorf("inspect decision_current schema: %w", err)
	}

	report := storageDecisionReport{
		GeneratedAt: time.Now().UTC(),
		DataDir:     dataDir,
		Playlist:    playlistName,
		Bouquet:     strings.TrimSpace(opts.Bouquet),
		Filters: storageDecisionReportFilters{
			ClientFamily: normalize.Token(opts.ClientFamily),
			Intent:       string(playbackprofile.NormalizeRequestedIntent(opts.Intent)),
			Origin:       normalizeDecisionReportOrigin(opts.Origin),
			SubjectKind:  "live",
		},
		Rows: make([]storageDecisionReportRow, 0, len(servicesResult.Items)),
	}

	for _, service := range dedupeReportServices(servicesResult.Items) {
		serviceRef := normalize.ServiceRef(service.ServiceRef)
		capability, capabilityFound, err := queryCapability(scanDB, capabilityColumns, serviceRef)
		if err != nil {
			return storageDecisionReport{}, fmt.Errorf("query capability for %s: %w", serviceRef, err)
		}
		truthStatus := deriveTruthStatus(capabilityFound, capability)

		decisionRows, err := queryDecisionCurrentRows(decisionDB, decisionColumns, serviceRef, normalize.Token(opts.ClientFamily), string(playbackprofile.NormalizeRequestedIntent(opts.Intent)), normalizeDecisionReportOrigin(opts.Origin))
		if err != nil {
			return storageDecisionReport{}, fmt.Errorf("query current decisions for %s: %w", serviceRef, err)
		}
		if len(decisionRows) == 0 {
			report.Rows = append(report.Rows, buildStorageDecisionReportRow(service, capability, capabilityFound, truthStatus, nil))
			continue
		}
		for _, dec := range decisionRows {
			report.Rows = append(report.Rows, buildStorageDecisionReportRow(service, capability, capabilityFound, truthStatus, &dec))
		}
	}

	sort.SliceStable(report.Rows, func(i, j int) bool {
		if report.Rows[i].Bouquet != report.Rows[j].Bouquet {
			return report.Rows[i].Bouquet < report.Rows[j].Bouquet
		}
		if report.Rows[i].ChannelName != report.Rows[j].ChannelName {
			return report.Rows[i].ChannelName < report.Rows[j].ChannelName
		}
		if report.Rows[i].ClientFamily != report.Rows[j].ClientFamily {
			return report.Rows[i].ClientFamily < report.Rows[j].ClientFamily
		}
		if report.Rows[i].DecisionOrigin != report.Rows[j].DecisionOrigin {
			return report.Rows[i].DecisionOrigin < report.Rows[j].DecisionOrigin
		}
		return report.Rows[i].RequestedIntent < report.Rows[j].RequestedIntent
	})

	report.Summary = summarizeStorageDecisionReport(report.Rows)
	report.Warnings = buildStorageDecisionReportWarnings(report.Summary)
	return report, nil
}

func buildStorageDecisionReportRow(service read.Service, capability scan.Capability, capabilityFound bool, truthStatus string, decisionRow *storageDecisionAuditRow) storageDecisionReportRow {
	row := storageDecisionReportRow{
		ServiceRef:        normalize.ServiceRef(service.ServiceRef),
		ChannelName:       service.Name,
		Bouquet:           service.Group,
		TruthStatus:       truthStatus,
		DecisionPresent:   decisionRow != nil,
		ScanState:         string(capability.State),
		ScanFailureReason: capability.FailureReason,
		ScanContainer:     capability.Container,
		ScanVideoCodec:    capability.VideoCodec,
		ScanAudioCodec:    capability.AudioCodec,
		ScanResolution:    capability.Resolution,
	}
	if capabilityFound {
		row.TruthSource = deriveTruthSource(truthStatus, decisionRow != nil)
	} else {
		row.TruthSource = deriveTruthSource(reportTruthMissing, decisionRow != nil)
	}
	if decisionRow == nil {
		return row
	}

	row.ClientFamily = decisionRow.ClientFamily
	row.DecisionOrigin = decisionRow.Origin
	row.ClientCapsSourceCode = decisionRow.ClientCapsSource
	row.ClientCapsSource = presentClientCapsSource(decisionRow.ClientCapsSource)
	row.HostFingerprint = presentHostFingerprint(decisionRow.HostFingerprint)
	row.RequestedIntent = decisionRow.RequestedIntent
	row.EffectiveIntent = decisionRow.EffectiveIntent
	row.ModeCode = decisionRow.Mode
	row.Mode = presentDecisionMode(decisionRow.Mode)
	row.TargetProfile = decisionRow.TargetProfile
	row.TargetProfileSummary = summarizeTargetProfile(decisionRow.TargetProfile)
	row.Reasons = append([]string(nil), decisionRow.Reasons...)
	row.BasisHash = decisionRow.BasisHash
	row.ChangedAt = decisionRow.ChangedAt
	row.LastSeenAt = decisionRow.LastSeenAt
	return row
}

func summarizeStorageDecisionReport(rows []storageDecisionReportRow) storageDecisionReportSummary {
	summary := storageDecisionReportSummary{
		RowsTotal: len(rows),
	}
	servicesSeen := make(map[string]bool)
	servicesWithDecision := make(map[string]bool)
	serviceTruthCounted := make(map[string]bool)
	hostFingerprintsSeen := make(map[string]bool)
	basisHostPairs := make(map[string]bool)
	basisHosts := make(map[string]map[string]bool)

	for _, row := range rows {
		if !servicesSeen[row.ServiceRef] {
			servicesSeen[row.ServiceRef] = true
			summary.ServicesTotal++
		}
		if row.DecisionPresent {
			servicesWithDecision[row.ServiceRef] = true
			hostFingerprint := presentHostFingerprint(row.HostFingerprint)
			hostFingerprintsSeen[hostFingerprint] = true
			if hostFingerprint == reportUnknownHost {
				summary.UnknownHostRows++
			}
			if row.BasisHash != "" {
				basisHostPairs[row.BasisHash+"\x00"+hostFingerprint] = true
				if hostFingerprint != reportUnknownHost {
					if _, ok := basisHosts[row.BasisHash]; !ok {
						basisHosts[row.BasisHash] = make(map[string]bool)
					}
					basisHosts[row.BasisHash][hostFingerprint] = true
				}
			}
		}
		if !serviceTruthCounted[row.ServiceRef] {
			serviceTruthCounted[row.ServiceRef] = true
			switch row.TruthStatus {
			case reportTruthComplete:
				summary.TruthComplete++
			case reportTruthIncomplete:
				summary.TruthIncomplete++
			case reportTruthEventInactive:
				summary.TruthEventInactive++
			default:
				summary.TruthMissing++
			}
			switch row.TruthSource {
			case reportTruthSourceScan:
				summary.TruthSourceScan++
			case reportTruthSourceFallback:
				summary.TruthSourceFallback++
			case reportTruthSourceEventInactive:
				summary.TruthSourceEventInactive++
			default:
				summary.TruthSourceUnresolved++
			}
		}
	}

	summary.ServicesWithDecision = len(servicesWithDecision)
	summary.ServicesWithoutDecision = summary.ServicesTotal - summary.ServicesWithDecision
	summary.DistinctHostFingerprints = len(hostFingerprintsSeen)
	summary.DistinctBasisHostPairs = len(basisHostPairs)
	for _, hosts := range basisHosts {
		if len(hosts) > 1 {
			summary.BasisHashesWithMultiHost++
		}
	}
	return summary
}

func buildStorageDecisionReportWarnings(summary storageDecisionReportSummary) []string {
	warnings := make([]string, 0, 1)
	if summary.UnknownHostRows > 0 {
		warnings = append(warnings, fmt.Sprintf("%d decision row(s) are bucketed as %s; these rows predate decision_audit schema v4 or were written without host fingerprints", summary.UnknownHostRows, reportUnknownHost))
	}
	return warnings
}

func renderStorageDecisionReportTable(w io.Writer, report storageDecisionReport) {
	_, _ = fmt.Fprintf(w, "Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintf(w, "DataDir:   %s\n", report.DataDir)
	_, _ = fmt.Fprintf(w, "Playlist:  %s\n", report.Playlist)
	if report.Bouquet != "" {
		_, _ = fmt.Fprintf(w, "Bouquet:   %s\n", report.Bouquet)
	}
	if report.Filters.ClientFamily != "" || report.Filters.Intent != "" || report.Filters.Origin != "" {
		_, _ = fmt.Fprintf(w, "Filters:   client_family=%s intent=%s origin=%s subject_kind=%s\n",
			emptyDash(report.Filters.ClientFamily),
			emptyDash(report.Filters.Intent),
			emptyDash(report.Filters.Origin),
			report.Filters.SubjectKind,
		)
	}
	_, _ = fmt.Fprintf(w, "Summary:   services=%d rows=%d with_decision=%d without_decision=%d truth_complete=%d truth_incomplete=%d truth_missing=%d truth_event_inactive=%d host_fingerprints=%d basis_host_pairs=%d multi_host_basis=%d unknown_host_rows=%d\n",
		report.Summary.ServicesTotal,
		report.Summary.RowsTotal,
		report.Summary.ServicesWithDecision,
		report.Summary.ServicesWithoutDecision,
		report.Summary.TruthComplete,
		report.Summary.TruthIncomplete,
		report.Summary.TruthMissing,
		report.Summary.TruthEventInactive,
		report.Summary.DistinctHostFingerprints,
		report.Summary.DistinctBasisHostPairs,
		report.Summary.BasisHashesWithMultiHost,
		report.Summary.UnknownHostRows,
	)
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(w, "Warning:   %s\n", warning)
	}
	_, _ = fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SERVICE_REF\tCHANNEL\tBOUQUET\tTRUTH_SOURCE\tTRUTH_STATUS\tORIGIN\tCLIENT\tCAPS_SOURCE\tHOST_FINGERPRINT\tREQUESTED_INTENT\tEFFECTIVE_INTENT\tMODE\tTARGET_PROFILE\tREASONS\tCHANGED_AT")
	for _, row := range report.Rows {
		changedAt := ""
		if row.ChangedAt != nil {
			changedAt = row.ChangedAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.Bouquet,
			row.TruthSource,
			row.TruthStatus,
			emptyDash(row.DecisionOrigin),
			emptyDash(row.ClientFamily),
			emptyDash(row.ClientCapsSource),
			emptyDash(row.HostFingerprint),
			emptyDash(row.RequestedIntent),
			emptyDash(row.EffectiveIntent),
			emptyDash(row.Mode),
			emptyDash(row.TargetProfileSummary),
			emptyDash(strings.Join(row.Reasons, ",")),
			emptyDash(changedAt),
		)
	}
	_ = tw.Flush()
}

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

func presentHostFingerprint(value string) string {
	if strings.TrimSpace(value) == "" {
		return reportUnknownHost
	}
	return value
}

func normalizeDecisionReportOrigin(value string) string {
	switch normalize.Token(value) {
	case "":
		return ""
	case decisionaudit.OriginRuntime:
		return decisionaudit.OriginRuntime
	case decisionaudit.OriginSweep:
		return decisionaudit.OriginSweep
	default:
		return normalize.Token(value)
	}
}

func deriveTruthStatus(found bool, capability scan.Capability) string {
	if !found {
		return reportTruthMissing
	}
	if capability.IsInactiveEventFeed() {
		return reportTruthEventInactive
	}
	if capability.HasMediaTruth() {
		return reportTruthComplete
	}
	return reportTruthIncomplete
}

func deriveTruthSource(truthStatus string, decisionPresent bool) string {
	switch truthStatus {
	case reportTruthComplete:
		return reportTruthSourceScan
	case reportTruthEventInactive:
		return reportTruthSourceEventInactive
	case reportTruthIncomplete, reportTruthMissing:
		if decisionPresent {
			return reportTruthSourceFallback
		}
		return reportTruthSourceUnresolved
	default:
		return reportTruthSourceUnresolved
	}
}

func summarizeTargetProfile(targetProfile *playbackprofile.TargetPlaybackProfile) string {
	if targetProfile == nil {
		return ""
	}
	videoCodec := strings.TrimSpace(targetProfile.Video.Codec)
	audioCodec := strings.TrimSpace(targetProfile.Audio.Codec)
	switch {
	case targetProfile.Container != "" && videoCodec != "" && audioCodec != "":
		return fmt.Sprintf("%s/%s/%s", targetProfile.Container, videoCodec, audioCodec)
	case targetProfile.Container != "" && videoCodec != "":
		return fmt.Sprintf("%s/%s", targetProfile.Container, videoCodec)
	case targetProfile.Container != "":
		return targetProfile.Container
	default:
		return videoCodec
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func dedupeReportServices(services []read.Service) []read.Service {
	if len(services) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(services))
	out := make([]read.Service, 0, len(services))
	for _, service := range services {
		key := normalize.ServiceRef(service.ServiceRef)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(service.Group)) + "|" + strings.ToLower(strings.TrimSpace(service.Name))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, service)
	}
	return out
}
