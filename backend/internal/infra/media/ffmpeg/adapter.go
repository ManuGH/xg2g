package ffmpeg

import (
	"context"
	"github.com/ManuGH/xg2g/internal/config"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/store"
	"github.com/rs/zerolog"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	// safariDirtyHLSTimeSec reduces first-frame latency for dirty live sources while
	// preserving stable 2-second GOP/segment alignment.
	safariDirtyHLSTimeSec = 2
	// safariDirtyHLSInitTimeSec allows a shorter startup segment before steady-state.
	safariDirtyHLSInitTimeSec = 1

	defaultRuntimePathCorrectnessMinYAvg = 8.0
	defaultRuntimePathCorrectnessChecks  = 2
)

// vaapiEncodersToTest is the list of VAAPI encoders verified during preflight.
var vaapiEncodersToTest = []string{"h264_vaapi", "hevc_vaapi", "av1_vaapi"}

// nvencEncodersToTest is the list of NVENC encoders verified during preflight.
var nvencEncodersToTest = []string{"h264_nvenc", "hevc_nvenc", "av1_nvenc"}

var startupProfilesToBenchmark = []string{
	playbackports.BenchmarkProfileAudioAACStereo,
	playbackports.BenchmarkProfileVideoH2641080P,
	playbackports.BenchmarkProfileVideoH2641080I,
	playbackports.BenchmarkProfileVideoH2641080I50,
	playbackports.BenchmarkProfileVideoH2642160P,
	playbackports.BenchmarkProfileVideoH2642160P50,
}

type profileProbeRequest struct {
	ProfileID string
	Backend   string
	Encoder   string
}

type pathProbeRequest struct {
	PathID  string
	Backend string
	Encoder string
}

// LocalAdapter implements ports.MediaPipeline using local exec.Command.
type LocalAdapter struct {
	Config                     AdapterConfig
	BinPath                    string
	FFprobeBin                 string
	HLSRoot                    string
	StoreRegistry              store.StoreRegistry
	DiagnosticLookup           ports.DiagnosticLookup
	AnalyzeDuration            string
	ProbeSize                  string
	LiveAnalyzeDuration        string
	LiveProbeSize              string
	LiveUserAgent              string
	StreamRelayAnalyzeDuration string
	StreamRelayProbeSize       string
	LiveNoBuffer               bool
	ForceIgnDTS                bool
	LiveAvsyncAtrim            bool
	LiveAvsyncPipeNoTrim       bool
	IngestFFlags               string
	IngestErrDetect            string
	IngestMaxErrorRate         string
	IngestFlags2               string
	DVRWindow                  time.Duration
	KillTimeout                time.Duration
	httpClient                 *http.Client
	Logger                     zerolog.Logger
	E2                         *enigma2.Client // Dependency for Tuner operations

	// LiveSources is where a SourceTuner transcode gets its bytes. It is the only
	// sanctioned live input: with it absent, Start refuses a tuner source rather
	// than resolving a receiver URL, so a misconfigured deployment fails loudly
	// instead of quietly reopening the path this replaced.
	LiveSources      ports.LiveSourceProvider
	FallbackTo8001   bool
	PreflightTimeout time.Duration
	SegmentSeconds   int
	// LowLatencyHLS switches fmp4 live sessions to the LL-HLS segment
	// layout: short segments fragmented on the part-target grid.
	LowLatencyHLS             bool
	ReadySegments             int
	StartTimeout              time.Duration
	StallTimeout              time.Duration
	FPSProbeTimeout           time.Duration
	FPSMin                    int
	FPSMax                    int
	FPSFallback               int
	FPSFallbackInter          int
	SafariDirtyFilter         string
	SafariDirtyX264Tune       string
	FPSProbeFFlags            string
	FPSProbeErrDetect         string
	FPSProbeAnalyze           string
	FPSProbeSize              string
	FPSProbeRetryAn           string
	FPSProbeRetrySize         string
	SkipFPSProbeOnCache       bool
	SkipFPSProbeWarmup        time.Duration
	SafariRuntimeProbeTimeout time.Duration
	VaapiDevice               string // e.g. "/dev/dri/renderD128"; empty = no VAAPI
	detector                  *Detector
	// fpsProbeFn is test-only hook; nil in production.
	fpsProbeFn func(context.Context, string) (int, string, error)
	// streamProbeFn is a test-only hook for runtime source truth; nil in production.
	streamProbeFn func(context.Context, string) (*vod.StreamInfo, error)
	// liveAudioProbeFn is a test-only hook for live audio stream selection; nil in production.
	liveAudioProbeFn func(context.Context, string) ([]liveAudioStream, error)
	// hostBenchmarkClassFn is a test-only hook returning the host benchmark class
	// ("weak"/"moderate"/"strong"/"") for a profile id; nil in production (real
	// host benchmark snapshot is used).
	hostBenchmarkClassFn func(profileID string) string
	// lastKnownFPS caches learned FPS by service_ref to survive probe failures.
	lastKnownFPS   map[string]fpsCacheEntry
	FPSCacheTTL    time.Duration
	fpsCacheMu     sync.RWMutex
	mu             sync.Mutex
	inMemoryIngest bool
	ingestPort     int
	ingestServer   *ringbuffer.IngestServer
	// activeProcs maps run handles to running commands
	activeProcs map[ports.RunHandle]*exec.Cmd
	// handleSessions maps run handles to session IDs for diagnostics
	handleSessions map[ports.RunHandle]string
	// finalizedProfiles keeps the finalized profile that actually launched for a handle.
	finalizedProfiles map[ports.RunHandle]ports.ProfileSpec
	// executedPlans keeps the execution-truth plan parsed from the real argv that launched for a handle.
	executedPlans map[ports.RunHandle]ports.ExecutedFFmpegPlan
	// generations tracks the process generation counter per transcode job ID for
	// P6.1a correlation. Unlike every other per-handle map here it deliberately
	// OUTLIVES the process it describes: a respawn of the same job must observe
	// generation N+1, so clearing it in removeActiveProcessLocked would reset the
	// counter on exactly the event the correlation chain exists to describe.
	// It is bounded by generationOrder instead — see retireGenerationsLocked.
	generations map[string]uint64
	// generationOrder is the eviction order for generations, least-recently-spawned
	// first. A job is appended on its first spawn and moved to the back on every
	// respawn, so the front of the slice is the job that has gone longest without
	// starting a process.
	generationOrder []string
	// processIdentities keeps operational TranscodeProcessIdentity for active handles
	processIdentities map[ports.RunHandle]TranscodeProcessIdentity
	// runtimeDiagnostics keeps the latest FFmpeg progress/source-warning snapshot.
	runtimeDiagnostics map[ports.RunHandle]ports.RuntimeDiagnostics
	// processDetails keeps the last meaningful failure summary for a handle.
	processDetails map[ports.RunHandle]string
	// completedProcessDetails briefly preserves final process summaries after active cleanup.
	completedProcessDetails     map[ports.RunHandle]string
	completedProcessDetailOrder []ports.RunHandle
}

