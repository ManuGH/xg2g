package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/media/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

const (
	// safariDirtyHLSTimeSec reduces first-frame latency for dirty live sources while
	// preserving stable 2-second GOP/segment alignment.
	safariDirtyHLSTimeSec = 2
	// safariDirtyHLSInitTimeSec allows a shorter startup segment before steady-state.
	safariDirtyHLSInitTimeSec = 1
)

// vaapiEncodersToTest is the list of VAAPI encoders verified during preflight.
var vaapiEncodersToTest = []string{"h264_vaapi", "hevc_vaapi"}

// LocalAdapter implements ports.MediaPipeline using local exec.Command.
type LocalAdapter struct {
	BinPath                   string
	FFprobeBin                string
	HLSRoot                   string
	AnalyzeDuration           string
	ProbeSize                 string
	LiveAnalyzeDuration       string
	LiveProbeSize             string
	LiveNoBuffer              bool
	IngestFFlags              string
	IngestErrDetect           string
	IngestMaxErrorRate        string
	IngestFlags2              string
	DVRWindow                 time.Duration
	KillTimeout               time.Duration
	httpClient                *http.Client
	Logger                    zerolog.Logger
	E2                        *enigma2.Client // Dependency for Tuner operations
	FallbackTo8001            bool
	PreflightTimeout          time.Duration
	SegmentSeconds            int
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
	VaapiDevice               string          // e.g. "/dev/dri/renderD128"; empty = no VAAPI
	vaapiEncoders             map[string]bool // per-encoder preflight results ("h264_vaapi" -> true)
	vaapiDeviceChecked        bool            // device-level preflight ran
	vaapiDeviceErr            error           // device-level preflight error
	// fpsProbeFn is test-only hook; nil in production.
	fpsProbeFn func(context.Context, string) (int, string, error)
	// streamProbeFn is a test-only hook for runtime source truth; nil in production.
	streamProbeFn func(context.Context, string) (*vod.StreamInfo, error)
	// lastKnownFPS caches learned FPS by service_ref to survive probe failures.
	lastKnownFPS map[string]fpsCacheEntry
	FPSCacheTTL  time.Duration
	fpsCacheMu   sync.RWMutex
	mu           sync.Mutex
	// activeProcs maps run handles to running commands
	activeProcs map[ports.RunHandle]*exec.Cmd
}

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
	liveNoBuffer := envBool("XG2G_LIVE_NOBUFFER", false)
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
	return &LocalAdapter{
		BinPath:                   binPath,
		FFprobeBin:                strings.TrimSpace(ffprobeBin),
		HLSRoot:                   hlsRoot,
		AnalyzeDuration:           analyzeDuration,
		ProbeSize:                 probeSize,
		LiveAnalyzeDuration:       liveAnalyzeDuration,
		LiveProbeSize:             liveProbeSize,
		LiveNoBuffer:              liveNoBuffer,
		IngestFFlags:              ingestFFlags,
		IngestErrDetect:           ingestErrDetect,
		IngestMaxErrorRate:        ingestMaxErrorRate,
		IngestFlags2:              ingestFlags2,
		DVRWindow:                 dvrWindow,
		KillTimeout:               killTimeout,
		PreflightTimeout:          preflightTimeout,
		SegmentSeconds:            segmentSeconds,
		httpClient:                httpClient,
		E2:                        e2,
		Logger:                    logger,
		FallbackTo8001:            fallbackTo8001,
		StartTimeout:              startTimeout,
		StallTimeout:              stallTimeout,
		FPSProbeTimeout:           time.Duration(fpsProbeTimeoutMs) * time.Millisecond,
		FPSMin:                    fpsMin,
		FPSMax:                    fpsMax,
		FPSFallback:               fpsFallback,
		FPSFallbackInter:          fpsFallbackInter,
		SafariDirtyFilter:         safariDirtyFilter,
		SafariDirtyX264Tune:       safariDirtyTune,
		FPSProbeFFlags:            fpsProbeFFlags,
		FPSProbeErrDetect:         fpsProbeErrDetect,
		FPSProbeAnalyze:           fpsProbeAnalyze,
		FPSProbeSize:              fpsProbeSize,
		FPSProbeRetryAn:           fpsProbeRetryAnalyze,
		FPSProbeRetrySize:         fpsProbeRetrySize,
		SkipFPSProbeOnCache:       skipFPSProbeOnCache,
		SkipFPSProbeWarmup:        skipFPSProbeWarmup,
		SafariRuntimeProbeTimeout: time.Duration(safariRuntimeProbeTimeoutMs) * time.Millisecond,
		VaapiDevice:               strings.TrimSpace(vaapiDevice),
		lastKnownFPS:              make(map[string]fpsCacheEntry),
		FPSCacheTTL:               fpsCacheTTL,
		activeProcs:               make(map[ports.RunHandle]*exec.Cmd),
	}
}

