package ffmpeg

import (
	"bufio"
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
	"strconv"
	"strings"
	"sync"
	"time"

	codecdecision "github.com/ManuGH/xg2g/internal/decision"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/media/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

const (
	preflightMinBytes = 188 * 3
	preflightTimeout  = 2 * time.Second
)

// vaapiEncodersToTest is the list of VAAPI encoders verified during preflight.
var vaapiEncodersToTest = []string{"h264_vaapi", "hevc_vaapi"}

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
	VaapiDevice        string          // e.g. "/dev/dri/renderD128"; empty = no VAAPI
	vaapiEncoders      map[string]bool // per-encoder preflight results ("h264_vaapi" -> true)
	vaapiDeviceChecked bool            // device-level preflight ran
	vaapiDeviceErr     error           // device-level preflight error
	mu                 sync.Mutex
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
	if killTimeout <= 0 {
		killTimeout = 5 * time.Second
	}
	if segmentSeconds <= 0 {
		segmentSeconds = 6 // Keep in sync with config registry default (hls.segmentSeconds)
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
		activeProcs:      make(map[ports.RunHandle]*exec.Cmd),
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

	// 1. Generate Arguments from Spec
	args, err := a.buildArgs(ctx, spec, inputURL)
	if err != nil {
		return "", fmt.Errorf("failed to build args: %w", err)
	}

	// 2. Prepare Command
	// #nosec G204 - BinPath is trusted from config; args are generated by strict internal logic (buildArgs)
	cmd := exec.CommandContext(ctx, a.BinPath, args...)
	procgroup.Set(cmd) // Mandatory for tree reaping

	// 3. Setup Logging & Watchdog
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to pipe stderr: %w", err)
	}
	cmd.Stdout = nil

	// 4. Start
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ffmpeg start failed: %w", err)
	}

	// 6. Monitor Process
	pid := cmd.Process.Pid
	handle := ports.RunHandle(fmt.Sprintf("%s-%d", spec.SessionID, pid))
	a.mu.Lock()
	a.activeProcs[handle] = cmd
	a.mu.Unlock()

	// Start watchdog monitor
	go a.monitorProcess(ctx, handle, cmd, stderr, spec.SessionID)

	// Metrics: Record pipeline spawn with cause="admitted" only AFTER successful start.
	// We use engine="ffmpeg" as per truth.
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
		wd.ParseLine(line)

		// Map back to session metrics if needed, but primarily for stall detection

	}
	if scanErr := scanner.Err(); scanErr != nil {
		a.Logger.Warn().Err(scanErr).Str("sessionId", sessionID).Msg("ffmpeg stderr scan error")
	}

	// Wait for process or watchdog
	procErr := cmd.Wait()
	wdCancel()

	select {
	case wdErr := <-wdErrCh:
		if wdErr != nil {
			a.Logger.Error().Err(wdErr).Str("sessionId", sessionID).Msg("watchdog triggered process termination")
			// We don't have an easy way to signal the domain here other than the process exiting.
			// But since we are monitoring STDEER and it closed, cmd.Wait() will return.
		}
	default:
	}

	if procErr != nil {
		a.Logger.Debug().Err(procErr).Str("sessionId", sessionID).Msg("ffmpeg process exited")
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

	cmd, exists := a.activeProcs[handle]
	if !exists {
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process not found",
			LastCheck: time.Now(),
		}
	}
	if cmd == nil || cmd.Process == nil {
		delete(a.activeProcs, handle)
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process unavailable",
			LastCheck: time.Now(),
		}
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		delete(a.activeProcs, handle)
		return ports.HealthStatus{
			Healthy:   false,
			Message:   "process exited",
			LastCheck: time.Now(),
		}
	}

	return ports.HealthStatus{
		Healthy:   true,
		Message:   "process active",
		LastCheck: time.Now(),
	}
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

func (a *LocalAdapter) selectStreamURL(ctx context.Context, sessionID, serviceRef, streamURL string) (string, error) {
	return a.selectStreamURLWithPreflight(ctx, sessionID, serviceRef, streamURL, a.preflightTS)
}

