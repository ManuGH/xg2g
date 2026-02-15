package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/decision"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	xlog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

const (
	preflightMinBytes       = 188 * 3
	preflightRelayMinBytes  = 188 * 10
	preflightTimeout        = 2 * time.Second
	streamRelayPortExpected = 17999
	maxEndedEntries         = 1000
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		// 8KB buffer for preflight checks
		return make([]byte, 8192)
	},
}

// vaapiEncodersToTest is the list of VAAPI encoders verified during preflight.
var vaapiEncodersToTest = []string{"h264_vaapi", "hevc_vaapi", "av1_vaapi"}

// LocalAdapter implements ports.MediaPipeline using local exec.Command.
type LocalAdapter struct {
	BinPath            string
	FFprobeBin         string
	HLSRoot            string
	AnalyzeDuration    string
	ProbeSize          string
	DVRWindow          time.Duration
	KillTimeout        time.Duration
	httpClient         *http.Client
	Logger             zerolog.Logger
	E2                 *enigma2.Client // Dependency for Tuner operations
	FallbackTo8001     bool
	PreflightTimeout   time.Duration
	SegmentSeconds     int
	StartTimeout       time.Duration
	StallTimeout       time.Duration
	VaapiDevice         string          // e.g. "/dev/dri/renderD128"; empty = no VAAPI
	DefaultClientCodecs []string        // Fallback codecs for clients without codec negotiation
	vaapiEncoders       map[string]bool // per-encoder preflight results ("h264_vaapi" -> true)
	vaapiDeviceChecked bool            // device-level preflight ran
	vaapiDeviceErr     error           // device-level preflight error
	Engine             *decision.Engine
	mu                 sync.Mutex
	// activeProcs maps run handles to running commands
	activeProcs map[ports.RunHandle]*exec.Cmd
	// stderrRings stores recent stderr lines for diagnostics (per-handle).
	stderrRings map[ports.RunHandle]*ringBuffer
	// ended stores last-known exit diagnostics for handles that have terminated.
	ended map[ports.RunHandle]procExitInfo
}

type procExitInfo struct {
	err        error
	watchdog   error
	stderrTail string
}

type squarePixelNormalization struct {
	Enabled   bool
	Width     int
	Height    int
	SourceSAR string
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
	if killTimeout <= 0 {
		killTimeout = 5 * time.Second
	}
	if segmentSeconds <= 0 {
		segmentSeconds = 4 // Best Practice 2026 Low Latency default
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
		BinPath:          binPath,
		FFprobeBin:       strings.TrimSpace(ffprobeBin),
		HLSRoot:          hlsRoot,
		AnalyzeDuration:  analyzeDuration,
		ProbeSize:        probeSize,
		DVRWindow:        dvrWindow,
		KillTimeout:      killTimeout,
		PreflightTimeout: preflightTimeout,
		SegmentSeconds:   segmentSeconds,
		httpClient:       httpClient,
		E2:               e2,
		Logger:           logger,
		FallbackTo8001:   fallbackTo8001,
		StartTimeout:     startTimeout,
		StallTimeout:     stallTimeout,
		VaapiDevice:      strings.TrimSpace(vaapiDevice),
		Engine:           decision.NewEngine(),
		activeProcs:      make(map[ports.RunHandle]*exec.Cmd),
		stderrRings:      make(map[ports.RunHandle]*ringBuffer),
		ended:            make(map[ports.RunHandle]procExitInfo),
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

	a.Logger.Info().Str(xlog.FieldDevice, a.VaapiDevice).Msg("vaapi preflight: starting (delegated to hardware package)")

	// Configure central hardware capability manager
	hardware.Configure(a.BinPath, a.VaapiDevice)

	// Trigger preflight (blocking, cached via sync.Once)
	verified := hardware.GetVerifiedVAAPIEncoders(context.Background())

	a.vaapiEncoders = verified
	a.vaapiDeviceChecked = true

	if len(verified) == 0 {
		a.vaapiDeviceErr = fmt.Errorf("vaapi preflight: no working VAAPI encoders found")
		// Logged by hardware package already
		return a.vaapiDeviceErr
	}

	a.Logger.Info().
		Str(xlog.FieldDevice, a.VaapiDevice).
		Int("verified_encoders", len(a.vaapiEncoders)).
		Msg("vaapi preflight: passed")
	return nil
}

// testVaapiEncoder runs a real 5-frame encode test for a specific VAAPI encoder.
func (a *LocalAdapter) testVaapiEncoder(encoder string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.BinPath,
		"-vaapi_device", a.VaapiDevice,
		"-f", "lavfi",
		"-i", "testsrc=duration=0.2:size=1280x720:rate=25",
		"-vf", "format=nv12,hwupload",
		"-c:v", encoder,
		"-frames:v", "5",
		"-f", "null", "-",
	)

	// Ensure process tree cleanup
	procgroup.Set(cmd)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("encode test failed: %w (output: %s)", err, string(out))
	}

	// Process is already reaped by CombinedOutput
	return nil
}

// VaapiEncoderVerified returns true if the given encoder passed preflight.
func (a *LocalAdapter) VaapiEncoderVerified(encoder string) bool {
	return a.vaapiEncoders[encoder]
}

// byteRingBuf for bounded stderr capture (memory safe)
type byteRingBuf struct {
	buf []byte
	cap int
}

func newByteRingBuf(capacity int) *byteRingBuf {
	return &byteRingBuf{
		buf: make([]byte, 0, capacity),
		cap: capacity,
	}
}

func (r *byteRingBuf) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// If p is larger than cap, just take the last cap bytes
	if len(p) >= r.cap {
		r.buf = append(r.buf[:0], p[len(p)-r.cap:]...)
		return len(p), nil
	}
	// If append fits
	if len(r.buf)+len(p) <= r.cap {
		r.buf = append(r.buf, p...)
		return len(p), nil
	}
	// Verify overflow logic: strictly limit memory usage
	needed := (len(r.buf) + len(p)) - r.cap
	r.buf = append(r.buf[needed:], p...)
	return len(p), nil
}

func (r *byteRingBuf) Bytes() []byte {
	return r.buf
}

func (r *byteRingBuf) Tail(lines int) string {
	b := r.Bytes()
	if len(b) == 0 {
		return ""
	}
	s := string(b)
	allLines := strings.Split(s, "\n")
	if len(allLines) <= lines {
		return s
	}
	return strings.Join(allLines[len(allLines)-lines:], "\n")
}