const maxCompletedProcessDetails = 128

// maxTrackedGenerations bounds the per-job generation counters retained by the
// adapter. The counter for a job that is currently running a process is never
// evicted (see retireGenerationsLocked), so the bound can only ever drop jobs
// with no live process — and reaching it at all takes maxTrackedGenerations
// distinct newer jobs since that job last spawned. Past that point a respawn
// restarts at generation 1, which duplicates a generation number in the logs
// rather than corrupting a live correlation chain.
const maxTrackedGenerations = 1024

// NewLocalAdapter creates a new adapter instance.
func NewLocalAdapter(binPath string, ffprobeBin string, hlsRoot string, e2 *enigma2.Client, logger zerolog.Logger, analyzeDuration string, probeSize string, dvrWindow time.Duration, killTimeout time.Duration, fallbackTo8001 bool, preflightTimeout time.Duration, segmentSeconds int, startTimeout, stallTimeout time.Duration, vaapiDevice string) *LocalAdapter {
	return NewLocalAdapterWithConfig(
		binPath, ffprobeBin, hlsRoot, e2, logger, analyzeDuration, probeSize,
		dvrWindow, killTimeout, fallbackTo8001, preflightTimeout, segmentSeconds,
		startTimeout, stallTimeout, vaapiDevice,
		LoadAdapterConfig(analyzeDuration, probeSize),
	)
}

