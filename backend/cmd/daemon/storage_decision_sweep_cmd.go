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
	"text/tabwriter"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	domainrecordings "github.com/ManuGH/xg2g/internal/control/recordings"
	recordingcaps "github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	decisionaudit "github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
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

type storageDecisionSweepOptions struct {
	ConfigPath        string
	DataDir           string
	PlaylistName      string
	Bouquet           string
	ChannelNamesCSV   string
	ServiceRefsCSV    string
	ClientFamiliesCSV string
	RequestedProfile  string
	APIVersion        string
	SchemaType        string
	Limit             int
	SkipScan          bool
	StatePath         string
	NoState           bool
	Timeout           time.Duration
	ProbeDelay        time.Duration
	Format            string
	OutPath           string
}

type storageDecisionSweep struct {
	GeneratedAt      time.Time                      `json:"generatedAt"`
	ConfigPath       string                         `json:"configPath,omitempty"`
	DataDir          string                         `json:"dataDir"`
	Playlist         string                         `json:"playlist"`
	Bouquet          string                         `json:"bouquet,omitempty"`
	RequestedProfile string                         `json:"requestedProfile"`
	SkipScan         bool                           `json:"skipScan,omitempty"`
	StatePath        string                         `json:"statePath,omitempty"`
	ScopeKey         string                         `json:"scopeKey,omitempty"`
	ClientFamilies   []string                       `json:"clientFamilies"`
	Summary          storageDecisionSweepSummary    `json:"summary"`
	Diff             *storageDecisionSweepDiff      `json:"diff,omitempty"`
	ScannedServices  []storageDecisionSweepScanRow  `json:"scannedServices"`
	Decisions        []storageDecisionSweepDecision `json:"decisions"`
}

type storageDecisionSweepSummary struct {
	ServicesSelected         int `json:"servicesSelected"`
	TruthComplete            int `json:"truthComplete"`
	TruthIncomplete          int `json:"truthIncomplete"`
	TruthMissing             int `json:"truthMissing"`
	TruthEventInactive       int `json:"truthEventInactive"`
	ServicesWithDecision     int `json:"servicesWithDecision"`
	DecisionRows             int `json:"decisionRows"`
	DecisionErrors           int `json:"decisionErrors"`
	TruthSourceScan          int `json:"truthSourceScan"`
	TruthSourceFallback      int `json:"truthSourceFallback"`
	TruthSourceUnresolved    int `json:"truthSourceUnresolved"`
	TruthSourceEventInactive int `json:"truthSourceEventInactive"`
}

