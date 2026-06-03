package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"golang.org/x/time/rate"
)

const defaultStorageDecisionSweepTimeout = 10 * time.Minute

var defaultStorageDecisionSweepPreferredChannels = []string{
	"13th Street",
	"Cartoon Network",
	"DAZN 1",
	"DMAX HD",
	"HISTORY Channel",
	"ATV HD",
}

var storageDecisionSweepExecutor = executeStorageDecisionSweep

func runStorageDecisionSweep(args []string) int {
	fs := flag.NewFlagSet("xg2g storage decision-sweep", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		printStorageDecisionSweepUsage(fs.Output())
	}

	defaultDataDir := strings.TrimSpace(config.ResolveDataDirFromEnv())
	defaultPlaylistName := strings.TrimSpace(config.ParseString("XG2G_PLAYLIST_FILENAME", "playlist.m3u8"))

	opts := storageDecisionSweepOptions{}
	fs.StringVar(&opts.ConfigPath, "config", "", "Path to config.yaml")
	fs.StringVar(&opts.DataDir, "data-dir", defaultDataDir, "Path to the xg2g data directory")
	fs.StringVar(&opts.PlaylistName, "playlist", defaultPlaylistName, "Relative playlist filename inside data dir")
	fs.StringVar(&opts.Bouquet, "bouquet", "", "Bouquet/group filter (for example Premium)")
	fs.StringVar(&opts.ChannelNamesCSV, "channel", "", "Comma-separated exact channel names to sweep")
	fs.StringVar(&opts.ServiceRefsCSV, "service-ref", "", "Comma-separated service refs to sweep")
	fs.StringVar(&opts.ClientFamiliesCSV, "client-family", "ios_safari_native,chromium_hlsjs", "Comma-separated built-in client families")
	fs.StringVar(&opts.RequestedProfile, "requested-profile", "quality", "Requested playback profile/intent")
	fs.StringVar(&opts.APIVersion, "api-version", "v3.1", "API version label recorded for decisions")
	fs.StringVar(&opts.SchemaType, "schema-type", "live", "Schema type for the decision engine")
	fs.IntVar(&opts.Limit, "limit", 0, "Maximum matched senders to sweep (0 = all)")
	fs.BoolVar(&opts.SkipScan, "skip-scan", false, "Skip probing and decide from existing capabilities.sqlite truth only")
	fs.StringVar(&opts.StatePath, "state-path", "", "Persist and diff against sweep snapshot JSON (default: data-dir/store/last_sweep.json)")
	fs.BoolVar(&opts.NoState, "no-state", false, "Disable persisted sweep snapshot diffing")
	fs.DurationVar(&opts.Timeout, "timeout", defaultStorageDecisionSweepTimeout, "Overall sweep timeout")
	fs.DurationVar(&opts.ProbeDelay, "probe-delay", 0, "Delay between probes")
	fs.StringVar(&opts.Format, "format", "table", "Output format: table or json")
	fs.StringVar(&opts.OutPath, "out", "", "Write report to a file instead of stdout")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	result, err := storageDecisionSweepExecutor(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	var data []byte
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "json":
		data, err = json.MarshalIndent(result, "", "  ")
		if err == nil {
			data = append(data, '\n')
		}
	case "", "table":
		var rendered strings.Builder
		renderStorageDecisionSweepTable(&rendered, result)
		data = []byte(rendered.String())
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid format %q. Use 'table' or 'json'.\n", opts.Format)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: render sweep: %v\n", err)
		return 1
	}

	if opts.OutPath != "" {
		if err := os.WriteFile(opts.OutPath, data, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "Error: write sweep: %v\n", err)
			return 1
		}
		if storageDecisionSweepHasRelevantDiff(result) {
			return 1
		}
		return 0
	}

	_, _ = os.Stdout.Write(data)
	if storageDecisionSweepHasRelevantDiff(result) {
		return 1
	}
	return 0
}

func printStorageDecisionSweepUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  xg2g storage decision-sweep [flags]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Flags:")
	_, _ = fmt.Fprintln(w, "  --config string            Path to config.yaml (defaults to data-dir/config.yaml when present)")
	_, _ = fmt.Fprintln(w, "  --data-dir string          Path to xg2g data dir (default: XG2G_DATA_DIR / XG2G_DATA)")
	_, _ = fmt.Fprintln(w, "  --playlist string          Relative playlist filename inside data dir")
	_, _ = fmt.Fprintln(w, "  --bouquet string           Bouquet/group filter (required unless --channel or --service-ref is set)")
	_, _ = fmt.Fprintln(w, "  --channel string           Comma-separated exact channel names to sweep")
	_, _ = fmt.Fprintln(w, "  --service-ref string       Comma-separated service refs to sweep")
	_, _ = fmt.Fprintln(w, "  --client-family string     Comma-separated SSOT client fixture families")
	_, _ = fmt.Fprintln(w, "  --requested-profile string Requested playback profile/intent (default: quality)")
	_, _ = fmt.Fprintln(w, "  --api-version string       API version label recorded for decisions (default: v3.1)")
	_, _ = fmt.Fprintln(w, "  --schema-type string       Schema type for the decision engine (default: live)")
	_, _ = fmt.Fprintln(w, "  --limit int                Maximum matched senders to sweep (0 = all)")
	_, _ = fmt.Fprintln(w, "  --skip-scan                Skip probing and decide from existing capabilities.sqlite truth only")
	_, _ = fmt.Fprintln(w, "  --state-path string        Persist and diff against sweep snapshot JSON")
	_, _ = fmt.Fprintln(w, "  --no-state                 Disable persisted sweep snapshot diffing")
	_, _ = fmt.Fprintln(w, "  --timeout duration         Overall sweep timeout (default: 10m)")
	_, _ = fmt.Fprintln(w, "  --probe-delay duration     Delay between probes (default: 0s)")
	_, _ = fmt.Fprintln(w, "  --format string            Output format: table (default) or json")
	_, _ = fmt.Fprintln(w, "  --out string               Write report to a file instead of stdout")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Notes:")
	_, _ = fmt.Fprintln(w, "  decision-sweep writes decision_audit.sqlite with origin 'sweep'.")
	_, _ = fmt.Fprintln(w, "  Without --skip-scan it also updates capabilities.sqlite.")
	_, _ = fmt.Fprintln(w, "  By default it also persists a scope-aware snapshot and reports mode/truth/coverage deltas.")
	_, _ = fmt.Fprintln(w, "  Exit code is 1 when a relevant diff is detected; 0 means no relevant diff.")
	_, _ = fmt.Fprintln(w, "  Persisted sweep decisions do not overwrite runtime current rows.")
	_, _ = fmt.Fprintln(w, "  Output shows direct_stream as 'remux' and runtime_plus_family as 'runtime+family'.")
}