// NewLocalAdapterWithConfig constructs an adapter from an immutable operator
// snapshot. Production composition roots use this constructor so request-time
// command planning never consults process environment.
func NewLocalAdapterWithConfig(binPath string, ffprobeBin string, hlsRoot string, e2 *enigma2.Client, logger zerolog.Logger, analyzeDuration string, probeSize string, dvrWindow time.Duration, killTimeout time.Duration, fallbackTo8001 bool, preflightTimeout time.Duration, segmentSeconds int, startTimeout, stallTimeout time.Duration, vaapiDevice string, cfg AdapterConfig) *LocalAdapter {
	analyzeDuration = strings.TrimSpace(analyzeDuration)
	probeSize = strings.TrimSpace(probeSize)
	if analyzeDuration == "" {
		analyzeDuration = "2000000" // 2s for fast live starts
	}
	if probeSize == "" {
		probeSize = "5M" // 5MB for live streams
	}
	if killTimeout <= 0 {
		killTimeout = 5 * time.Second
	}
	if segmentSeconds <= 0 {
		segmentSeconds = config.DefaultHLSSegmentSeconds
	}

	cfg = cloneAdapterConfig(cfg)

	httpClient := &http.Client{
		Timeout: preflightTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: preflightTimeout,
			}).DialContext,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   preflightTimeout,
			ResponseHeaderTimeout: preflightTimeout,
			DisableCompression:    true,
		},
	}
	adapter := &LocalAdapter{
		Config:                     cfg,
		BinPath:                    binPath,
		FFprobeBin:                 strings.TrimSpace(ffprobeBin),
		HLSRoot:                    hlsRoot,
		AnalyzeDuration:            analyzeDuration,
		ProbeSize:                  probeSize,
		LiveAnalyzeDuration:        cfg.LiveAnalyzeDuration,
		LiveProbeSize:              cfg.LiveProbeSize,
		LiveUserAgent:              cfg.LiveUserAgent,
		StreamRelayAnalyzeDuration: cfg.StreamRelayAnalyzeDuration,
		StreamRelayProbeSize:       cfg.StreamRelayProbeSize,
		LiveNoBuffer:               cfg.LiveNoBuffer,
		ForceIgnDTS:                cfg.ForceIgnDTS,
		LiveAvsyncAtrim:            cfg.LiveAvsyncAtrim,
		LiveAvsyncPipeNoTrim:       cfg.LiveAvsyncPipeNoTrim,
		IngestFFlags:               cfg.IngestFFlags,
		IngestErrDetect:            cfg.IngestErrDetect,
		IngestMaxErrorRate:         cfg.IngestMaxErrorRate,
		IngestFlags2:               cfg.IngestFlags2,
		DVRWindow:                  dvrWindow,
		KillTimeout:                killTimeout,
		PreflightTimeout:           preflightTimeout,
		SegmentSeconds:             segmentSeconds,
		httpClient:                 httpClient,
		E2:                         e2,
		Logger:                     logger,
		FallbackTo8001:             fallbackTo8001,
		StartTimeout:               startTimeout,
		StallTimeout:               stallTimeout,
		FPSProbeTimeout:            cfg.FPSProbeTimeout,
		FPSMin:                     cfg.FPSMin,
		FPSMax:                     cfg.FPSMax,
		FPSFallback:                cfg.FPSFallback,
		FPSFallbackInter:           cfg.FPSFallbackInter,
		SafariDirtyFilter:          cfg.SafariDirtyFilter,
		SafariDirtyX264Tune:        cfg.SafariDirtyX264Tune,
		FPSProbeFFlags:             cfg.FPSProbeFFlags,
		FPSProbeErrDetect:          cfg.FPSProbeErrDetect,
		FPSProbeAnalyze:            cfg.FPSProbeAnalyze,
		FPSProbeSize:               cfg.FPSProbeSize,
		FPSProbeRetryAn:            cfg.FPSProbeRetryAn,
		FPSProbeRetrySize:          cfg.FPSProbeRetrySize,
		SkipFPSProbeOnCache:        cfg.SkipFPSProbeOnCache,
		SkipFPSProbeWarmup:         cfg.SkipFPSProbeWarmup,
		SafariRuntimeProbeTimeout:  cfg.SafariRuntimeProbeTimeout,
		VaapiDevice:                strings.TrimSpace(vaapiDevice),
		lastKnownFPS:               make(map[string]fpsCacheEntry),
		FPSCacheTTL:                cfg.FPSCacheTTL,
		activeProcs:                make(map[ports.RunHandle]*exec.Cmd),
		handleSessions:             make(map[ports.RunHandle]string),
		finalizedProfiles:          make(map[ports.RunHandle]ports.ProfileSpec),
		executedPlans:              make(map[ports.RunHandle]ports.ExecutedFFmpegPlan),
		generations:                make(map[string]uint64),
		processIdentities:          make(map[ports.RunHandle]TranscodeProcessIdentity),
		runtimeDiagnostics:         make(map[ports.RunHandle]ports.RuntimeDiagnostics),
		processDetails:             make(map[ports.RunHandle]string),
		completedProcessDetails:    make(map[ports.RunHandle]string),
	}
	adapter.detector = newDetector(binPath, logger, strings.TrimSpace(vaapiDevice), hlsRoot, cfg)
	adapter.detector.recordProcessDetail = adapter.recordProcessDetail
	adapter.detector.terminateProcessGroup = adapter.terminateProcessGroup
	return adapter
}