type storageDecisionSweepScanRow struct {
	ServiceRef    string `json:"serviceRef"`
	ChannelName   string `json:"channelName"`
	Bouquet       string `json:"bouquet"`
	TruthStatus   string `json:"truthStatus"`
	TruthSource   string `json:"truthSource"`
	ScanState     string `json:"scanState,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
	Container     string `json:"container,omitempty"`
	VideoCodec    string `json:"videoCodec,omitempty"`
	AudioCodec    string `json:"audioCodec,omitempty"`
	Resolution    string `json:"resolution,omitempty"`
}

type storageDecisionSweepDecision struct {
	ServiceRef           string   `json:"serviceRef"`
	ChannelName          string   `json:"channelName"`
	Bouquet              string   `json:"bouquet"`
	ClientFamily         string   `json:"clientFamily"`
	ClientCapsSource     string   `json:"clientCapsSource,omitempty"`
	ClientCapsSourceCode string   `json:"clientCapsSourceCode,omitempty"`
	TruthStatus          string   `json:"truthStatus"`
	TruthSource          string   `json:"truthSource"`
	Mode                 string   `json:"mode,omitempty"`
	ModeCode             string   `json:"modeCode,omitempty"`
	EffectiveIntent      string   `json:"effectiveIntent,omitempty"`
	TargetProfileSummary string   `json:"targetProfileSummary,omitempty"`
	Reasons              []string `json:"reasons,omitempty"`
	Error                string   `json:"error,omitempty"`
}

type storageDecisionSweepDiff struct {
	StatePath           string                              `json:"statePath,omitempty"`
	BaselineFound       bool                                `json:"baselineFound"`
	BaselineGeneratedAt *time.Time                          `json:"baselineGeneratedAt,omitempty"`
	ScopeChanged        bool                                `json:"scopeChanged,omitempty"`
	RelevantChanges     int                                 `json:"relevantChanges"`
	ModeChanges         []storageDecisionSweepModeChange    `json:"modeChanges,omitempty"`
	TruthChanges        []storageDecisionSweepTruthChange   `json:"truthChanges,omitempty"`
	Coverage            *storageDecisionSweepCoverageChange `json:"coverage,omitempty"`
}

type storageDecisionSweepModeChange struct {
	ServiceRef   string `json:"serviceRef"`
	ChannelName  string `json:"channelName"`
	ClientFamily string `json:"clientFamily"`
	FromMode     string `json:"fromMode"`
	ToMode       string `json:"toMode"`
}

type storageDecisionSweepTruthChange struct {
	ServiceRef      string `json:"serviceRef"`
	ChannelName     string `json:"channelName"`
	FromTruth       string `json:"fromTruth"`
	ToTruth         string `json:"toTruth"`
	FromTruthStatus string `json:"fromTruthStatus,omitempty"`
	ToTruthStatus   string `json:"toTruthStatus,omitempty"`
}

type storageDecisionSweepCoverageChange struct {
	FallbackBefore   int                               `json:"fallbackBefore"`
	FallbackAfter    int                               `json:"fallbackAfter"`
	FallbackDelta    int                               `json:"fallbackDelta"`
	UnresolvedBefore int                               `json:"unresolvedBefore"`
	UnresolvedAfter  int                               `json:"unresolvedAfter"`
	UnresolvedDelta  int                               `json:"unresolvedDelta"`
	Regression       bool                              `json:"regression"`
	NewFallback      []storageDecisionSweepServiceNote `json:"newFallback,omitempty"`
	NewUnresolved    []storageDecisionSweepServiceNote `json:"newUnresolved,omitempty"`
}

type storageDecisionSweepServiceNote struct {
	ServiceRef  string `json:"serviceRef"`
	ChannelName string `json:"channelName"`
}

type storageDecisionSweepClientProfile struct {
	Name string
}

type storageDecisionSweepSelection struct {
	Name       string
	Group      string
	ServiceRef string
	Raw        string
	URL        string
}

type storageDecisionSweepRecordingsService struct{}

func (storageDecisionSweepRecordingsService) ResolvePlayback(context.Context, string, string) (domainrecordings.PlaybackResolution, error) {
	return domainrecordings.PlaybackResolution{}, fmt.Errorf("recording playback is not used for live decision sweeps")
}

func (storageDecisionSweepRecordingsService) GetMediaTruth(context.Context, string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, fmt.Errorf("recording truth is not used for live decision sweeps")
}

type storageDecisionSweepDeps struct {
	cfg          config.AppConfig
	truthSource  v3recordings.ChannelTruthSource
	decisionSink v3recordings.DecisionAuditSink
}

type storageDecisionSweepScanStoreTruthSource struct {
	store *scan.SqliteStore
}

func (s storageDecisionSweepScanStoreTruthSource) GetCapability(serviceRef string) (scan.Capability, bool) {
	if s.store == nil {
		return scan.Capability{}, false
	}
	return s.store.Get(serviceRef)
}

type storageDecisionSweepOriginSink struct {
	inner v3recordings.DecisionAuditSink
}

func (s storageDecisionSweepOriginSink) Record(ctx context.Context, event decisionaudit.Event) error {
	if s.inner == nil {
		return nil
	}
	event.Origin = decisionaudit.OriginSweep
	return s.inner.Record(ctx, event)
}

func (d storageDecisionSweepDeps) RecordingsService() v3recordings.RecordingsService {
	return storageDecisionSweepRecordingsService{}
}

func (d storageDecisionSweepDeps) ChannelTruthSource() v3recordings.ChannelTruthSource {
	return d.truthSource
}

func (d storageDecisionSweepDeps) DecisionAuditSink() v3recordings.DecisionAuditSink {
	return d.decisionSink
}

func (d storageDecisionSweepDeps) Config() config.AppConfig {
	return d.cfg
}

func (d storageDecisionSweepDeps) HostPressure(context.Context) playbackprofile.HostPressureAssessment {
	return playbackprofile.HostPressureAssessment{}
}

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

		e2 := enigma2.NewClientWithOptions(cfg.Enigma2.BaseURL, enigma2.Options{
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
		Decisions:        make([]storageDecisionSweepDecision, 0, len(selections)*len(profiles)),
	}
	result.ScopeKey = computeStorageDecisionSweepScopeKey(opts, playlistName, result.ClientFamilies)

	for _, selection := range selections {
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
				result.Decisions = append(result.Decisions, row)
				continue
			}
			row.ClientCapsSourceCode = strings.TrimSpace(res.ResolvedCapabilities.ClientCapsSource)
			row.ClientCapsSource = presentClientCapsSource(row.ClientCapsSourceCode)
			row.ModeCode = string(res.Decision.Mode)
			row.Mode = presentDecisionMode(row.ModeCode)
			row.EffectiveIntent = strings.TrimSpace(res.Decision.Trace.ResolvedIntent)
			row.TargetProfileSummary = summarizeTargetProfile(res.Decision.TargetProfile)
			row.Reasons = storageDecisionSweepReasonStrings(res.Decision.Reasons)
			result.Decisions = append(result.Decisions, row)
		}
	}

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

func resolveStorageDecisionSweepConfigPath(explicit string, dataDir string) string {
	if path := strings.TrimSpace(explicit); path != "" {
		return path
	}
	if dir := strings.TrimSpace(dataDir); dir != "" {
		candidate := filepath.Join(dir, "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return resolveDefaultConfigPath()
}

func resolveStorageDecisionSweepStatePath(dataDir string, explicit string) string {
	if path := strings.TrimSpace(explicit); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(resolveStorageDBPath(dataDir, "decision_audit.sqlite")), "last_sweep.json")
}

func computeStorageDecisionSweepScopeKey(opts storageDecisionSweepOptions, playlistName string, clientFamilies []string) string {
	channelFilter := append([]string(nil), splitCSVString(opts.ChannelNamesCSV)...)
	serviceRefFilter := append([]string(nil), splitCSVString(opts.ServiceRefsCSV)...)
	sort.Strings(channelFilter)
	sort.Strings(serviceRefFilter)
	clients := append([]string(nil), clientFamilies...)
	sort.Strings(clients)
	return strings.Join([]string{
		"playlist=" + strings.TrimSpace(playlistName),
		"bouquet=" + strings.TrimSpace(opts.Bouquet),
		"channels=" + strings.Join(channelFilter, ","),
		"service_refs=" + strings.Join(serviceRefFilter, ","),
		"requested_profile=" + strings.TrimSpace(opts.RequestedProfile),
		"api_version=" + strings.TrimSpace(opts.APIVersion),
		"schema_type=" + strings.TrimSpace(opts.SchemaType),
		"skip_scan=" + fmt.Sprintf("%t", opts.SkipScan),
		"limit=" + fmt.Sprintf("%d", opts.Limit),
		"clients=" + strings.Join(clients, ","),
	}, "|")
}

func loadStorageDecisionSweepState(path string) (*storageDecisionSweep, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	// #nosec G304 -- state path is resolved from controlled dataDir/CLI input and file reads are the purpose of this command.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var snapshot storageDecisionSweep
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func persistStorageDecisionSweepState(path string, result storageDecisionSweep) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	snapshot := result
	snapshot.StatePath = ""
	snapshot.Diff = nil

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func diffStorageDecisionSweep(previous *storageDecisionSweep, current storageDecisionSweep, statePath string) *storageDecisionSweepDiff {
	diff := &storageDecisionSweepDiff{
		StatePath: statePath,
	}
	if previous == nil {
		return diff
	}
	diff.BaselineFound = true
	if !previous.GeneratedAt.IsZero() {
		ts := previous.GeneratedAt.UTC()
		diff.BaselineGeneratedAt = &ts
	}
	if strings.TrimSpace(previous.ScopeKey) == "" || previous.ScopeKey != current.ScopeKey {
		diff.ScopeChanged = true
		return diff
	}

	diff.ModeChanges = collectStorageDecisionSweepModeChanges(*previous, current)
	diff.TruthChanges = collectStorageDecisionSweepTruthChanges(*previous, current)
	diff.Coverage = collectStorageDecisionSweepCoverageChange(*previous, current)
	diff.RelevantChanges = len(diff.ModeChanges) + len(diff.TruthChanges)
	if diff.Coverage != nil && diff.Coverage.Regression {
		diff.RelevantChanges++
	}
	if len(diff.ModeChanges) == 0 {
		diff.ModeChanges = nil
	}
	if len(diff.TruthChanges) == 0 {
		diff.TruthChanges = nil
	}
	if diff.Coverage != nil && !diff.Coverage.Regression && len(diff.Coverage.NewFallback) == 0 && len(diff.Coverage.NewUnresolved) == 0 {
		diff.Coverage = nil
	}
	return diff
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

func collectStorageDecisionSweepModeChanges(previous storageDecisionSweep, current storageDecisionSweep) []storageDecisionSweepModeChange {
	type decisionKey struct {
		ServiceRef   string
		ClientFamily string
	}
	previousRows := make(map[decisionKey]storageDecisionSweepDecision)
	for _, row := range previous.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			continue
		}
		previousRows[decisionKey{ServiceRef: row.ServiceRef, ClientFamily: row.ClientFamily}] = row
	}

	out := make([]storageDecisionSweepModeChange, 0)
	for _, row := range current.Decisions {
		if strings.TrimSpace(row.Error) != "" {
			continue
		}
		prev, ok := previousRows[decisionKey{ServiceRef: row.ServiceRef, ClientFamily: row.ClientFamily}]
		if !ok {
			continue
		}
		fromMode := storageDecisionSweepModeCode(prev)
		toMode := storageDecisionSweepModeCode(row)
		if !isRelevantStorageDecisionSweepModeShift(fromMode, toMode) {
			continue
		}
		out = append(out, storageDecisionSweepModeChange{
			ServiceRef:   row.ServiceRef,
			ChannelName:  firstNonEmptySweep(row.ChannelName, prev.ChannelName),
			ClientFamily: row.ClientFamily,
			FromMode:     presentDecisionMode(fromMode),
			ToMode:       presentDecisionMode(toMode),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ChannelName != out[j].ChannelName {
			return out[i].ChannelName < out[j].ChannelName
		}
		return out[i].ClientFamily < out[j].ClientFamily
	})
	return out
}

func collectStorageDecisionSweepTruthChanges(previous storageDecisionSweep, current storageDecisionSweep) []storageDecisionSweepTruthChange {
	previousRows := make(map[string]storageDecisionSweepScanRow, len(previous.ScannedServices))
	for _, row := range previous.ScannedServices {
		previousRows[row.ServiceRef] = row
	}

	out := make([]storageDecisionSweepTruthChange, 0)
	for _, row := range current.ScannedServices {
		prev, ok := previousRows[row.ServiceRef]
		if !ok {
			continue
		}
		if storageDecisionSweepTruthFingerprint(prev) == storageDecisionSweepTruthFingerprint(row) {
			continue
		}
		out = append(out, storageDecisionSweepTruthChange{
			ServiceRef:      row.ServiceRef,
			ChannelName:     firstNonEmptySweep(row.ChannelName, prev.ChannelName),
			FromTruth:       storageDecisionSweepTruthDisplay(prev),
			ToTruth:         storageDecisionSweepTruthDisplay(row),
			FromTruthStatus: prev.TruthStatus,
			ToTruthStatus:   row.TruthStatus,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ChannelName < out[j].ChannelName
	})
	return out
}

func collectStorageDecisionSweepCoverageChange(previous storageDecisionSweep, current storageDecisionSweep) *storageDecisionSweepCoverageChange {
	change := &storageDecisionSweepCoverageChange{
		FallbackBefore:   previous.Summary.TruthSourceFallback,
		FallbackAfter:    current.Summary.TruthSourceFallback,
		FallbackDelta:    current.Summary.TruthSourceFallback - previous.Summary.TruthSourceFallback,
		UnresolvedBefore: previous.Summary.TruthSourceUnresolved,
		UnresolvedAfter:  current.Summary.TruthSourceUnresolved,
		UnresolvedDelta:  current.Summary.TruthSourceUnresolved - previous.Summary.TruthSourceUnresolved,
	}
	previousFallback := storageDecisionSweepServicesByTruthSource(previous.ScannedServices, reportTruthSourceFallback)
	currentFallback := storageDecisionSweepServicesByTruthSource(current.ScannedServices, reportTruthSourceFallback)
	previousUnresolved := storageDecisionSweepServicesByTruthSource(previous.ScannedServices, reportTruthSourceUnresolved)
	currentUnresolved := storageDecisionSweepServicesByTruthSource(current.ScannedServices, reportTruthSourceUnresolved)
	change.NewFallback = storageDecisionSweepNewServices(previousFallback, currentFallback)
	change.NewUnresolved = storageDecisionSweepNewServices(previousUnresolved, currentUnresolved)
	change.Regression = change.FallbackDelta > 0 || change.UnresolvedDelta > 0
	return change
}

func storageDecisionSweepServicesByTruthSource(rows []storageDecisionSweepScanRow, truthSource string) map[string]storageDecisionSweepServiceNote {
	out := make(map[string]storageDecisionSweepServiceNote)
	for _, row := range rows {
		if row.TruthSource != truthSource {
			continue
		}
		out[row.ServiceRef] = storageDecisionSweepServiceNote{
			ServiceRef:  row.ServiceRef,
			ChannelName: row.ChannelName,
		}
	}
	return out
}

func storageDecisionSweepNewServices(previous map[string]storageDecisionSweepServiceNote, current map[string]storageDecisionSweepServiceNote) []storageDecisionSweepServiceNote {
	out := make([]storageDecisionSweepServiceNote, 0)
	for serviceRef, row := range current {
		if _, ok := previous[serviceRef]; ok {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ChannelName < out[j].ChannelName
	})
	return out
}

func storageDecisionSweepModeCode(row storageDecisionSweepDecision) string {
	if code := normalize.Token(row.ModeCode); code != "" {
		return code
	}
	switch normalize.Token(row.Mode) {
	case "remux":
		return string(decisionaudit.ModeDirectStream)
	default:
		return normalize.Token(row.Mode)
	}
}

func isRelevantStorageDecisionSweepModeShift(fromMode string, toMode string) bool {
	return fromMode != toMode &&
		((fromMode == string(decisionaudit.ModeDirectPlay) || fromMode == string(decisionaudit.ModeTranscode)) &&
			(toMode == string(decisionaudit.ModeDirectPlay) || toMode == string(decisionaudit.ModeTranscode)))
}

func storageDecisionSweepTruthFingerprint(row storageDecisionSweepScanRow) string {
	return strings.Join([]string{
		row.TruthStatus,
		normalize.Token(row.Container),
		normalize.Token(row.VideoCodec),
		normalize.Token(row.AudioCodec),
	}, "|")
}

func storageDecisionSweepTruthDisplay(row storageDecisionSweepScanRow) string {
	parts := []string{row.Container, row.VideoCodec, row.AudioCodec}
	return emptyDash(strings.Trim(strings.Join(parts, "/"), "/"))
}

func firstNonEmptySweep(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func renderStorageDecisionSweepTable(w io.Writer, result storageDecisionSweep) {
	_, _ = fmt.Fprintf(w, "Generated: %s\n", result.GeneratedAt.Format(time.RFC3339))
	if strings.TrimSpace(result.ConfigPath) != "" {
		_, _ = fmt.Fprintf(w, "Config:    %s\n", result.ConfigPath)
	}
	_, _ = fmt.Fprintf(w, "DataDir:   %s\n", result.DataDir)
	_, _ = fmt.Fprintf(w, "Playlist:  %s\n", result.Playlist)
	if result.SkipScan {
		_, _ = fmt.Fprintln(w, "ScanMode:  skip-scan")
	}
	if strings.TrimSpace(result.StatePath) != "" {
		_, _ = fmt.Fprintf(w, "State:     %s\n", result.StatePath)
	}
	if result.Bouquet != "" {
		_, _ = fmt.Fprintf(w, "Bouquet:   %s\n", result.Bouquet)
	}
	_, _ = fmt.Fprintf(w, "Clients:   %s\n", strings.Join(result.ClientFamilies, ","))
	_, _ = fmt.Fprintf(w, "Summary:   selected=%d truth_complete=%d truth_incomplete=%d truth_missing=%d truth_event_inactive=%d fallback=%d unresolved=%d services_with_decision=%d decision_rows=%d decision_errors=%d\n",
		result.Summary.ServicesSelected,
		result.Summary.TruthComplete,
		result.Summary.TruthIncomplete,
		result.Summary.TruthMissing,
		result.Summary.TruthEventInactive,
		result.Summary.TruthSourceFallback,
		result.Summary.TruthSourceUnresolved,
		result.Summary.ServicesWithDecision,
		result.Summary.DecisionRows,
		result.Summary.DecisionErrors,
	)
	if result.Diff != nil {
		switch {
		case !result.Diff.BaselineFound:
			_, _ = fmt.Fprintln(w, "Diff:      first run (no baseline)")
		case result.Diff.ScopeChanged:
			_, _ = fmt.Fprintln(w, "Diff:      baseline reset (scope changed)")
		default:
			coverageRegression := "no"
			if result.Diff.Coverage != nil && result.Diff.Coverage.Regression {
				coverageRegression = "yes"
			}
			_, _ = fmt.Fprintf(w, "Diff:      relevant=%d mode_changes=%d truth_changes=%d coverage_regression=%s\n",
				result.Diff.RelevantChanges,
				len(result.Diff.ModeChanges),
				len(result.Diff.TruthChanges),
				coverageRegression,
			)
		}
	}
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintln(w, "Scanned Services:")
	scanWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(scanWriter, "SERVICE_REF\tCHANNEL\tBOUQUET\tTRUTH_STATUS\tTRUTH_SOURCE\tSCAN_STATE\tTRUTH\tFAILURE")
	for _, row := range result.ScannedServices {
		truth := emptyDash(strings.Trim(strings.Join([]string{row.Container, row.VideoCodec, row.AudioCodec}, "/"), "/"))
		_, _ = fmt.Fprintf(scanWriter, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.Bouquet,
			row.TruthStatus,
			row.TruthSource,
			emptyDash(row.ScanState),
			truth,
			emptyDash(row.FailureReason),
		)
	}
	_ = scanWriter.Flush()

	if len(result.Decisions) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Decisions:")
	decisionWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(decisionWriter, "SERVICE_REF\tCHANNEL\tCLIENT\tCAPS_SOURCE\tTRUTH_SOURCE\tEFFECTIVE_INTENT\tMODE\tTARGET_PROFILE\tREASONS\tERROR")
	for _, row := range result.Decisions {
		_, _ = fmt.Fprintf(decisionWriter, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ServiceRef,
			row.ChannelName,
			row.ClientFamily,
			emptyDash(row.ClientCapsSource),
			row.TruthSource,
			emptyDash(row.EffectiveIntent),
			emptyDash(row.Mode),
			emptyDash(row.TargetProfileSummary),
			emptyDash(strings.Join(row.Reasons, ",")),
			emptyDash(row.Error),
		)
	}
	_ = decisionWriter.Flush()

	if result.Diff == nil || !result.Diff.BaselineFound || result.Diff.ScopeChanged {
		return
	}
	if len(result.Diff.ModeChanges) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Mode Changes:")
		modeWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(modeWriter, "CHANNEL\tCLIENT\tFROM\tTO")
		for _, row := range result.Diff.ModeChanges {
			_, _ = fmt.Fprintf(modeWriter, "%s\t%s\t%s\t%s\n", row.ChannelName, row.ClientFamily, row.FromMode, row.ToMode)
		}
		_ = modeWriter.Flush()
	}
	if len(result.Diff.TruthChanges) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Truth Changes:")
		truthWriter := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(truthWriter, "CHANNEL\tFROM\tTO\tSTATUS")
		for _, row := range result.Diff.TruthChanges {
			_, _ = fmt.Fprintf(truthWriter, "%s\t%s\t%s\t%s -> %s\n",
				row.ChannelName,
				row.FromTruth,
				row.ToTruth,
				emptyDash(row.FromTruthStatus),
				emptyDash(row.ToTruthStatus),
			)
		}
		_ = truthWriter.Flush()
	}
	if result.Diff.Coverage != nil && result.Diff.Coverage.Regression {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintf(w, "Coverage Regression: fallback %d -> %d, unresolved %d -> %d\n",
			result.Diff.Coverage.FallbackBefore,
			result.Diff.Coverage.FallbackAfter,
			result.Diff.Coverage.UnresolvedBefore,
			result.Diff.Coverage.UnresolvedAfter,
		)
		if len(result.Diff.Coverage.NewFallback) > 0 {
			_, _ = fmt.Fprintf(w, "New fallback: %s\n", storageDecisionSweepServiceNames(result.Diff.Coverage.NewFallback))
		}
		if len(result.Diff.Coverage.NewUnresolved) > 0 {
			_, _ = fmt.Fprintf(w, "New unresolved: %s\n", storageDecisionSweepServiceNames(result.Diff.Coverage.NewUnresolved))
		}
	}
}

func selectStorageDecisionSweepClientProfiles(requested []string) ([]storageDecisionSweepClientProfile, error) {
	available := make(map[string]storageDecisionSweepClientProfile, len(playbackprofile.ClientFixtureIDs()))
	for _, family := range playbackprofile.ClientFixtureIDs() {
		available[family] = storageDecisionSweepClientProfile{Name: family}
	}
	if len(requested) == 0 {
		requested = []string{"ios_safari_native", "chromium_hlsjs"}
	}

	out := make([]storageDecisionSweepClientProfile, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, raw := range requested {
		name := normalize.Token(raw)
		if name == "" || seen[name] {
			continue
		}
		profile, ok := available[name]
		if !ok {
			known := make([]string, 0, len(available))
			for key := range available {
				known = append(known, key)
			}
			sort.Strings(known)
			return nil, fmt.Errorf("unsupported client family %q (known: %s)", raw, strings.Join(known, ", "))
		}
		seen[name] = true
		out = append(out, profile)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid client families selected")
	}
	return out, nil
}

func storageDecisionSweepFamilyCaps(family string) recordingcaps.PlaybackCapabilities {
	return recordingcaps.ResolveRuntimeProbeCapabilities(recordingcaps.PlaybackCapabilities{
		CapabilitiesVersion:  2,
		ClientFamilyFallback: family,
	})
}

func cloneStorageDecisionSweepCaps(in recordingcaps.PlaybackCapabilities) *recordingcaps.PlaybackCapabilities {
	out := in
	if in.AllowTranscode != nil {
		v := *in.AllowTranscode
		out.AllowTranscode = &v
	}
	if in.SupportsRange != nil {
		v := *in.SupportsRange
		out.SupportsRange = &v
	}
	if in.MaxVideo != nil {
		out.MaxVideo = &recordingcaps.MaxVideo{
			Width:  in.MaxVideo.Width,
			Height: in.MaxVideo.Height,
			Fps:    in.MaxVideo.Fps,
		}
	}
	out.Containers = append([]string(nil), in.Containers...)
	out.VideoCodecs = append([]string(nil), in.VideoCodecs...)
	out.AudioCodecs = append([]string(nil), in.AudioCodecs...)
	out.HLSEngines = append([]string(nil), in.HLSEngines...)
	out.VideoCodecSignals = append([]recordingcaps.VideoCodecSignal(nil), in.VideoCodecSignals...)
	return &out
}

func storageDecisionSweepClientFamilyNames(profiles []storageDecisionSweepClientProfile) []string {
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, profile.Name)
	}
	return out
}

func storageDecisionSweepReasonStrings(reasons []decisionaudit.ReasonCode) []string {
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		out = append(out, string(reason))
	}
	return out
}

func storageDecisionSweepServiceNames(rows []storageDecisionSweepServiceNote) string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ChannelName) != "" {
			names = append(names, row.ChannelName)
			continue
		}
		names = append(names, row.ServiceRef)
	}
	return strings.Join(names, ", ")
}