func executeStorageDecisionSweep(opts storageDecisionSweepOptions) (storageDecisionSweep, error) {
	playlistName := strings.TrimSpace(opts.PlaylistName)
	if playlistName == "" {
		playlistName = "playlist.m3u8"
	}

	configPath := resolveStorageDecisionSweepConfigPath(strings.TrimSpace(opts.ConfigPath), strings.TrimSpace(opts.DataDir))
	cfg, err := config.NewLoader(configPath, "storage-decision-sweep").Load()
	if err != nil {
		if strings.TrimSpace(configPath) != "" {
			return storageDecisionSweep{}, fmt.Errorf("load config %s: %w", configPath, err)
		}
		return storageDecisionSweep{}, fmt.Errorf("load config from environment: %w", err)
	}

	dataDir := strings.TrimSpace(opts.DataDir)
	if dataDir == "" {
		dataDir = strings.TrimSpace(cfg.DataDir)
	}
	if dataDir == "" {
		return storageDecisionSweep{}, fmt.Errorf("--data-dir is required (or set XG2G_DATA_DIR / XG2G_DATA)")
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return storageDecisionSweep{}, fmt.Errorf("resolve data dir: %w", err)
	}

	playlistPath, err := paths.ValidatePlaylistPath(dataDir, playlistName)
	if err != nil {
		return storageDecisionSweep{}, fmt.Errorf("resolve playlist path: %w", err)
	}
	if _, err := os.Stat(playlistPath); err != nil {
		if os.IsNotExist(err) {
			return storageDecisionSweep{}, fmt.Errorf("playlist not found: %s", playlistPath)
		}
		return storageDecisionSweep{}, fmt.Errorf("stat playlist: %w", err)
	}

	selections, err := selectStorageDecisionSweepServices(playlistPath, opts)
	if err != nil {
		return storageDecisionSweep{}, err
	}
	if len(selections) == 0 {
		return storageDecisionSweep{}, fmt.Errorf("no senders matched the requested sweep scope")
	}

	capabilitiesDBPath := resolveStorageDBPath(dataDir, "capabilities.sqlite")
	decisionDBPath := resolveStorageDBPath(dataDir, "decision_audit.sqlite")
	for _, path := range []string{capabilitiesDBPath, decisionDBPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return storageDecisionSweep{}, fmt.Errorf("create store dir for %s: %w", path, err)
		}
	}

	applyStorageDecisionSweepConfigDefaults(&cfg, dataDir, capabilitiesDBPath)

	scanStore, err := scan.NewSqliteStore(capabilitiesDBPath)
	if err != nil {
		return storageDecisionSweep{}, fmt.Errorf("open scan store: %w", err)
	}
	defer func() { _ = scanStore.Close() }()

	profiles, err := selectStorageDecisionSweepClientProfiles(splitCSVString(opts.ClientFamiliesCSV))
	if err != nil {
		return storageDecisionSweep{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	truthSource := v3recordings.ChannelTruthSource(storageDecisionSweepScanStoreTruthSource{store: scanStore})
	if !opts.SkipScan {
		tempPlaylist, err := writeStorageDecisionSweepPlaylist(selections)
		if err != nil {
			return storageDecisionSweep{}, fmt.Errorf("write temp playlist: %w", err)
		}
		defer func() { _ = os.Remove(tempPlaylist) }()

		e2 := newStorageDecisionSweepEnigma2Client(cfg)

		manager := scan.NewManager(scanStore, tempPlaylist, e2)
		manager.AttachLifecycle(ctx)
		manager.ProbeDelay = opts.ProbeDelay
		if !manager.RunBackgroundForce() {
			return storageDecisionSweep{}, fmt.Errorf("forced scan did not start")
		}
		if err := waitForStorageDecisionSweepScan(ctx, manager); err != nil {
			return storageDecisionSweep{}, fmt.Errorf("wait for scan: %w", err)
		}
		truthSource = manager
	}

	auditStore, err := decisionaudit.NewSqliteAuditStore(decisionDBPath)
	if err != nil {
		return storageDecisionSweep{}, fmt.Errorf("open decision audit store: %w", err)
	}
	defer func() { _ = auditStore.DB.Close() }()

	service := v3recordings.NewService(storageDecisionSweepDeps{
		cfg:          cfg,
		truthSource:  truthSource,
		decisionSink: storageDecisionSweepOriginSink{inner: auditStore},
	})

	result := storageDecisionSweep{
		GeneratedAt:      time.Now().UTC(),
		ConfigPath:       strings.TrimSpace(configPath),
		DataDir:          dataDir,
		Playlist:         playlistName,
		Bouquet:          strings.TrimSpace(opts.Bouquet),
		RequestedProfile: strings.TrimSpace(opts.RequestedProfile),
		SkipScan:         opts.SkipScan,
		ClientFamilies:   storageDecisionSweepClientFamilyNames(profiles),
		ScannedServices:  collectStorageDecisionSweepScanRows(scanStore, selections),
		Decisions:        nil,
	}
	result.ScopeKey = computeStorageDecisionSweepScopeKey(opts, playlistName, result.ClientFamilies)
	result.Decisions = collectStorageDecisionSweepDecisions(ctx, service, scanStore, selections, profiles, opts)

	sortStorageDecisionSweepResult(&result)

	finalizeStorageDecisionSweepCoverage(&result)
	result.Summary = summarizeStorageDecisionSweep(result)
	if !opts.NoState {
		statePath := resolveStorageDecisionSweepStatePath(dataDir, strings.TrimSpace(opts.StatePath))
		result.StatePath = statePath
		previous, err := loadStorageDecisionSweepState(statePath)
		if err != nil {
			return storageDecisionSweep{}, fmt.Errorf("load sweep state: %w", err)
		}
		result.Diff = diffStorageDecisionSweep(previous, result, statePath)
		if err := persistStorageDecisionSweepState(statePath, result); err != nil {
			return storageDecisionSweep{}, fmt.Errorf("persist sweep state: %w", err)
		}
	}
	return result, nil
}

// applyStorageDecisionSweepConfigDefaults pins the loaded config to the sweep's
// data dir and capability store and fills in the ffmpeg/HLS defaults the sweep
// relies on, mutating cfg in place exactly as the inline setup did.
func applyStorageDecisionSweepConfigDefaults(cfg *config.AppConfig, dataDir, capabilitiesDBPath string) {
	cfg.DataDir = dataDir
	cfg.Store.Path = filepath.Dir(capabilitiesDBPath)
	cfg.Store.Backend = "sqlite"
	if strings.TrimSpace(cfg.FFmpeg.Bin) == "" {
		cfg.FFmpeg.Bin = "ffmpeg"
	}
	cfg.FFmpeg.FFprobeBin = config.ResolveFFprobeBin(cfg.FFmpeg.FFprobeBin, cfg.FFmpeg.Bin)
	if strings.TrimSpace(cfg.HLS.Root) == "" {
		cfg.HLS.Root = filepath.Join(dataDir, "hls")
	}
}

// newStorageDecisionSweepEnigma2Client builds the Enigma2 client used by the
// forced scan from the resolved config, preserving the exact option mapping.
func newStorageDecisionSweepEnigma2Client(cfg config.AppConfig) *enigma2.Client {
	return enigma2.NewClientWithOptions(cfg.Enigma2.BaseURL, enigma2.Options{
		Timeout:               cfg.Enigma2.Timeout,
		ResponseHeaderTimeout: cfg.Enigma2.ResponseHeaderTimeout,
		MaxRetries:            cfg.Enigma2.Retries,
		Backoff:               cfg.Enigma2.Backoff,
		MaxBackoff:            cfg.Enigma2.MaxBackoff,
		Username:              cfg.Enigma2.Username,
		Password:              cfg.Enigma2.Password,
		UserAgent:             cfg.Enigma2.UserAgent,
		RateLimit:             rate.Limit(cfg.Enigma2.RateLimit),
		RateLimitBurst:        cfg.Enigma2.RateBurst,
		UseWebIFStreams:       cfg.Enigma2.UseWebIFStreams,
		StreamPort:            cfg.Enigma2.StreamPort,
	})
}

// collectStorageDecisionSweepDecisions resolves a decision row for every usable
// selection/client-profile pair, preserving the original skip rules, error
// handling, append ordering, and pre-sized slice capacity.
func collectStorageDecisionSweepDecisions(
	ctx context.Context,
	service *v3recordings.Service,
	scanStore *scan.SqliteStore,
	selections []storageDecisionSweepSelection,
	profiles []storageDecisionSweepClientProfile,
	opts storageDecisionSweepOptions,
) []storageDecisionSweepDecision {
	decisions := make([]storageDecisionSweepDecision, 0, len(selections)*len(profiles))
	for _, selection := range selections {
		if ctx.Err() != nil {
			break
		}
		capability, found := scanStore.Get(selection.ServiceRef)
		truthStatus := deriveTruthStatus(found, capability)
		if !found || !capability.Usable() {
			continue
		}
		truthSource := deriveTruthSource(truthStatus, true)
		for _, profile := range profiles {
			row := storageDecisionSweepDecision{
				ServiceRef:           selection.ServiceRef,
				ChannelName:          selection.Name,
				Bouquet:              selection.Group,
				ClientFamily:         profile.Name,
				ClientCapsSourceCode: "",
				TruthStatus:          truthStatus,
				TruthSource:          truthSource,
			}
			res, playbackErr := service.ResolvePlaybackInfo(ctx, v3recordings.PlaybackInfoRequest{
				SubjectID:        selection.ServiceRef,
				SubjectKind:      v3recordings.PlaybackSubjectLive,
				APIVersion:       strings.TrimSpace(opts.APIVersion),
				SchemaType:       strings.TrimSpace(opts.SchemaType),
				RequestedProfile: strings.TrimSpace(opts.RequestedProfile),
				ClientProfile:    profile.Name,
				Capabilities:     cloneStorageDecisionSweepCaps(storageDecisionSweepFamilyCaps(profile.Name)),
			})
			if playbackErr != nil {
				row.Error = playbackErr.Error()
				decisions = append(decisions, row)
				continue
			}
			row.ClientCapsSourceCode = strings.TrimSpace(res.ResolvedCapabilities.ClientCapsSource)
			row.ClientCapsSource = presentClientCapsSource(row.ClientCapsSourceCode)
			row.ModeCode = string(res.Decision.Mode)
			row.Mode = presentDecisionMode(row.ModeCode)
			row.EffectiveIntent = strings.TrimSpace(res.Decision.Trace.ResolvedIntent)
			row.TargetProfileSummary = summarizeTargetProfile(res.Decision.TargetProfile)
			row.Reasons = storageDecisionSweepReasonStrings(res.Decision.Reasons)
			decisions = append(decisions, row)
		}
	}
	return decisions
}

// sortStorageDecisionSweepResult applies the deterministic ordering of scanned
// services and decisions, identical to the inline sort.SliceStable calls.
func sortStorageDecisionSweepResult(result *storageDecisionSweep) {
	sort.SliceStable(result.ScannedServices, func(i, j int) bool {
		if result.ScannedServices[i].Bouquet != result.ScannedServices[j].Bouquet {
			return result.ScannedServices[i].Bouquet < result.ScannedServices[j].Bouquet
		}
		if result.ScannedServices[i].ChannelName != result.ScannedServices[j].ChannelName {
			return result.ScannedServices[i].ChannelName < result.ScannedServices[j].ChannelName
		}
		return result.ScannedServices[i].ServiceRef < result.ScannedServices[j].ServiceRef
	})
	sort.SliceStable(result.Decisions, func(i, j int) bool {
		if result.Decisions[i].Bouquet != result.Decisions[j].Bouquet {
			return result.Decisions[i].Bouquet < result.Decisions[j].Bouquet
		}
		if result.Decisions[i].ChannelName != result.Decisions[j].ChannelName {
			return result.Decisions[i].ChannelName < result.Decisions[j].ChannelName
		}
		return result.Decisions[i].ClientFamily < result.Decisions[j].ClientFamily
	})
}

func storageDecisionSweepHasRelevantDiff(result storageDecisionSweep) bool {
	if result.Diff == nil {
		return false
	}
	if result.Diff.ScopeChanged {
		return false
	}
	if result.Diff.RelevantChanges > 0 {
		return true
	}
	return result.Diff.Coverage != nil && result.Diff.Coverage.Regression
}

func selectStorageDecisionSweepServices(playlistPath string, opts storageDecisionSweepOptions) ([]storageDecisionSweepSelection, error) {
	bouquet := strings.TrimSpace(opts.Bouquet)
	channelFilters := splitCSVString(opts.ChannelNamesCSV)
	serviceRefFilters := splitCSVString(opts.ServiceRefsCSV)
	if bouquet == "" && len(channelFilters) == 0 && len(serviceRefFilters) == 0 {
		return nil, fmt.Errorf("decision-sweep requires --bouquet, --channel, or --service-ref to scope the sweep")
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("--limit must be >= 0")
	}

	// #nosec G304 -- playlistPath is validated by ResolvePlaylistPath before it reaches the sweep selector.
	content, err := os.ReadFile(filepath.Clean(playlistPath))
	if err != nil {
		return nil, err
	}

	channelFilterSet := make(map[string]bool, len(channelFilters))
	for _, channelName := range channelFilters {
		channelFilterSet[strings.ToLower(strings.TrimSpace(channelName))] = true
	}
	serviceFilterSet := make(map[string]bool, len(serviceRefFilters))
	for _, serviceRef := range serviceRefFilters {
		serviceFilterSet[normalize.ServiceRef(serviceRef)] = true
	}

	allChannels := m3u.Parse(string(content))
	matched := make(map[string]storageDecisionSweepSelection)
	for _, channel := range allChannels {
		if bouquet != "" && strings.TrimSpace(channel.Group) != bouquet {
			continue
		}
		serviceRef := normalize.ServiceRef(scan.ExtractServiceRef(channel.URL))
		if serviceRef == "" {
			continue
		}
		if len(channelFilterSet) > 0 && !channelFilterSet[strings.ToLower(strings.TrimSpace(channel.Name))] {
			continue
		}
		if len(serviceFilterSet) > 0 && !serviceFilterSet[serviceRef] {
			continue
		}
		matched[serviceRef] = storageDecisionSweepSelection{
			Name:       channel.Name,
			Group:      channel.Group,
			ServiceRef: serviceRef,
			Raw:        channel.Raw,
			URL:        channel.URL,
		}
	}

	if len(matched) == 0 {
		return nil, nil
	}

	out := make([]storageDecisionSweepSelection, 0, len(matched))
	seen := make(map[string]bool, len(matched))
	if len(channelFilterSet) == 0 && len(serviceFilterSet) == 0 {
		for _, name := range defaultStorageDecisionSweepPreferredChannels {
			for _, selection := range matched {
				if selection.Name != name || seen[selection.ServiceRef] {
					continue
				}
				seen[selection.ServiceRef] = true
				out = append(out, selection)
			}
		}
	}

	remainingRefs := make([]string, 0, len(matched))
	for ref := range matched {
		if seen[ref] {
			continue
		}
		remainingRefs = append(remainingRefs, ref)
	}
	sort.SliceStable(remainingRefs, func(i, j int) bool {
		left := matched[remainingRefs[i]]
		right := matched[remainingRefs[j]]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ServiceRef < right.ServiceRef
	})
	for _, ref := range remainingRefs {
		out = append(out, matched[ref])
	}

	if opts.Limit > 0 && len(out) > opts.Limit {
		out = append([]storageDecisionSweepSelection(nil), out[:opts.Limit]...)
	}
	return out, nil
}

func writeStorageDecisionSweepPlaylist(selections []storageDecisionSweepSelection) (string, error) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, selection := range selections {
		b.WriteString(selection.Raw)
		b.WriteString("\n")
		b.WriteString(selection.URL)
		b.WriteString("\n")
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("xg2g-decision-sweep-%d.m3u8", time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		return "", err
	}
	return path, nil
}

func waitForStorageDecisionSweepScan(ctx context.Context, manager *scan.Manager) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		status := manager.GetStatus()
		switch status.State {
		case "complete":
			return nil
		case "failed", "cancelled":
			return fmt.Errorf("%s: %s", status.State, status.LastError)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func collectStorageDecisionSweepScanRows(store *scan.SqliteStore, selections []storageDecisionSweepSelection) []storageDecisionSweepScanRow {
	rows := make([]storageDecisionSweepScanRow, 0, len(selections))
	for _, selection := range selections {
		capability, found := store.Get(selection.ServiceRef)
		truthStatus := deriveTruthStatus(found, capability)
		rows = append(rows, storageDecisionSweepScanRow{
			ServiceRef:    selection.ServiceRef,
			ChannelName:   selection.Name,
			Bouquet:       selection.Group,
			TruthStatus:   truthStatus,
			TruthSource:   deriveTruthSource(truthStatus, false),
			ScanState:     string(capability.State),
			FailureReason: capability.FailureReason,
			Container:     capability.Container,
			VideoCodec:    capability.VideoCodec,
			AudioCodec:    capability.AudioCodec,
			Resolution:    capability.Resolution,
		})
	}
	return rows
}

func finalizeStorageDecisionSweepCoverage(result *storageDecisionSweep) {
	if result == nil {
		return
	}
	successByService := make(map[string]bool, len(result.Decisions))
	for _, row := range result.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			continue
		}
		successByService[row.ServiceRef] = true
	}
	for i := range result.ScannedServices {
		row := &result.ScannedServices[i]
		row.TruthSource = deriveTruthSource(row.TruthStatus, successByService[row.ServiceRef])
	}
}

func summarizeStorageDecisionSweep(result storageDecisionSweep) storageDecisionSweepSummary {
	summary := storageDecisionSweepSummary{
		ServicesSelected: len(result.ScannedServices),
		DecisionRows:     len(result.Decisions),
	}
	servicesWithDecision := make(map[string]bool)
	for _, row := range result.ScannedServices {
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
	for _, row := range result.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			summary.DecisionErrors++
			continue
		}
		servicesWithDecision[row.ServiceRef] = true
	}
	summary.ServicesWithDecision = len(servicesWithDecision)
	return summary
}