// DiagnosticContext holds context fields for passive lifecycle diagnostics.
type DiagnosticContext struct {
	SessionID          string
	GenerationID       string
	Reason             string
	ElapsedSinceStopMs int64
}

// GetDiagnosticContext queries DiagnosticLookup for diagnostic session metadata.
func (a *LocalAdapter) GetDiagnosticContext(sessionID string) DiagnosticContext {
	dc := DiagnosticContext{
		SessionID:    sessionID,
		GenerationID: "unknown",
		Reason:       "none",
	}
	if a != nil && a.DiagnosticLookup != nil && sessionID != "" {
		if meta, ok := a.DiagnosticLookup.GetDiagnosticMetadata(context.Background(), sessionID); ok {
			if meta.GenerationID != "" {
				dc.GenerationID = meta.GenerationID
			} else if meta.CorrelationID != "" {
				dc.GenerationID = meta.CorrelationID
			}
			if meta.Reason != "" {
				dc.Reason = meta.Reason
			}
			if meta.StopRequestedAtUnixMs > 0 {
				dc.ElapsedSinceStopMs = time.Now().UnixMilli() - meta.StopRequestedAtUnixMs
			}
		}
	}
	return dc
}

func (a *LocalAdapter) registerProcessIdentity(handle ports.RunHandle, jobID string, pid int, startedAt time.Time) TranscodeProcessIdentity {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.generations == nil {
		a.generations = make(map[string]uint64)
	}
	if a.processIdentities == nil {
		a.processIdentities = make(map[ports.RunHandle]TranscodeProcessIdentity)
	}
	a.generations[jobID]++
	gen := a.generations[jobID]
	ident := NewProcessIdentity(jobID, gen, pid, startedAt)
	a.processIdentities[handle] = ident
	a.touchGenerationLocked(jobID)
	a.retireGenerationsLocked()
	return ident
}

// touchGenerationLocked moves jobID to the back of the eviction order, marking it
// the most recently spawned job.
func (a *LocalAdapter) touchGenerationLocked(jobID string) {
	for i, id := range a.generationOrder {
		if id == jobID {
			a.generationOrder = append(a.generationOrder[:i], a.generationOrder[i+1:]...)
			break
		}
	}
	a.generationOrder = append(a.generationOrder, jobID)
}

// retireGenerationsLocked drops the least-recently-spawned generation counters
// once more than maxTrackedGenerations jobs are tracked.
//
// The counter cannot be retired at process end — a respawn has to see N+1 — and
// there is no single session-teardown path in the adapter that is guaranteed to
// run for every session, so a bound is what keeps the map from growing for the
// lifetime of the daemon. A job holding a live process is skipped rather than
// evicted, which is what makes the bound safe for correlation: the only counters
// that can disappear belong to jobs with nothing running to correlate. That skip
// also means the map may briefly exceed the bound, capped by the number of
// concurrently running processes.
func (a *LocalAdapter) retireGenerationsLocked() {
	if len(a.generations) <= maxTrackedGenerations {
		return
	}
	live := make(map[string]struct{}, len(a.processIdentities))
	for _, ident := range a.processIdentities {
		live[ident.JobID] = struct{}{}
	}
	for len(a.generations) > maxTrackedGenerations {
		evicted := false
		for i, id := range a.generationOrder {
			if _, running := live[id]; running {
				continue
			}
			a.generationOrder = append(a.generationOrder[:i], a.generationOrder[i+1:]...)
			delete(a.generations, id)
			evicted = true
			break
		}
		if !evicted {
			// Every tracked job is running. Nothing may be dropped without
			// resetting a live counter, so the map stays over its bound until
			// one of them stops.
			return
		}
	}
}

func (a *LocalAdapter) getProcessIdentity(handle ports.RunHandle) (TranscodeProcessIdentity, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ident, ok := a.processIdentities[handle]
	return ident, ok
}

func (a *LocalAdapter) GetProcessIdentity(handle ports.RunHandle) (TranscodeProcessIdentity, bool) {
	return a.getProcessIdentity(handle)
}