// PreflightVAAPI validates that the configured VAAPI device is functional.
// Tests each available encoder (h264_vaapi, hevc_vaapi) independently.
// Results are cached per-encoder: buildArgs checks the specific encoder.
func (a *LocalAdapter) PreflightVAAPI() error {
	if a.VaapiDevice == "" {
		return nil
	}
	if a.vaapiDeviceChecked {
		return a.vaapiDeviceErr
	}

	a.vaapiEncoders = make(map[string]bool)
	a.vaapiDeviceChecked = true

	a.Logger.Info().Str("device", a.VaapiDevice).Msg("vaapi preflight: starting")

	// 1. Device accessible
	if _, err := os.Stat(a.VaapiDevice); err != nil {
		a.vaapiDeviceErr = fmt.Errorf("vaapi device not accessible: %w", err)
		a.Logger.Error().Err(a.vaapiDeviceErr).Str("device", a.VaapiDevice).Msg("vaapi preflight: device stat failed")
		hardware.SetVAAPIPreflightResult(false)
		return a.vaapiDeviceErr
	}

	// 2. Enumerate available VAAPI encoders
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- BinPath is trusted from config
	checkCmd := exec.CommandContext(ctx, a.BinPath, "-hide_banner", "-encoders")
	checkOut, err := checkCmd.Output()
	if err != nil {
		a.vaapiDeviceErr = fmt.Errorf("vaapi preflight: ffmpeg -encoders failed: %w", err)
		a.Logger.Error().Err(a.vaapiDeviceErr).Msg("vaapi preflight: encoder check failed")
		hardware.SetVAAPIPreflightResult(false)
		return a.vaapiDeviceErr
	}
	encoderList := string(checkOut)

	// 3. Test each encoder with a real 5-frame encode
	for _, enc := range vaapiEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			a.Logger.Info().Str("encoder", enc).Msg("vaapi preflight: encoder not in ffmpeg build, skipping")
			continue
		}
		if err := a.testVaapiEncoder(enc); err != nil {
			a.Logger.Warn().Err(err).Str("encoder", enc).Msg("vaapi preflight: encoder test failed")
		} else {
			a.vaapiEncoders[enc] = true
			a.Logger.Info().Str("encoder", enc).Msg("vaapi preflight: encoder verified")
		}
	}

	if len(a.vaapiEncoders) == 0 {
		a.vaapiDeviceErr = fmt.Errorf("vaapi preflight: no working VAAPI encoders found")
		a.Logger.Error().Err(a.vaapiDeviceErr).Msg("vaapi preflight: failed")
		hardware.SetVAAPIPreflightResult(false)
		return a.vaapiDeviceErr
	}

	// Publish per-encoder results for higher layers (HTTP/profile selection).
	hardware.SetVAAPIEncoderPreflight(a.vaapiEncoders)

	hardware.SetVAAPIPreflightResult(true)
	a.Logger.Info().
		Str("device", a.VaapiDevice).
		Int("verified_encoders", len(a.vaapiEncoders)).
		Msg("vaapi preflight: passed")
	return nil
}

// testVaapiEncoder runs a real 5-frame encode test for a specific VAAPI encoder.
func (a *LocalAdapter) testVaapiEncoder(encoder string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// #nosec G204 -- BinPath and VaapiDevice are trusted from config
	cmd := exec.CommandContext(ctx, a.BinPath,
		"-vaapi_device", a.VaapiDevice,
		"-f", "lavfi",
		"-i", "testsrc=duration=0.2:size=1280x720:rate=25",
		"-vf", "format=nv12,hwupload",
		"-c:v", encoder,
		"-frames:v", "5",
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("encode test failed: %w (output: %s)", err, string(out))
	}
	return nil
}

// VaapiEncoderVerified returns true if the given encoder passed preflight.
func (a *LocalAdapter) VaapiEncoderVerified(encoder string) bool {
	return a.vaapiEncoders[encoder]
}

