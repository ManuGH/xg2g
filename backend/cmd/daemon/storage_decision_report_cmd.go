package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
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
