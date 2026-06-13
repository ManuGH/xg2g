package ffmpeg

import (
	"context"
	"github.com/ManuGH/xg2g/internal/config"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
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
	StreamRelayAnalyzeDuration string
	StreamRelayProbeSize       string
	LiveNoBuffer               bool
	ForceIgnDTS                bool
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
	StartTimeout               time.Duration
	StallTimeout               time.Duration
	FPSProbeTimeout            time.Duration
	FPSMin                     int
	FPSMax                     int
	FPSFallback                int
	FPSFallbackInter           int
	SafariDirtyFilter          string
	SafariDirtyX264Tune        string
	FPSProbeFFlags             string
	FPSProbeErrDetect          string
	FPSProbeAnalyze            string
	FPSProbeSize               string
	FPSProbeRetryAn            string
	FPSProbeRetrySize          string
	SkipFPSProbeOnCache        bool
	SkipFPSProbeWarmup         time.Duration
	SafariRuntimeProbeTimeout  time.Duration
	VaapiDevice                string // e.g. "/dev/dri/renderD128"; empty = no VAAPI
	detector                   *Detector
	// fpsProbeFn is test-only hook; nil in production.
	fpsProbeFn func(context.Context, string) (int, string, error)
	// streamProbeFn is a test-only hook for runtime source truth; nil in production.
	streamProbeFn func(context.Context, string) (*vod.StreamInfo, error)
	// hostBenchmarkClassFn is a test-only hook returning the host benchmark class
	// ("weak"/"moderate"/"strong"/"") for a profile id; nil in production (real
	// host benchmark snapshot is used).
	hostBenchmarkClassFn func(profileID string) string
	// lastKnownFPS caches learned FPS by service_ref to survive probe failures.
	lastKnownFPS map[string]fpsCacheEntry
	FPSCacheTTL  time.Duration
	fpsCacheMu   sync.RWMutex
	mu           sync.Mutex
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
	liveAnalyzeDuration := strings.TrimSpace(config.ParseString("XG2G_LIVE_ANALYZE_DURATION", ""))
	if liveAnalyzeDuration == "" {
		liveAnalyzeDuration = "1000000" // 1s for low-latency live ingest
	}
	liveProbeSize := strings.TrimSpace(config.ParseString("XG2G_LIVE_PROBE_SIZE", ""))
	if liveProbeSize == "" {
		liveProbeSize = "1M" // 1MB for low-latency live ingest
	}
	// Stream-relay transcode inputs need a deeper probe than the general live
	// default to resolve dimensions/audio. 5s is the sharpened default: it
	// roughly halves the relay-transcode startup wait vs the old 10s while
	// keeping margin for slower/burstier relays. (3s was verified clean on a 4K
	// relay.) Raise XG2G_STREAMRELAY_ANALYZE_DURATION if a relay needs deeper
	// probing; lower it (e.g. 3000000) for the fastest start.
	streamRelayAnalyzeDuration := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_ANALYZE_DURATION", ""))
	if streamRelayAnalyzeDuration == "" {
		streamRelayAnalyzeDuration = "5000000" // 5s
	}
	streamRelayProbeSize := strings.TrimSpace(config.ParseString("XG2G_STREAMRELAY_PROBE_SIZE", ""))
	if streamRelayProbeSize == "" {
		streamRelayProbeSize = "20M"
	}
	liveNoBuffer := envBool("XG2G_LIVE_NOBUFFER", false)
	forceIgnDTS := envBool("XG2G_FORCE_IGNDTS", false)
	if killTimeout <= 0 {
		killTimeout = 5 * time.Second
	}
	if segmentSeconds <= 0 {
		segmentSeconds = config.DefaultHLSSegmentSeconds
	}
	fpsProbeTimeoutMs := envIntBounded("XG2G_FPS_PROBE_TIMEOUT_MS", 1500, 300, 5000)
	fpsMin := envIntBounded("XG2G_FPS_MIN", 15, 10, 240)
	fpsMax := envIntBounded("XG2G_FPS_MAX", 120, fpsMin, 240)
	fpsFallback := envIntBounded("XG2G_FPS_FALLBACK", 25, 10, 120)
	fpsFallbackInter := envIntBounded("XG2G_FPS_FALLBACK_INTERLACED", 50, 10, 120)
	resilientIngest := envBool("XG2G_RESILIENT_INGEST", true)
	safariDirtyFilter := strings.TrimSpace(config.ParseString("XG2G_SAFARI_DIRTY_DEINTERLACE_FILTER", ""))
	if safariDirtyFilter == "" {
		safariDirtyFilter = "bwdif=mode=send_field:parity=auto:deint=all"
	}
	safariDirtyTune := strings.TrimSpace(config.ParseString("XG2G_SAFARI_DIRTY_X264_TUNE", ""))
	ingestFFlags := strings.TrimSpace(config.ParseString("XG2G_INGEST_FFLAGS", ""))
	if ingestFFlags == "" {
		if resilientIngest {
			ingestFFlags = "+genpts+discardcorrupt+flush_packets"
		} else {
			ingestFFlags = "+genpts"
		}
	}
	ingestErrDetect := strings.TrimSpace(config.ParseString("XG2G_INGEST_ERR_DETECT", ""))
	if ingestErrDetect == "" && resilientIngest {
		ingestErrDetect = "ignore_err"
	}
	ingestMaxErrorRate := strings.TrimSpace(config.ParseString("XG2G_INGEST_MAX_ERROR_RATE", ""))
	if ingestMaxErrorRate == "" && resilientIngest {
		ingestMaxErrorRate = "1.0"
	}
	ingestFlags2 := strings.TrimSpace(config.ParseString("XG2G_INGEST_FLAGS2", ""))
	if ingestFlags2 == "" && resilientIngest {
		ingestFlags2 = "+showall+export_mvs"
	}
	fpsProbeFFlags := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_FFLAGS", ""))
	if fpsProbeFFlags == "" {
		if resilientIngest {
			fpsProbeFFlags = "+genpts+discardcorrupt+igndts"
		} else {
			fpsProbeFFlags = "+genpts+igndts"
		}
	}
	fpsProbeErrDetect := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_ERR_DETECT", ""))
	if fpsProbeErrDetect == "" && resilientIngest {
		fpsProbeErrDetect = "ignore_err"
	}
	fpsProbeAnalyze := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_ANALYZE_DURATION", ""))
	if fpsProbeAnalyze == "" {
		fpsProbeAnalyze = analyzeDuration
	}
	fpsProbeSize := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_SIZE", ""))
	if fpsProbeSize == "" {
		fpsProbeSize = probeSize
	}
	fpsProbeRetryAnalyze := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_RETRY_ANALYZE_DURATION", ""))
	if fpsProbeRetryAnalyze == "" {
		fpsProbeRetryAnalyze = "10000000"
	}
	fpsProbeRetrySize := strings.TrimSpace(config.ParseString("XG2G_FPS_PROBE_RETRY_SIZE", ""))
	if fpsProbeRetrySize == "" {
		fpsProbeRetrySize = "20M"
	}
	skipFPSProbeOnCache := envBool("XG2G_SKIP_FPS_PROBE_ON_CACHE_HIT", false)
	skipFPSProbeWarmup := config.ParseDuration("XG2G_SKIP_FPS_PROBE_WARMUP", 500*time.Millisecond)
	if skipFPSProbeWarmup < 0 {
		skipFPSProbeWarmup = 500 * time.Millisecond
	}
	safariRuntimeProbeTimeoutMs := envIntBounded("XG2G_SAFARI_RUNTIME_PROBE_TIMEOUT_MS", 6000, 1000, 15000)
	fpsCacheTTL := config.ParseDuration("XG2G_FPS_CACHE_TTL", 24*time.Hour)
	if fpsCacheTTL <= 0 {
		fpsCacheTTL = 24 * time.Hour
	}

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
		LiveAnalyzeDuration:        liveAnalyzeDuration,
		LiveProbeSize:              liveProbeSize,
		StreamRelayAnalyzeDuration: streamRelayAnalyzeDuration,
		StreamRelayProbeSize:       streamRelayProbeSize,
		LiveNoBuffer:               liveNoBuffer,
		ForceIgnDTS:                forceIgnDTS,
		IngestFFlags:               ingestFFlags,
		IngestErrDetect:            ingestErrDetect,
		IngestMaxErrorRate:         ingestMaxErrorRate,
		IngestFlags2:               ingestFlags2,
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
		FPSProbeTimeout:            time.Duration(fpsProbeTimeoutMs) * time.Millisecond,
		FPSMin:                     fpsMin,
		FPSMax:                     fpsMax,
		FPSFallback:                fpsFallback,
		FPSFallbackInter:           fpsFallbackInter,
		SafariDirtyFilter:          safariDirtyFilter,
		SafariDirtyX264Tune:        safariDirtyTune,
		FPSProbeFFlags:             fpsProbeFFlags,
		FPSProbeErrDetect:          fpsProbeErrDetect,
		FPSProbeAnalyze:            fpsProbeAnalyze,
		FPSProbeSize:               fpsProbeSize,
		FPSProbeRetryAn:            fpsProbeRetryAnalyze,
		FPSProbeRetrySize:          fpsProbeRetrySize,
		SkipFPSProbeOnCache:        skipFPSProbeOnCache,
		SkipFPSProbeWarmup:         skipFPSProbeWarmup,
		SafariRuntimeProbeTimeout:  time.Duration(safariRuntimeProbeTimeoutMs) * time.Millisecond,
		VaapiDevice:                strings.TrimSpace(vaapiDevice),
		lastKnownFPS:               make(map[string]fpsCacheEntry),
		FPSCacheTTL:                fpsCacheTTL,
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