// Start initiates the media process.
func (a *LocalAdapter) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	if spec.Source.Type == ports.SourceTuner && a.E2 != nil {
		if spec.Source.TunerSlot < 0 {
			return "", fmt.Errorf("invalid tuner slot: %d", spec.Source.TunerSlot)
		}
		tuner := enigma2.NewTuner(a.E2, spec.Source.TunerSlot, 10*time.Second)
		if err := tuner.Tune(ctx, spec.Source.ID); err != nil {
			return "", fmt.Errorf("tuning failed: %w", err)
		}
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "tuner_tuned").
			Int("tuner_slot", spec.Source.TunerSlot).
			Str("service_ref", spec.Source.ID).
			Msg("tuner tune completed")
	}

	inputURL := ""
	switch spec.Source.Type {
	case ports.SourceTuner:
		if a.E2 == nil {
			return "", fmt.Errorf("tuner source requires enigma2 client")
		}
		streamURL, err := a.E2.ResolveStreamURL(ctx, spec.Source.ID)
		if err != nil {
			return "", fmt.Errorf("resolve stream url: %w", err)
		}
		streamURL = a.injectCredentialsIfAllowed(streamURL)
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "stream_url_resolved").
			Str("resolved_url", sanitizeURLForLog(streamURL)).
			Msg("stream url resolved")

		chosenURL, err := a.selectStreamURL(ctx, spec.SessionID, spec.Source.ID, streamURL)
		if err != nil {
			return "", err
		}
		inputURL = chosenURL
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "input_url_selected").
			Str("input_url", sanitizeURLForLog(inputURL)).
			Msg("stream input url selected")
	case ports.SourceURL:
		inputURL = spec.Source.ID
		a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "input_url_selected").
			Str("input_url", sanitizeURLForLog(inputURL)).
			Msg("direct input url selected")
	}
	sourceKey := fpsCacheKey(spec.Source, inputURL)

	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_args_build_started").
		Str("input_url", sanitizeURLForLog(inputURL)).
		Msg("ffmpeg args build started")
	args, err := a.buildArgs(ctx, spec, inputURL)
	if err != nil {
		return "", fmt.Errorf("failed to build args: %w", err)
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_args_built").
		Int("arg_count", len(args)).
		Msg("ffmpeg args build finished")

	// #nosec G204 - BinPath is trusted from config; args are generated by strict internal logic (buildArgs)
	cmd := exec.CommandContext(ctx, a.BinPath, args...)
	procgroup.Set(cmd) // Mandatory for tree reaping

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to pipe stderr: %w", err)
	}
	cmd.Stdout = nil

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg start failed: %w", err)
	}

	pid := cmd.Process.Pid
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "ffmpeg_started").
		Int("pid", pid).
		Msg("ffmpeg process started")
	handle := ports.RunHandle(fmt.Sprintf("%s-%d", spec.SessionID, pid))
	a.mu.Lock()
	a.activeProcs[handle] = cmd
	a.mu.Unlock()

	go a.monitorProcess(ctx, handle, cmd, stderr, spec.SessionID)
	if sourceKey != "" {
		go a.learnFPSFromOutput(sourceKey, spec.SessionID)
	}

	metrics.RecordPipelineSpawn("ffmpeg", "admitted")
	return handle, nil
}