func (a *LocalAdapter) selectStreamURLWithPreflight(ctx context.Context, sessionID, serviceRef, streamURL string, preflight preflightFn) (string, error) {
	result, err := preflight(ctx, streamURL)
	reason := preflightReason(result, err)
	if err == nil && result.ok {
		return streamURL, nil
	}

	resolvedLogURL := sanitizeURLForLog(streamURL)
	isRelay := isStreamRelayURL(streamURL)
	if isRelay {
		a.Logger.Warn().
			Str("event", "streamrelay_preflight_failed").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
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
			a.Logger.Error().
				Str("event", "preflight_failed_no_valid_ts").
				Str("sessionId", sessionID).
				Str("service_ref", serviceRef).
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
		a.Logger.Warn().
			Str("event", "fallback_to_8001_activated").
			Str("sessionId", sessionID).
			Str("service_ref", serviceRef).
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

		a.Logger.Error().
			Str("event", "all_fallbacks_failed").
			Str("sessionId", sessionID).
			Msg("all stream source fallbacks failed")
		// Return original error (or fallback error)
		return "", &ports.PreflightError{Reason: "fallback_failed_all"}
	}

	a.Logger.Error().
		Str("event", "preflight_failed_no_valid_ts").
		Str("sessionId", sessionID).
		Str("service_ref", serviceRef).
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

	buf := make([]byte, preflightMinBytes)
	n, err := io.ReadAtLeast(resp.Body, buf, preflightMinBytes)
	result.bytes = n

	latency := time.Since(start)
	a.Logger.Info().
		Str("url", sanitizeURLForLog(rawURL)).
		Int("bytes", n).
		Dur("latency", latency).
		Int64("preflight_latency_ms", latency.Milliseconds()).
		Int("http_status", result.httpStatus).
		Int("resolved_port", result.resolvedPort).
		Msg("preflight read completed")

	if err != nil {
		result.reason = "short_read"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			result.reason = "timeout"
		}
		return result, err
	}

	if !hasTSSync(buf) {
		result.reason = "sync_miss"
		return result, fmt.Errorf("preflight ts sync missing")
	}

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
	return buf[0] == 0x47 && buf[188] == 0x47 && buf[376] == 0x47
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
	return port == "17999"
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
	var args []string
	fflags := "+genpts+discardcorrupt+flush_packets"
	baseInputArgs := []string{
		"-err_detect", "ignore_err",
		"-max_error_rate", "1.0",
		"-ignore_unknown",
	}
	if spec.Source.Type != ports.SourceFile {
		// Stream Relay (/web) often has broken DTS/PTS; igndts + genpts will regenerate timestamps.
		// avoid_negative_ts prevents negative timestamps in HLS output (common with DVB corruption).
		if !strings.Contains(fflags, "igndts") {
			fflags += "+igndts"
		}
		baseInputArgs = append(baseInputArgs,
			"-avoid_negative_ts", "make_zero",
			"-flags2", "+showall+export_mvs",
			// OpenWebIF compatibility: VLC User-Agent + Icy-MetaData
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
	netInputArgs := append([]string{}, baseInputArgs...)
	netInputArgs = append(netInputArgs,
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
	)

	useHWPath := spec.Profile.HWAccel == "vaapi"
	if isPreferHWProfile(spec.Profile.Name) {
		useHWPath = true
	}

	hardVAAPIRequest := spec.Profile.HWAccel == "vaapi" && !isPreferHWProfile(spec.Profile.Name)
	decisionIn := codecdecision.Input{
		Profile:        spec.Profile.Name,
		RequestedCodec: spec.Profile.VideoCodec,
		RequireHW:      hardVAAPIRequest,
		Server: codecdecision.ServerCapabilities{
			HWAccelAvailable:  useHWPath,
			SupportedHWCodecs: a.supportedHWCodecs(),
		},
	}
	neg := codecdecision.Decide(decisionIn)
	decisionInSummary := decisionIn.Summary()
	decisionOutSummary := neg.Summary()
	metrics.RecordDecisionSummary(
		decisionInSummary.Profile,
		decisionOutSummary.Path,
		decisionOutSummary.OutputCodec,
		decisionOutSummary.UseHWAccel,
		decisionOutSummary.Reason,
	)

	a.Logger.Info().
		Str("event", "decision.summary").
		Str("profile", decisionInSummary.Profile).
		Str("requested_codec", decisionInSummary.RequestedCodec).
		Strs("supported_hw_codecs", decisionInSummary.SupportedHWCodecs).
		Bool("hwaccel_available", decisionInSummary.HWAccelAvailable).
		Str("path", decisionOutSummary.Path).
		Str("output_codec", decisionOutSummary.OutputCodec).
		Bool("use_hwaccel", decisionOutSummary.UseHWAccel).
		Str("reason", decisionOutSummary.Reason).
		Msg("decision summary")

	if neg.Path == codecdecision.PathReject && !hardVAAPIRequest {
		return nil, fmt.Errorf("codec negotiation rejected (profile=%s codec=%s reason=%s)", spec.Profile.Name, spec.Profile.VideoCodec, neg.Reason)
	}
	resolvedCodec := neg.OutputCodec
	if resolvedCodec == "" && hardVAAPIRequest {
		resolvedCodec = normalizeRequestedCodec(spec.Profile.VideoCodec)
	}
	if resolvedCodec == "" {
		resolvedCodec = "h264"
	}
	useVAAPI := neg.Path == codecdecision.PathTranscodeHW
	if hardVAAPIRequest {
		useVAAPI = true
	}

	// VAAPI device init (must come before -i for hwaccel decode).
	// Fail-closed: two independent checks must pass before VAAPI args are emitted.
	if useVAAPI {
		if a.VaapiDevice == "" {
			return nil, fmt.Errorf("vaapi requested by profile but no vaapi device configured on adapter")
		}
		// Resolve the encoder name for per-encoder preflight check
		reqEncoder, ok := codecToVAAPIEncoder(resolvedCodec)
		if !ok {
			return nil, fmt.Errorf("unsupported vaapi codec resolved by decision engine: %s", resolvedCodec)
		}
		if !a.VaapiEncoderVerified(reqEncoder) {
			return nil, fmt.Errorf("vaapi encoder %s not verified by preflight (device=%s, deviceErr=%v)", reqEncoder, a.VaapiDevice, a.vaapiDeviceErr)
		}
		args = append(args,
			"-vaapi_device", a.VaapiDevice,
			"-hwaccel", "vaapi",
			"-hwaccel_output_format", "vaapi",
		)
	}

	// Input
	switch spec.Source.Type {
	case ports.SourceTuner:
		if inputURL == "" {
			return nil, fmt.Errorf("missing stream url for tuner source")
		}
		args = append(args, netInputArgs...)
		args = append(args, "-i", inputURL)
	case ports.SourceURL:
		if inputURL == "" {
			inputURL = spec.Source.ID
		}
		args = append(args, netInputArgs...)
		args = append(args, "-i", inputURL)
	case ports.SourceFile:
		args = append(args, baseInputArgs...)
		args = append(args, "-re", "-i", spec.Source.ID)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}

	args = append(args, "-progress", "pipe:2")

	if spec.Mode == ports.ModeLive {
		segmentDurationSec := a.SegmentSeconds

		// Detect FPS with fallback strategies
		// Default: 30 (Generic/NTSC), Fallback for Tuner/Relay: 25 (DVB/PAL)
		fps := 30
		if spec.Source.Type == ports.SourceTuner || isStreamRelayURL(inputURL) {
			fps = 25
		}

		// Attempt dynamic detection (Best Practice 2026: Input-Robustness)
		if detected, err := a.detectFPS(ctx, inputURL); err == nil && detected >= 15 && detected <= 120 {
			fps = detected
			a.Logger.Debug().Str("sessionId", spec.SessionID).Int("fps", fps).Str("url", sanitizeURLForLog(inputURL)).Msg("detected input fps")
		} else {
			a.Logger.Warn().Str("sessionId", spec.SessionID).Err(err).Int("fallback_fps", fps).Str("url", sanitizeURLForLog(inputURL)).Msg("fps detection failed, using fallback")
		}

		gop := fps * segmentDurationSec

		listSize := 10
		if a.DVRWindow > 0 {
			listSize = int(math.Ceil(a.DVRWindow.Seconds() / float64(segmentDurationSec)))
			if listSize < 3 {
				listSize = 3 // Minimum for stable playback
			}
		}

		// Stream mapping (same for all paths)
		// Use -sn to disable subtitles/teletext, as they often cause "Invalid data" errors
		// during transcoding (mapped to WebVTT by default) with Stream Relay sources.
		args = append(args,
			"-map", "0:v:0?",
			"-map", "0:a:0?",
		)

		// Video encoding (two paths: VAAPI GPU or CPU)
		if useVAAPI {
			args = a.buildVaapiVideoArgs(args, spec, resolvedCodec, gop, segmentDurationSec)
		} else {
			args = a.buildCPUVideoArgs(args, spec, resolvedCodec, gop, segmentDurationSec)
		}

		// Audio (same for all paths)
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
			"-f", "hls",
		)

		// HLS segment configuration (Best Practice 2026)
		// - hls_time: 6s segments (industry standard for live streaming)
		// - hls_list_size: DVR window in segments (default 10)
		// - hls_flags: delete old segments + append list + independent segments + program date time
		// - hls_segment_type: mpegts for compatibility
		args = append(args,
			"-hls_time", strconv.Itoa(a.SegmentSeconds),
			"-hls_list_size", strconv.Itoa(listSize),
			"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
			"-hls_segment_type", "mpegts",
			"-hls_segment_filename", filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "seg_%06d.ts"),
		)

		outputPath := filepath.Join(a.HLSRoot, "sessions", spec.SessionID, "index.m3u8")
		_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
		args = append(args, outputPath)
	}

	return args, nil
}