// Redaction helpers for forensic logging
var redactionRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(token|pass|auth|key|secret)[=:].+`),
	regexp.MustCompile(`(?i)\b(Bearer)\s+[A-Za-z0-9._-]+\b`),
}

func redactArg(val string) string {
	if val == "" {
		return val
	}
	// 1. Regex-based redaction for known patterns
	for _, re := range redactionRegexes {
		if re.MatchString(val) {
			return "[REDACTED]"
		}
	}

	// 2. URL-based redaction (handles query params)
	if strings.Contains(val, "://") {
		u, err := url.Parse(val)
		if err == nil {
			q := u.Query()
			changed := false
			for k, vv := range q {
				// Redact common auth keys
				kLower := strings.ToLower(k)
				if strings.Contains(kLower, "token") || strings.Contains(kLower, "auth") || strings.Contains(kLower, "key") || strings.Contains(kLower, "pass") {
					q.Set(k, "[REDACTED]")
					changed = true
					continue
				}
				// Panic brake: redact any extremely long values (likely session tokens/blobs)
				// Limitation: only applied to query parameters to avoid redacting long legitimate args
				for i, v := range vv {
					if len(v) > 128 {
						vv[i] = "[REDACTED_LONG]"
						changed = true
					}
				}
				if changed {
					q[k] = vv
				}
			}
			if changed {
				u.RawQuery = q.Encode()
				return u.String()
			}
		}
	}

	// 3. Fallback: if it looks like a bearer token but wasn't caught
	if len(val) > 50 && (strings.HasPrefix(strings.ToLower(val), "bearer") || strings.Contains(val, ".")) {
		// Heuristic: long strings with dots are often JWTs (Panic Brake)
		return "[REDACTED_TOKEN_HEURISTIC]"
	}

	return val
}

func redactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, redactArg(a))
	}
	return out
}

func cmdRepro(bin string, args []string) string {
	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, strconv.Quote(bin))
	for _, a := range args {
		quoted = append(quoted, strconv.Quote(a))
	}
	return strings.Join(quoted, " ")
}

// Start launches an ffmpeg process to produce the requested stream.
func (a *LocalAdapter) Start(ctx context.Context, spec ports.StreamSpec) (ports.RunHandle, error) {
	if !isValidSessionID(spec.SessionID) {
		return "", fmt.Errorf("invalid session ID: %q", spec.SessionID)
	}

	// Check remaining time for preflight
	deadline, ok := ctx.Deadline()
	if ok {
		remaining := time.Until(deadline)
		if remaining < 2*time.Second {
			return "", fmt.Errorf("insufficient time remaining for preflight: %v", remaining)
		}
		// Create preflight context with timeout
		preflightCtx, cancel := context.WithTimeout(ctx, remaining-500*time.Millisecond)
		defer cancel()
		// Use preflightCtx for preflight operations
		if err := a.runPreflight(preflightCtx, spec); err != nil {
			return "", err
		}
	} else {
		// No deadline, run preflight with default Context
		if err := a.runPreflight(ctx, spec); err != nil {
			return "", err
		}
	}

	// 0. Tune if required
	if spec.Source.Type == ports.SourceTuner && a.E2 != nil {
		if spec.Source.TunerSlot < 0 {
			return "", fmt.Errorf("invalid tuner slot: %d", spec.Source.TunerSlot)
		}
		// Create ephemeral tuner using legacy logic (reused)
		// We use a short timeout for the tune operation itself
		tuner := enigma2.NewTuner(a.E2, spec.Source.TunerSlot, 10*time.Second)

		// Use a detached context or the start context?
		// Start context is appropriate.
		if err := tuner.Tune(ctx, spec.Source.ID); err != nil {
			return "", fmt.Errorf("tuning failed: %w", err)
		}
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

		chosenURL, err := a.selectStreamURL(ctx, spec.SessionID, spec.Source.ID, streamURL)
		if err != nil {
			return "", err
		}
		inputURL = chosenURL
	case ports.SourceURL:
		inputURL = spec.Source.ID
	}

	// Fix 3: Add Context Timeout to Start logic (Preflight phase)
	// We want to ensure we don't spend too long in preflight/setup if the global context is tight.
	if dl, ok := ctx.Deadline(); ok {
		if time.Until(dl) < 500*time.Millisecond {
			return "", fmt.Errorf("insufficient time for start operation")
		}
	}

	// 1. Generate Arguments from Spec
	args, err := a.buildArgs(ctx, spec, inputURL)
	if err != nil {
		a.Logger.Error().
			Err(err).
			Str("sessionId", spec.SessionID).
			Str("event", "transcode.rejected").
			Str("decision.profile", spec.Profile.Name).
			Str("decision.output_codec", spec.Profile.VideoCodec).
			Msg("transcode request rejected: failed to build pipeline arguments")
		metrics.TranscodeRejectedTotal.WithLabelValues(spec.Profile.Name, "build_failure").Inc()
		return "", fmt.Errorf("failed to build args: %w", err)
	}

	logger := xlog.WithContext(ctx, a.Logger).With().Str(xlog.FieldSessionID, spec.SessionID).Logger()

	// FORENSICS: Prepare redacted command for logging
	redactedArgs := redactArgs(args)
	reproStr := cmdRepro(a.BinPath, redactedArgs)

	logger.Debug().
		Str("bin", a.BinPath).
		Strs("args_redacted", redactedArgs).
		Msg("starting ffmpeg process")

	// 2. Prepare Command
	// #nosec G204 - BinPath is trusted from config; args are generated by strict internal logic (buildArgs)
	cmd := exec.CommandContext(ctx, a.BinPath, args...)
	procgroup.Set(cmd) // Mandatory for tree reaping

	// Use ByteRingBuffer for bounded memory capture of stderr.
	// 64KB is sufficient for ~500-1000 lines of typical FFmpeg log output,
	// providing deep forensic context without risking memory exhaustion.
	ring := newByteRingBuf(64 * 1024)
	cmd.Stderr = ring
	cmd.Stdout = nil

	// 4. Start
	if err := cmd.Start(); err != nil {
		logger.Error().
			Err(err).
			Str("event", "ffmpeg.start_failed").
			Str("cmd_repro", reproStr).
			Msg("ffmpeg process failed to start")
		metrics.TranscodeRejectedTotal.WithLabelValues(spec.Profile.Name, "start_failure").Inc()
		return "", fmt.Errorf("ffmpeg start failed: %w", err)
	}

	logger.Info().
		Str("event", "ffmpeg.started").
		Str("cmd_repro", reproStr).
		Int("pid", cmd.Process.Pid).
		Msg("ffmpeg process started successfully")
	metrics.TranscodeStartedTotal.WithLabelValues(spec.Profile.Name).Inc()

	// 6. Monitor Process
	pid := cmd.Process.Pid
	handle := ports.RunHandle(fmt.Sprintf("%s-%d", spec.SessionID, pid))
	a.mu.Lock()
	a.activeProcs[handle] = cmd
	a.mu.Unlock()

	// Start watchdog monitor
	// Note: We pass the ring buffer for inspection after exit
	go a.monitorProcess(cmd, ring, spec.SessionID, handle, reproStr)

	// Metrics: Record pipeline spawn with cause="admitted" only AFTER successful start.
	// We use engine="ffmpeg" as per truth.
	metrics.RecordPipelineSpawn("ffmpeg", "admitted")
	return handle, nil
}

func (a *LocalAdapter) monitorProcess(cmd *exec.Cmd, ring *byteRingBuf, sessionID string, handle ports.RunHandle, cmdRepro string) {
	logger := a.Logger.With().Str(xlog.FieldSessionID, sessionID).Logger()
	// Stalling watchdog not actively fed in this hardening patch to prefer race-free ring buffer
	// wd := watchdog.New(a.StartTimeout, a.StallTimeout)

	a.mu.Lock()
	// If a handle is re-used (unlikely; includes pid), evict stale state.
	delete(a.ended, handle)
	// We no longer strictly need stderrRings map if we process exit here,
	// but keeping it if external access is needed.
	// a.stderrRings[handle] = ring
	a.mu.Unlock()

	// Wait for process or watchdog
	// Fix 2: Race Condition in Process Monitoring
	// Use a channel to signal process exit
	procDone := make(chan error, 1)
	go func() {
		procDone <- cmd.Wait()
	}()

	// Wait for exit
	var procErr error
	procErr = <-procDone

	// Check results
	exitCode := 0
	if procErr != nil {
		if exitErr, ok := procErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1 // Unknown/Signaled
		}
	}

	stderrTail := ring.Tail(200)

	// Remove from active maps
	a.mu.Lock()
	delete(a.activeProcs, handle)
	// active rings map...
	a.ended[handle] = procExitInfo{
		err:        procErr,
		watchdog:   nil, // Watchdog disabled in this patch
		stderrTail: stderrTail,
	}
	a.mu.Unlock()

	// Periodically clean up old ended entries
	if len(a.ended) > maxEndedEntries {
		go a.cleanupEndedMap()
	}

	// Log result
	if procErr != nil {
		logger.Error().
			Err(procErr).
			Str("stderr_tail", stderrTail).
			Str("event", "ffmpeg.exited").
			Int("exit_code", exitCode).
			Str("cmd_repro", cmdRepro).
			Msg("ffmpeg process failed")
	} else {
		logger.Info().
			Str("event", "ffmpeg.exited").
			Int("exit_code", 0).
			Msg("ffmpeg process finished successfully")
	}
}

func (a *LocalAdapter) cleanupEndedMap() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.ended) <= maxEndedEntries {
		return
	}

	// Remove oldest entries (simple FIFO via iteration)
	toRemove := len(a.ended) - maxEndedEntries
	removed := 0
	for handle := range a.ended {
		if removed >= toRemove {
			break
		}
		delete(a.ended, handle)
		removed++
	}
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

// Health checks if the process is running.
func (a *LocalAdapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()

	_, exists := a.activeProcs[handle]
	if !exists {
		if ended, ok := a.ended[handle]; ok {
			msg := "process ended"
			if ended.watchdog != nil {
				msg += ": watchdog timeout"
			} else if ended.err != nil {
				msg += ": " + ended.err.Error()
			}
			if len(ended.stderrTail) > 0 {
				msg += " (see logs: stderr_tail)"
			}
			return ports.HealthStatus{
				Healthy:   false,
				Message:   msg,
				LastCheck: time.Now(),
			}
		}
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process not found",
			LastCheck: time.Now(),
		}
	}

	return ports.HealthStatus{
		Healthy:   true,
		Message:   "process active",
		LastCheck: time.Now(),
	}
}

func sanitizeFFmpegArgsForLog(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i < len(out); i++ {
		if out[i] == "-i" && i+1 < len(out) {
			out[i+1] = sanitizeURLForLog(out[i+1])
			i++
			continue
		}
		if strings.HasPrefix(out[i], "http://") || strings.HasPrefix(out[i], "https://") {
			out[i] = sanitizeURLForLog(out[i])
		}
	}
	return out
}

type preflightResult struct {
	ok           bool
	bytes        int
	reason       string
	httpStatus   int
	latencyMs    int64
	resolvedPort int
}

type preflightFn func(context.Context, string) (preflightResult, error)

func (a *LocalAdapter) runPreflight(ctx context.Context, spec ports.StreamSpec) error {
	// This function encapsulates the preflight logic that was previously inline in Start.
	// It's called with a context that has a deadline for preflight operations.

	inputURL := ""
	switch spec.Source.Type {
	case ports.SourceTuner:
		if a.E2 == nil {
			return fmt.Errorf("tuner source requires enigma2 client")
		}
		streamURL, err := a.E2.ResolveStreamURL(ctx, spec.Source.ID)
		if err != nil {
			return fmt.Errorf("resolve stream url: %w", err)
		}
		streamURL = a.injectCredentialsIfAllowed(streamURL)

		chosenURL, err := a.selectStreamURL(ctx, spec.SessionID, spec.Source.ID, streamURL)
		if err != nil {
			return err
		}
		inputURL = chosenURL
	case ports.SourceURL:
		inputURL = spec.Source.ID
	case ports.SourceFile:
		// No preflight needed for local files
		return nil
	}

	// Perform preflight check on the chosen inputURL
	_, err := a.preflightTS(ctx, inputURL)
	if err != nil {
		return fmt.Errorf("preflight check failed for %s: %w", inputURL, err)
	}
	return nil
}

func (a *LocalAdapter) selectStreamURL(ctx context.Context, sessionID, serviceRef, streamURL string) (string, error) {
	// Fix 6: Validate SessionID input
	if !isValidSessionID(sessionID) {
		return "", fmt.Errorf("invalid session ID: %q", sessionID)
	}
	return a.selectStreamURLWithPreflight(ctx, sessionID, serviceRef, streamURL, a.preflightTS)
}

func isValidSessionID(id string) bool {
	if len(id) == 0 || len(id) > 100 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (a *LocalAdapter) selectStreamURLWithPreflight(ctx context.Context, sessionID, serviceRef, streamURL string, preflight preflightFn) (string, error) {
	logger := a.Logger.With().Str(xlog.FieldSessionID, sessionID).Str(xlog.FieldServiceRef, serviceRef).Logger()
	result, err := preflight(ctx, streamURL)
	reason := preflightReason(result, err)
	if err == nil && result.ok {
		return streamURL, nil
	}

	resolvedLogURL := sanitizeURLForLog(streamURL)
	isRelay := isStreamRelayURL(streamURL)
	if isRelay {
		logger.Warn().
			Str(xlog.FieldEvent, "streamrelay_preflight_failed").
			Str("resolved_url", resolvedLogURL).
			Int("preflight_bytes", result.bytes).
			Str("preflight_reason", reason).
			Int64("preflight_latency_ms", result.latencyMs).
			Int("http_status", result.httpStatus).
			Int("resolved_port", result.resolvedPort).
			Msg("streamrelay preflight failed")
	}

	if isRelay && a.FallbackTo8001 {
		fallbackURL, buildErr := buildFallbackURL(streamURL, serviceRef)
		if buildErr != nil {
			logger.Error().
				Str(xlog.FieldEvent, "preflight_failed_no_valid_ts").
				Str("resolved_url", resolvedLogURL).
				Int("preflight_bytes", result.bytes).
				Str("preflight_reason", "fallback_url_invalid").
				Int64("preflight_latency_ms", result.latencyMs).
				Int("http_status", result.httpStatus).
				Int("resolved_port", result.resolvedPort).
				Msg("preflight failed and fallback url was invalid")
			return "", &ports.PreflightError{Reason: "fallback_url_invalid"}
		}
		fallbackURL = a.injectCredentialsIfAllowed(fallbackURL)
		fallbackLogURL := sanitizeURLForLog(fallbackURL)
		logger.Warn().
			Str(xlog.FieldEvent, "fallback_to_8001_activated").
			Str("resolved_url", resolvedLogURL).
			Str("fallback_url", fallbackLogURL).
			Int("preflight_bytes", result.bytes).
			Str("preflight_reason", reason).
			Int64("preflight_latency_ms", result.latencyMs).
			Int("http_status", result.httpStatus).
			Int("resolved_port", result.resolvedPort).
			Msg("fallback to 8001 activated after streamrelay preflight failure")

		fallbackResult, fallbackErr := preflight(ctx, fallbackURL)
		if fallbackErr == nil && fallbackResult.ok {
			return fallbackURL, nil
		}
		a.Logger.Warn().Str("url", fallbackLogURL).Msg("fallback 8001 failed, trying original WebIF URL")

		// Fallback 2: Original WebIF URL (M3U)
		// Reconstruct standard OpenWebIF M3U URL
		// http://host/web/stream.m3u?ref=...
		if a.E2 != nil && a.E2.BaseURL != "" {
			u, _ := url.Parse(a.E2.BaseURL)
			u.Path = "/web/stream.m3u"
			q := u.Query()
			q.Set("ref", serviceRef)
			u.RawQuery = q.Encode()
			origURL := u.String()

			// Preflight for M3U: expect 200 OK, ignore TS sync
			origRes, origErr := preflight(ctx, origURL)
			if (origErr == nil && origRes.httpStatus == 200) || (origRes.httpStatus == 200 && origRes.bytes > 0) {
				a.Logger.Info().Str("url", sanitizeURLForLog(origURL)).Msg("fallback to original URL succeeded (M3U)")
				return origURL, nil
			}
		}

		logger.Error().
			Str(xlog.FieldEvent, "all_fallbacks_failed").
			Msg("all stream source fallbacks failed")
		// Return original error (or fallback error)
		return "", &ports.PreflightError{Reason: "fallback_failed_all"}
	}

	a.Logger.Error().
		Str("event", "preflight_failed_no_valid_ts").
		Str(xlog.FieldSessionID, sessionID).
		Str(xlog.FieldServiceRef, serviceRef).
		Str("resolved_url", resolvedLogURL).
		Int("preflight_bytes", result.bytes).
		Str("preflight_reason", reason).
		Int64("preflight_latency_ms", result.latencyMs).
		Int("http_status", result.httpStatus).
		Int("resolved_port", result.resolvedPort).
		Msg("preflight failed for resolved stream url")
	return "", &ports.PreflightError{Reason: reason}
}

func (a *LocalAdapter) preflightTS(ctx context.Context, rawURL string) (result preflightResult, err error) {
	start := time.Now()
	defer func() {
		latency := time.Since(start)
		result.latencyMs = latency.Milliseconds()
		metrics.ObservePreflightLatency(result.resolvedPort, latency)
	}()

	if strings.TrimSpace(rawURL) == "" {
		result.reason = "empty_url"
		return result, fmt.Errorf("preflight url empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		result.reason = "invalid_url"
		return result, err
	}

	port := parsed.Port()
	if port == "" {
		port = defaultPortForScheme(parsed.Scheme)
	}
	if port != "" {
		if portInt, portErr := strconv.Atoi(port); portErr == nil {
			result.resolvedPort = portInt
		}
	}

	timeout := a.PreflightTimeout
	if timeout <= 0 {
		timeout = preflightTimeout // fallback to legacy constant
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reqURL := *parsed
	user := reqURL.User
	reqURL.User = nil

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		result.reason = "request_build_failed"
		return result, err
	}
	if user != nil {
		username := user.Username()
		password, _ := user.Password()
		if username != "" || password != "" {
			req.SetBasicAuth(username, password)
		}
	}

	client := a.httpClient
	if client == nil {
		client = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				ResponseHeaderTimeout: timeout,
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		result.reason = "request_failed"
		return result, err
	}
	defer func() { _ = resp.Body.Close() }()

	result.httpStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		result.reason = fmt.Sprintf("http_status_%d", resp.StatusCode)
		return result, fmt.Errorf("preflight http status %d", resp.StatusCode)
	}

	// use a buffer pool to reduce allocations
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)

	// Ensure buffer is large enough
	if len(buf) < preflightRelayMinBytes {
		buf = make([]byte, preflightRelayMinBytes)
	}

	minToRead := preflightMinBytes
	if result.resolvedPort == streamRelayPortExpected {
		minToRead += preflightRelayMinBytes
	}

	n, err := io.ReadAtLeast(resp.Body, buf, minToRead)
	result.bytes = n

	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			if result.resolvedPort == streamRelayPortExpected {
				result.reason = "relay_unstable"
			} else {
				result.reason = "short_read"
			}
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			result.reason = "timeout"
		} else {
			result.reason = "read_failed"
		}
		return result, err
	}

	if !hasTSSync(buf) {
		result.reason = "sync_miss"
		return result, fmt.Errorf("preflight ts sync missing")
	}

	result.ok = true
	latency := time.Since(start)
	a.Logger.Info().
		Str("url", sanitizeURLForLog(rawURL)).
		Int("bytes", result.bytes).
		Dur("latency", latency).
		Int64("preflight_latency_ms", latency.Milliseconds()).
		Int("http_status", result.httpStatus).
		Int("resolved_port", result.resolvedPort).
		Msg("preflight read completed")

	result.ok = true
	return result, nil
}

func preflightReason(result preflightResult, err error) string {
	if result.reason != "" {
		return result.reason
	}
	if err != nil {
		return "request_failed"
	}
	return "unknown"
}

func hasTSSync(buf []byte) bool {
	if len(buf) < preflightMinBytes {
		return false
	}
	// TS streams don't always start on a packet boundary (e.g. relay/proxy quirks).
	// Accept sync at any alignment within the first packet to avoid false negatives.
	//
	// A valid alignment has sync bytes every 188 bytes for at least 3 packets.
	for off := 0; off < 188; off++ {
		if off+376 >= len(buf) {
			break
		}
		if buf[off] == 0x47 && buf[off+188] == 0x47 && buf[off+376] == 0x47 {
			return true
		}
	}
	return false
}

func buildFallbackURL(resolvedURL, serviceRef string) (string, error) {
	u, err := url.Parse(resolvedURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("missing host in resolved url")
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	u.Scheme = scheme
	u.Host = fmt.Sprintf("%s:%d", host, 8001)
	u.Path = "/" + serviceRef
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String(), nil
}

func isStreamRelayURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}
	return port == strconv.Itoa(streamRelayPortExpected)
}

func defaultPortForScheme(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func sanitizeURLForLog(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}

func (a *LocalAdapter) injectCredentialsIfAllowed(streamURL string) string {
	if a.E2 == nil {
		return streamURL
	}
	if a.E2.Username == "" && a.E2.Password == "" {
		return streamURL
	}

	u, err := url.Parse(streamURL)
	if err != nil {
		return streamURL
	}

	port := u.Port()
	if port == "" {
		port = defaultPortForScheme(u.Scheme)
	}

	if port == "80" || port == "443" || port == "8001" || port == "8002" {
		if a.E2.Username != "" {
			u.User = url.UserPassword(a.E2.Username, a.E2.Password)
		}
		return u.String()
	}
	return streamURL
}

func (a *LocalAdapter) buildArgs(ctx context.Context, spec ports.StreamSpec, inputURL string) ([]string, error) {
	// 1. Initial Metadata Collection & Probing
	relayInput := isStreamRelayURL(inputURL)
	inputCodec := "unknown"
	resolution := "unknown"
	var bitrateK int
	var interlaced bool
	var fps float64 = 30.0

	if spec.Source.Type == ports.SourceTuner || relayInput {
		fps = 25.0
	}

	if spec.SourceCap != nil {
		if spec.SourceCap.Codec != "" {
			inputCodec = normalizeProbeCodec(spec.SourceCap.Codec)
		}
		if spec.SourceCap.Resolution != "" && spec.SourceCap.Resolution != "0x0" {
			resolution = spec.SourceCap.Resolution
		}
		if spec.SourceCap.FPS > 0 {
			fps = spec.SourceCap.FPS
		}
		bitrateK = spec.SourceCap.BitrateK
		interlaced = spec.SourceCap.Interlaced
	}

	var squareNorm squarePixelNormalization
	// Dynamic Probing (supplements/overrides pre-scan when possible)
	if !relayInput || spec.Format == ports.FormatHLS {
		if detected, err := a.detectFPS(ctx, inputURL); err == nil && detected >= 15 && detected <= 120 {
			fps = float64(detected)
		}
		if w, h, sar, probedCodec, err := a.detectVideoGeometry(ctx, inputURL); err == nil {
			inputCodec = probedCodec
			if targetW, targetH, apply, _ := computeSquarePixelTarget(w, h, sar); apply {
				squareNorm = squarePixelNormalization{
					Enabled: true, Width: targetW, Height: targetH, SourceSAR: sar,
				}
			}
		}
	}

	// 2. Transcode Decision
	inputBitrateK := bitrateK
	if inputBitrateK == 0 {
		inputBitrateK = spec.Profile.VideoMaxRateK
	}

	// Normalize HWAccel intent from Resolve (which uses "vaapi") to Engine API ("force"/"auto"/"off")
	hwAccelMode := spec.Profile.HWAccel
	if hwAccelMode == "vaapi" {
		hwAccelMode = "force" // Resolve already checked availability + policy
	} else if hwAccelMode == "" {
		hwAccelMode = "auto"
	}

	decisionIn := decision.DecisionInput{
		InputCodec:      inputCodec,
		InputContainer:  "ts",
		BitrateKbps:     inputBitrateK,
		Resolution:      resolution,
		Framerate:       fps,
		Interlaced:      interlaced,
		ClientCodecs:    spec.ClientCodecs,
		SupportsHLSfMP4: strings.EqualFold(spec.Profile.Container, "fmp4"),
		SupportsTS:      strings.EqualFold(spec.Profile.Container, "ts") || spec.Profile.Container == "",
		Profile:         spec.Profile.Name,
		HWAccel:         hwAccelMode,
		ServerVAAPI:     hardware.IsVAAPIReady(),
		CPUUtilization:  spec.CPUUtilization,
		GPUUtilization:  spec.GPUUtilization,

		// Capabilities (Strict Gating - Phase 3.6)
		SupportedHWCodecs: hardware.GetSupportedHWCodecs(context.Background()),
		HWAccelAvailable:  hardware.IsVAAPIAvailable(context.Background()),
	}

	if len(decisionIn.ClientCodecs) == 0 {
		if len(a.DefaultClientCodecs) > 0 {
			decisionIn.ClientCodecs = a.DefaultClientCodecs
		} else {
			// Fail-safe default when client caps are unknown: prefer broad compatibility.
			// If a profile explicitly requests a target codec, seed with that preference.
			profileCodec := strings.ToLower(strings.TrimSpace(spec.Profile.VideoCodec))
			if profileCodec == "h265" {
				profileCodec = "hevc"
			}
			if profileCodec != "" {
				decisionIn.ClientCodecs = []string{profileCodec}
			} else {
				decisionIn.ClientCodecs = []string{"h264"}
			}
		}
	}

	decisionOut := a.Engine.Decide(decisionIn)
	resolvedOutputCodec := strings.ToLower(strings.TrimSpace(decisionOut.OutputCodec))
	if resolvedOutputCodec == "h265" {
		resolvedOutputCodec = "hevc"
	}
	if resolvedOutputCodec == "" {
		resolvedOutputCodec = strings.ToLower(strings.TrimSpace(spec.Profile.VideoCodec))
		if resolvedOutputCodec == "h265" {
			resolvedOutputCodec = "hevc"
		}
	}
	legacyProfile := spec.Profile.VideoCodec == "" && !spec.Profile.TranscodeVideo
	plannedSpec := spec
	if !legacyProfile && resolvedOutputCodec != "" {
		plannedSpec.Profile.VideoCodec = resolvedOutputCodec
	}
	if decisionOut.Container != "" {
		plannedSpec.Profile.Container = decisionOut.Container
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("decision.path", decisionOut.Path).
		Str("decision.reason", string(decisionOut.Reason)).
		Str("decision.output_codec", resolvedOutputCodec).
		Str("decision.container", decisionOut.Container).
		Msg("transcode decision completed")

	if decisionOut.Path == "rejected" {
		metrics.RecordTranscodeRejected(spec.Profile.Name, string(decisionOut.Reason))
		return nil, fmt.Errorf("transcode rejected: %s", decisionOut.Reason)
	}

	// 3. Command Synthesis
	var args []string

	// Global / Hardware Init (MUST COME BEFORE -i)
	if decisionOut.Path == "transcode_vaapi" {
		if a.VaapiDevice == "" {
			return nil, fmt.Errorf("no vaapi device configured")
		}
		// Final safety check: encoder must be verified
		if !a.vaapiEncoders[resolvedOutputCodec+"_vaapi"] {
			return nil, fmt.Errorf("vaapi encoder %s_vaapi not verified by preflight (device=%s)", resolvedOutputCodec, a.VaapiDevice)
		}
		args = append(args,
			"-vaapi_device", a.VaapiDevice,
			"-hwaccel", "vaapi",
			"-hwaccel_output_format", "vaapi",
		)
	}

	// Input Mapping
	fflags := "+genpts+discardcorrupt+flush_packets"
	baseInputArgs := []string{
		"-err_detect", "ignore_err",
		"-max_error_rate", "1.0",
		"-ignore_unknown",
	}
	if spec.Source.Type != ports.SourceFile {
		if !strings.Contains(fflags, "igndts") {
			fflags += "+igndts"
		}
		baseInputArgs = append(baseInputArgs,
			"-avoid_negative_ts", "make_zero",
			"-flags2", "+showall+export_mvs",
			"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",
			"-headers", "Icy-MetaData: 1\r\n",
		)
	}
	baseInputArgs = append([]string{"-fflags", fflags}, baseInputArgs...)
	if a.AnalyzeDuration != "" {
		baseInputArgs = append(baseInputArgs, "-analyzeduration", a.AnalyzeDuration)
	}
	if a.ProbeSize != "" {
		baseInputArgs = append(baseInputArgs, "-probesize", a.ProbeSize)
	}

	switch spec.Source.Type {
	case ports.SourceTuner, ports.SourceURL:
		if inputURL == "" {
			inputURL = spec.Source.ID
		}
		args = append(args, baseInputArgs...)
		// openwebif fix (non-relay): append reconnect flags for URLs
		if !relayInput {
			args = append(args,
				"-reconnect", "1", "-reconnect_at_eof", "1", "-reconnect_streamed", "1",
				"-reconnect_delay_max", "5", "-reconnect_on_network_error", "1",
				"-reconnect_on_http_error", "4xx,5xx",
			)
		}
		args = append(args, "-i", inputURL)
	case ports.SourceFile:
		args = append(args, baseInputArgs...)
		args = append(args, "-re", "-i", spec.Source.ID)
	}

	args = append(args, "-progress", "pipe:2")

	// Filter & Path Implementation
	segmentDurationSec := a.SegmentSeconds
	gop := int(fps) * segmentDurationSec

	// Auto-enable square pixel norm for fMP4 CPU if not already set
	if decisionOut.Path == "transcode_cpu" && decisionOut.Container == "fmp4" && !squareNorm.Enabled {
		squareNorm.Enabled = true
	}

	args = append(args, "-map", "0:v:0?", "-map", "0:a:0?")

	switch decisionOut.Path {
	case "direct", "remux":
		args = append(args, "-c:v", "copy")
	case "transcode_vaapi":
		args = a.buildVaapiVideoArgs(args, plannedSpec, gop, segmentDurationSec, squareNorm, inputCodec)
	case "transcode_cpu":
		args = a.buildCPUVideoArgs(args, plannedSpec, gop, segmentDurationSec, squareNorm, inputCodec)
	}

	// Audio & HLS Finalization
	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	args = append(args,
		"-c:a", "aac", "-b:a", audioBitrate, "-ac", "2", "-ar", "48000", "-sn", "-f", "hls",
	)

	segType := "mpegts"
	segExt := ".ts"
	if decisionOut.Container == "fmp4" {
		segType = "fmp4"
		segExt = ".m4s"
	}
	listSize := 10
	if a.DVRWindow > 0 {
		listSize = int(math.Ceil(a.DVRWindow.Seconds() / float64(segmentDurationSec)))
		if listSize < 3 {
			listSize = 3
		}
	}
	args = append(args,
		"-hls_time", strconv.Itoa(a.SegmentSeconds),
		"-hls_list_size", strconv.Itoa(listSize),
		"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
		"-hls_segment_type", segType,
		"-hls_segment_filename", filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "seg_%06d"+segExt),
	)
	if segType == "fmp4" {
		args = append(args, "-hls_fmp4_init_filename", "init.mp4")
	}

	outputPath := filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "stream.m3u8")
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	args = append(args, outputPath)

	// Fallback Output (Legacy AV1 check)
	if resolvedOutputCodec == "av1" {
		args = a.buildAV1FallbackOutput(args, spec, gop, segmentDurationSec, squareNorm, listSize)
	}

	return args, nil
}

// buildVaapiVideoArgs constructs video encoding arguments for the VAAPI GPU pipeline.
// Frames are already in GPU memory from -hwaccel vaapi -hwaccel_output_format vaapi.
func (a *LocalAdapter) buildVaapiVideoArgs(args []string, spec ports.StreamSpec, gop, segmentSec int, squareNorm squarePixelNormalization, inputCodec string) []string {
	prof := spec.Profile
	videoCodec := strings.ToLower(strings.TrimSpace(prof.VideoCodec))
	if videoCodec == "" {
		videoCodec = "h264"
	}
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("decision.path", "vaapi").
		Str("decision.reason", "profile_requested_vaapi").
		Str("decision.profile", prof.Name).
		Str("decision.input_codec", inputCodec).
		Str("decision.output_codec", videoCodec).
		Str("decision.container", prof.Container).
		Str("vaapi.device", a.VaapiDevice).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Bool("square_pixel_norm", squareNorm.Enabled).
		Msg("pipeline video: vaapi")

	filterChain := buildVAAPIFilterChain(prof.Deinterlace, squareNorm)
	if filterChain != "" {
		args = append(args, "-vf", filterChain)
	}

	// Encoder
	encoder := "h264_vaapi"
	switch videoCodec {
	case "hevc":
		encoder = "hevc_vaapi"
	case "av1":
		encoder = "av1_vaapi"
	}
	args = append(args, "-c:v", encoder)

	// Quality: VAAPI has no CRF; use VBR rate control from ProfileSpec
	if prof.VideoMaxRateK > 0 {
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", prof.VideoMaxRateK),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	} else {
		// Safe default when no rate specified
		args = append(args, "-global_quality", "23")
	}

	// GOP / keyframes (same logic as CPU path)
	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
	)

	// Closed GOP flag: only meaningful for H.264/HEVC, not AV1.
	if videoCodec != "av1" {
		args = append(args, "-flags", "+cgop")
	}

	args = append(args, "-profile:v", "main")

	// Apple/Safari compatibility: fMP4 HEVC should use hvc1 sample entry.
	if videoCodec == "hevc" && strings.EqualFold(prof.Container, "fmp4") {
		args = append(args, "-tag:v", "hvc1")
	}

	// AV1-specific: Safari/Apple hardware decoder compatibility.
	if videoCodec == "av1" {
		args = append(args,
			"-level", "4.0",
			"-tier", "main",
			"-tiles", "1x1",
			// Color metadata + temporal delimiter cleanup via bitstream filter.
			// NOTE: -color_primaries/-color_trc/-colorspace/-color_range CANNOT be used
			// as codec options with VAAPI — they trigger FFmpeg's auto_scale filter which
			// is incompatible with hardware surfaces. Use av1_metadata BSF instead.
			"-bsf:v", "av1_metadata=td=remove:color_primaries=1:transfer_characteristics=1:matrix_coefficients=1",
		)
	}

	return args
}

// buildAV1FallbackOutput appends a second FFmpeg output for an H.264 fMP4 rendition.
// This produces alt_stream.m3u8 / alt_init.mp4 / alt_seg_*.m4s alongside the primary AV1 output.
// The fallback bitrate is derived from the primary profile to ensure correct ABR ordering.
func (a *LocalAdapter) buildAV1FallbackOutput(args []string, spec ports.StreamSpec, gop, segmentSec int, squareNorm squarePixelNormalization, listSize int) []string {
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("fallback.codec", "h264").
		Msg("pipeline: appending H.264 fallback rendition for AV1 dual output")

	// Stream mapping (positional: applies to this output only)
	args = append(args, "-map", "0:v:0?", "-map", "0:a:0?")

	// Video filter chain (same deinterlace/scale as primary — codec-agnostic)
	filterChain := buildVAAPIFilterChain(spec.Profile.Deinterlace, squareNorm)
	if filterChain != "" {
		args = append(args, "-vf", filterChain)
	}

	// Derive fallback bitrate from primary: 80% of primary, capped at 5000k, floor at 1000k.
	fbMaxRateK := spec.Profile.VideoMaxRateK * 80 / 100
	if fbMaxRateK > 5000 {
		fbMaxRateK = 5000
	}
	if fbMaxRateK < 1000 {
		fbMaxRateK = 1000
	}
	fbBufSizeK := fbMaxRateK * 2

	// Video encoder: prefer h264_vaapi when available, fall back to libx264 (CPU).
	if spec.Profile.HWAccel == "vaapi" && a.vaapiEncoders["h264_vaapi"] {
		args = append(args,
			"-c:v", "h264_vaapi",
			"-b:v", fmt.Sprintf("%dk", fbMaxRateK),
			"-maxrate", fmt.Sprintf("%dk", fbMaxRateK),
			"-bufsize", fmt.Sprintf("%dk", fbBufSizeK),
			"-profile:v", "main",
			"-flags", "+cgop",
		)
	} else {
		args = append(args,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "20",
			"-maxrate", fmt.Sprintf("%dk", fbMaxRateK),
			"-bufsize", fmt.Sprintf("%dk", fbBufSizeK),
			"-profile:v", "main",
			"-flags", "+cgop",
		)
	}

	// GOP / keyframes (identical to primary for segment alignment)
	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
	)

	// Audio (same encoding as primary)
	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	args = append(args,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-ac", "2",
		"-ar", "48000",
		"-sn",
	)

	// HLS output (fMP4 container, flat naming with alt_ prefix)
	sessionDir := filepath.Join(a.HLSRoot, "sessions", spec.SessionID)
	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(a.SegmentSeconds),
		"-hls_list_size", strconv.Itoa(listSize),
		"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
		"-hls_segment_type", "fmp4",
		"-hls_segment_filename", filepath.Join(sessionDir, "alt_seg_%06d.m4s"),
		"-hls_fmp4_init_filename", "alt_init.mp4",
		filepath.Join(sessionDir, "alt_stream.m3u8"),
	)

	return args
}

func buildVAAPIFilterChain(deinterlace bool, av1SquareNorm squarePixelNormalization) string {
	filters := make([]string, 0, 2)
	if deinterlace {
		filters = append(filters, "deinterlace_vaapi")
	}
	if av1SquareNorm.Enabled {
		filters = append(filters, fmt.Sprintf("scale_vaapi=w=%d:h=%d:format=nv12", av1SquareNorm.Width, av1SquareNorm.Height))
	} else {
		// Ensure surface format is NV12 for encoder compatibility
		filters = append(filters, "scale_vaapi=format=nv12")
	}
	return strings.Join(filters, ",")
}

// buildCPUVideoArgs constructs video encoding arguments for the CPU transcode path.
// Handles both explicit ProfileSpec values and zero-valued ProfileSpec.
// When ProfileSpec is zero-valued (VideoCodec="" + TranscodeVideo=false), applies
// legacy defaults: libx264 + yadif + ultrafast + CRF 20. This ensures backwards
// compat for code paths that don't set ProfileSpec yet.
func (a *LocalAdapter) buildCPUVideoArgs(args []string, spec ports.StreamSpec, gop, segmentSec int, squareNorm squarePixelNormalization, inputCodec string) []string {
	prof := spec.Profile
	// Detect zero-valued profile → apply legacy defaults.
	// A zero-valued ProfileSpec has VideoCodec="" and TranscodeVideo=false,
	// which happens when the orchestrator doesn't pass a resolved profile.
	legacy := prof.VideoCodec == "" && !prof.TranscodeVideo

	codec := "libx264"
	resolvedCodec := strings.ToLower(strings.TrimSpace(prof.VideoCodec))
	if resolvedCodec == "h265" {
		resolvedCodec = "hevc"
	}
	preset := "ultrafast" // legacy default
	crf := 20
	deinterlace := true // legacy default: always deinterlace DVB streams

	if !legacy {
		if resolvedCodec == "hevc" {
			codec = "libx265"
		} else if resolvedCodec != "" && resolvedCodec != "h264" {
			codec = resolvedCodec
		}
		if prof.Preset != "" {
			preset = prof.Preset
		} else {
			preset = "superfast"
		}
		if prof.VideoCRF > 0 {
			crf = prof.VideoCRF
		}
		deinterlace = prof.Deinterlace
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("decision.path", "cpu").
		Str("decision.reason", "profile_requested_cpu_or_fallback").
		Str("decision.profile", prof.Name).
		Str("decision.input_codec", inputCodec).
		Str("decision.output_codec", codec).
		Str("decision.container", prof.Container).
		Int("video.crf", crf).
		Bool("deinterlace", deinterlace).
		Bool("square_pixel_norm", squareNorm.Enabled).
		Bool("legacy_defaults", legacy).
		Msg("pipeline video: cpu")

	filterChain := buildCPUFilterChain(deinterlace, squareNorm)
	if filterChain != "" {
		args = append(args, "-vf", filterChain)
	}

	args = append(args, "-c:v", codec)
	args = append(args, "-preset", preset)
	args = append(args, "-tune", "zerolatency")
	args = append(args, "-crf", strconv.Itoa(crf))

	if !legacy && prof.VideoMaxRateK > 0 {
		args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
	}

	if codec == "libx264" {
		args = append(args, "-x264-params", fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0", gop, gop))
	}
	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
		"-pix_fmt", "yuv420p",
		"-profile:v", "main",
	)
	if !legacy && resolvedCodec == "hevc" && strings.EqualFold(prof.Container, "fmp4") {
		args = append(args, "-tag:v", "hvc1")
	}
	return args
}

func buildCPUFilterChain(deinterlace bool, squareNorm squarePixelNormalization) string {
	filters := make([]string, 0, 3)
	if deinterlace {
		filters = append(filters, "yadif")
	}
	if squareNorm.Enabled {
		if squareNorm.Width > 0 && squareNorm.Height > 0 {
			filters = append(filters, fmt.Sprintf("scale=%d:%d", squareNorm.Width, squareNorm.Height))
		} else {
			// Dynamic square-pixel normalization from stream SAR:
			// square width = iw * sar, rounded to an even value.
			filters = append(filters, "scale=trunc(iw*sar/2)*2:ih")
		}
		filters = append(filters, "setsar=1")
	}
	return strings.Join(filters, ",")
}

func (a *LocalAdapter) detectFPS(ctx context.Context, inputURL string) (int, error) {
	// 1.5s rigid timeout for probe to avoid delaying startup
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	ffprobeBin := a.FFprobeBin
	if strings.TrimSpace(ffprobeBin) == "" {
		ffprobeBin = "ffprobe" // PATH fallback (last resort)
	}

	// #nosec G204 -- binPath is trusted
	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return 0, fmt.Errorf("empty output")
	}

	return parseFPS(output)
}

func (a *LocalAdapter) detectVideoGeometry(ctx context.Context, inputURL string) (int, int, string, string, error) {
	if strings.TrimSpace(inputURL) == "" {
		return 0, 0, "", "", fmt.Errorf("empty input url")
	}

	// Geometry probe needs a slightly larger budget than FPS probing on some
	// DVB/OpenWebIF sources; too-small timeouts cause persistent SAR misses.
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	ffprobeBin := a.FFprobeBin
	if strings.TrimSpace(ffprobeBin) == "" {
		ffprobeBin = "ffprobe" // PATH fallback (last resort)
	}

	// #nosec G204 -- binPath is trusted
	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,sample_aspect_ratio",
		"-of", "default=noprint_wrappers=1:nokey=1",
		inputURL,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0, 0, "", "", err
	}

	return parseVideoGeometryOutput(strings.TrimSpace(string(out)))
}

func parseFPS(output string) (int, error) {
	// Parse fraction "num/den" or "num"
	parts := strings.Split(output, "/")
	if len(parts) == 1 {
		val, err := strconv.Atoi(parts[0])
		return val, err
	}
	if len(parts) == 2 {
		num, err1 := strconv.Atoi(parts[0])
		den, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || den == 0 {
			return 0, fmt.Errorf("invalid fractional fps: %s", output)
		}
		// Round to nearest integer
		return int(math.Round(float64(num) / float64(den))), nil
	}

	return 0, fmt.Errorf("unrecognized fps format: %s", output)
}

func parseVideoGeometryOutput(output string) (int, int, string, string, error) {
	if strings.TrimSpace(output) == "" {
		return 0, 0, "", "", fmt.Errorf("empty output")
	}

	lines := strings.Split(output, "\n")
	tokens := make([]string, 0, len(lines))
	for _, line := range lines {
		token := strings.TrimSpace(line)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) < 4 {
		return 0, 0, "", "", fmt.Errorf("unexpected geometry output (expected 4 tokens: codec,w,h,sar): %q", output)
	}

	codec := normalizeProbeCodec(tokens[0])
	width, err := strconv.Atoi(tokens[1])
	if err != nil || width <= 0 {
		return 0, 0, "", "", fmt.Errorf("invalid width token: %q", tokens[1])
	}
	height, err := strconv.Atoi(tokens[2])
	if err != nil || height <= 0 {
		return 0, 0, "", "", fmt.Errorf("invalid height token: %q", tokens[2])
	}
	return width, height, tokens[3], codec, nil
}

func parseSampleAspectRatio(sar string) (int, int, error) {
	sar = strings.TrimSpace(strings.ToLower(sar))
	if sar == "" || sar == "n/a" {
		return 0, 0, fmt.Errorf("sample aspect ratio unavailable")
	}

	parts := strings.Split(sar, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid sample aspect ratio: %q", sar)
	}

	num, err1 := strconv.Atoi(parts[0])
	den, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || num <= 0 || den <= 0 {
		return 0, 0, fmt.Errorf("invalid sample aspect ratio: %q", sar)
	}
	return num, den, nil
}

func computeSquarePixelTarget(width, height int, sar string) (int, int, bool, error) {
	if width <= 0 || height <= 0 {
		return 0, 0, false, fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}

	num, den, err := parseSampleAspectRatio(sar)
	if err != nil {
		return 0, 0, false, err
	}
	if num == den {
		return 0, 0, false, nil
	}

	dar := (float64(width) * float64(num)) / (float64(height) * float64(den))
	targetW := int(math.Round(float64(height) * dar))
	if targetW < 2 {
		return 0, 0, false, fmt.Errorf("computed invalid square-pixel width: %d", targetW)
	}
	if targetW%2 != 0 {
		targetW++
	}
	if targetW == width {
		return 0, 0, false, nil
	}

	return targetW, height, true, nil
}

// normalizeProbeCodec handles codec aliases from ffprobe/container metadata.
// Standardizes on e.g. "hevc" instead of "hvc1" or "hev1".
func normalizeProbeCodec(codec string) string {
	codec = strings.ToLower(strings.TrimSpace(codec))
	switch codec {
	case "hvc1", "hev1", "h265":
		return "hevc"
	case "avc1":
		return "h264"
	case "mp4v":
		return "mpeg4"
	default:
		return codec
	}
}
