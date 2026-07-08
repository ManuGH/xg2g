package ffmpeg

import (
	"context"
	"github.com/ManuGH/xg2g/internal/config"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/hls/ringbuffer"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
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
	BinPath                    string
	FFprobeBin                 string
	HLSRoot                    string
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
	FallbackTo8001             bool
	PreflightTimeout           time.Duration
	SegmentSeconds             int
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
	// finalizedProfiles keeps the finalized profile that actually launched for a handle.
	finalizedProfiles map[ports.RunHandle]ports.ProfileSpec
	// executedPlans keeps the execution-truth plan parsed from the real argv that launched for a handle.
	executedPlans map[ports.RunHandle]ports.ExecutedFFmpegPlan
	// runtimeDiagnostics keeps the latest FFmpeg progress/source-warning snapshot.
	runtimeDiagnostics map[ports.RunHandle]ports.RuntimeDiagnostics
	// processDetails keeps the last meaningful failure summary for a handle.
	processDetails map[ports.RunHandle]string
	// completedProcessDetails briefly preserves final process summaries after active cleanup.
	completedProcessDetails     map[ports.RunHandle]string
	completedProcessDetailOrder []ports.RunHandle
}

const maxCompletedProcessDetails = 128

// NewLocalAdapter creates a new adapter instance.
func NewLocalAdapter(binPath string, ffprobeBin string, hlsRoot string, e2 *enigma2.Client, logger zerolog.Logger, analyzeDuration string, probeSize string, dvrWindow time.Duration, killTimeout time.Duration, fallbackTo8001 bool, preflightTimeout time.Duration, segmentSeconds int, startTimeout, stallTimeout time.Duration, vaapiDevice string) *LocalAdapter {
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

	// Every ENV-tunable ingest/FPS/Safari knob is resolved in one place
	// (LoadAdapterConfig), keeping this constructor focused on wiring
	// dependencies. The FPS probe falls back to the general analyze/probe depth
	// when its own override is unset, so those already-defaulted values are
	// passed in.
	cfg := LoadAdapterConfig(analyzeDuration, probeSize)

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
		finalizedProfiles:          make(map[ports.RunHandle]ports.ProfileSpec),
		executedPlans:              make(map[ports.RunHandle]ports.ExecutedFFmpegPlan),
		runtimeDiagnostics:         make(map[ports.RunHandle]ports.RuntimeDiagnostics),
		processDetails:             make(map[ports.RunHandle]string),
		completedProcessDetails:    make(map[ports.RunHandle]string),
	}
	adapter.detector = newDetector(binPath, logger, strings.TrimSpace(vaapiDevice), hlsRoot)
	adapter.detector.recordProcessDetail = adapter.recordProcessDetail
	adapter.detector.terminateProcessGroup = adapter.terminateProcessGroup
	return adapter
}