// buildVaapiVideoArgs constructs video encoding arguments for the VAAPI GPU pipeline.
// Frames are already in GPU memory from -hwaccel vaapi -hwaccel_output_format vaapi.
func (a *LocalAdapter) buildVaapiVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi")

	// Filter: frames are VAAPI surfaces from hwaccel decode
	if prof.Deinterlace {
		args = append(args, "-vf", "deinterlace_vaapi")
	}

	// Encoder
	encoder := "h264_vaapi"
	switch outputCodec {
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
		"-flags", "+cgop",
	)

	args = append(args, "-profile:v", "main")
	return args
}

// buildCPUVideoArgs constructs video encoding arguments for the CPU transcode path.
// Handles both explicit ProfileSpec values and zero-valued ProfileSpec.
// When ProfileSpec is zero-valued (VideoCodec="" + TranscodeVideo=false), applies
// legacy defaults: libx264 + yadif + ultrafast + CRF 20. This ensures backwards
// compat for code paths that don't set ProfileSpec yet.
func (a *LocalAdapter) buildCPUVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	// Detect zero-valued profile → apply legacy defaults.
	// A zero-valued ProfileSpec has VideoCodec="" and TranscodeVideo=false,
	// which happens when the orchestrator doesn't pass a resolved profile.
	legacy := prof.VideoCodec == "" && !prof.TranscodeVideo && outputCodec == "h264"

	codec := "libx264"
	preset := "ultrafast" // legacy default
	crf := 20
	deinterlace := true // legacy default: always deinterlace DVB streams

	if !legacy {
		if outputCodec == "hevc" {
			codec = "libx265"
		} else if outputCodec == "av1" {
			codec = "libsvtav1"
		} else if outputCodec != "" && outputCodec != "h264" {
			codec = outputCodec
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
		Str("transcode.mode", "cpu").
		Str("video.codec", codec).
		Int("video.crf", crf).
		Bool("deinterlace", deinterlace).
		Bool("legacy_defaults", legacy).
		Msg("pipeline video: cpu")

	if deinterlace {
		args = append(args, "-vf", "yadif")
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
	return args
}

func (a *LocalAdapter) supportedHWCodecs() []string {
	codecs := make([]string, 0, 3)
	if a.VaapiEncoderVerified("h264_vaapi") {
		codecs = append(codecs, "h264")
	}
	if a.VaapiEncoderVerified("hevc_vaapi") {
		codecs = append(codecs, "hevc")
	}
	if a.VaapiEncoderVerified("av1_vaapi") {
		codecs = append(codecs, "av1")
	}
	return codecs
}

func isPreferHWProfile(profileName string) bool {
	p := strings.ToLower(strings.TrimSpace(profileName))
	return p == "av1_hw" || strings.HasSuffix(p, "_hw") || strings.HasSuffix(p, "_hw_ll")
}

func codecToVAAPIEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		return "h264_vaapi", true
	case "hevc":
		return "hevc_vaapi", true
	case "av1":
		return "av1_vaapi", true
	default:
		return "", false
	}
}

func normalizeRequestedCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch c {
	case "", "h264", "avc", "avc1", "libx264", "h264_vaapi":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return c
	}
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