func (a *LocalAdapter) monitorProcess(parentCtx context.Context, handle ports.RunHandle, cmd *exec.Cmd, stderr io.ReadCloser, sessionID string) {
	defer func() {
		a.mu.Lock()
		delete(a.activeProcs, handle)
		a.mu.Unlock()
	}()

	wd := watchdog.New(a.StartTimeout, a.StallTimeout)

	// Forward lines to RingBuffer/Log and Watchdog
	scanner := bufio.NewScanner(stderr)
	scanner.Split(scanFFmpegLogTokens)
	firstFrameLogged := false
	firstSegmentLogged := false

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	wdCtx, wdCancel := context.WithCancel(parentCtx)
	defer wdCancel()

	wdErrCh := make(chan error, 1)
	go func() {
		wdErrCh <- wd.Run(wdCtx)
	}()

	for scanner.Scan() {
		line := scanner.Text()
		if !firstFrameLogged {
			if frame, ok := parseFFmpegFrameCount(line); ok && frame > 0 {
				firstFrameLogged = true
				a.Logger.Info().
					Str("session_id", sessionID).
					Str("startup_phase", "first_frame").
					Int("frame", frame).
					Msg("ffmpeg first frame observed")
			}
		}
		if !firstSegmentLogged {
			if segmentPath, ok := extractStartupSegmentPath(line); ok {
				firstSegmentLogged = true
				a.Logger.Info().
					Str("session_id", sessionID).
					Str("startup_phase", "first_segment_write").
					Str("segment_path", segmentPath).
					Msg("ffmpeg first segment write observed")
			}
		}
		a.Logger.Error().Str("sessionId", sessionID).Str("ffmpeg_log", sanitizeFFmpegLogLine(line)).Msg("ffmpeg output")
		wd.ParseLine(line)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		a.Logger.Warn().Err(scanErr).Str("sessionId", sessionID).Msg("ffmpeg stderr scan failed")
	}

	procErr := cmd.Wait()
	wdCancel()

	select {
	case wdErr := <-wdErrCh:
		if wdErr != nil {
			a.Logger.Error().Err(wdErr).Str("sessionId", sessionID).Msg("watchdog triggered process termination")
		}
	default:
	}

	if procErr != nil {
		a.Logger.Debug().Err(procErr).Str("sessionId", sessionID).Msg("ffmpeg process exited")
	}
}

func scanFFmpegLogTokens(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) == 0 {
		if atEOF {
			return 0, nil, nil
		}
		return 0, nil, nil
	}
	for i, b := range data {
		if b != '\n' && b != '\r' {
			continue
		}
		advance = i + 1
		if b == '\r' && advance < len(data) && data[advance] == '\n' {
			advance++
		}
		return advance, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	if atEOF {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

func parseFFmpegFrameCount(line string) (int, bool) {
	idx := strings.Index(line, "frame=")
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimLeft(line[idx+len("frame="):], " ")
	if rest == "" {
		return 0, false
	}
	count := 0
	digits := 0
	for digits < len(rest) {
		ch := rest[digits]
		if ch < '0' || ch > '9' {
			break
		}
		count = count*10 + int(ch-'0')
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	return count, true
}

func extractStartupSegmentPath(line string) (string, bool) {
	if !strings.Contains(line, "Opening ") || !strings.Contains(line, " for writing") {
		return "", false
	}
	start := strings.IndexAny(line, `'"`)
	if start < 0 || start+1 >= len(line) {
		return "", false
	}
	quote := line[start]
	endRel := strings.IndexByte(line[start+1:], quote)
	if endRel < 0 {
		return "", false
	}
	path := line[start+1 : start+1+endRel]
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "seg_") {
		return "", false
	}
	if strings.HasSuffix(base, ".m4s") || strings.HasSuffix(base, ".ts") {
		return path, true
	}
	return "", false
}

// Stop terminates the process.
func (a *LocalAdapter) Stop(ctx context.Context, handle ports.RunHandle) error {
	a.mu.Lock()
	cmd, exists := a.activeProcs[handle]
	if exists {
		delete(a.activeProcs, handle)
	}
	a.mu.Unlock()

	if !exists {
		return nil // Idempotent
	}

	if cmd.Process != nil {
		// Use procgroup for deterministic tree reaping
		return procgroup.KillGroup(cmd.Process.Pid, 2*time.Second, a.KillTimeout)
	}

	return nil
}

func (a *LocalAdapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	cmd, exists := a.activeProcs[handle]
	a.mu.Unlock()
	if !exists {
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process not found",
			LastCheck: time.Now(),
		}
	}
	if cmd == nil || cmd.Process == nil {
		a.mu.Lock()
		delete(a.activeProcs, handle)
		a.mu.Unlock()
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process not initialized",
			LastCheck: time.Now(),
		}
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		a.mu.Lock()
		delete(a.activeProcs, handle)
		a.mu.Unlock()
		msg := "process not running"
		if errors.Is(err, os.ErrProcessDone) {
			msg = "process exited"
		}
		return ports.HealthStatus{
			Healthy:   false,
			Message:   msg,
			LastCheck: time.Now(),
		}
	}

	return ports.HealthStatus{
		Healthy:   true,
		Message:   "process active",
		LastCheck: time.Now(),
	}
}

func envIntBounded(key string, defaultValue, minValue, maxValue int) int {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}

func envBool(key string, defaultValue bool) bool {
	return config.ParseBool(key, defaultValue)
}
