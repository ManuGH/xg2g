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
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/media/ffmpeg/watchdog"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/exec/enigma2"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/procgroup"
	"github.com/rs/zerolog"
)

const (
	// safariDirtyHLSTimeSec reduces first-frame latency for dirty live sources while
	// preserving stable 2-second GOP/segment alignment.
	safariDirtyHLSTimeSec = 2
	// safariDirtyHLSInitTimeSec allows a shorter startup segment before steady-state.
	safariDirtyHLSInitTimeSec = 1
	// Relative startup-cost thresholds for automatic codec promotion above H.264.
	defaultHEVCVAAPIAutoRatioMax = 1.75
	defaultAV1VAAPIAutoRatioMax  = 2.50
	defaultHEVCNVENCAutoRatioMax = 1.75
	defaultAV1NVENCAutoRatioMax  = 2.50
)

// vaapiEncodersToTest is the list of VAAPI encoders verified during preflight.
var vaapiEncodersToTest = []string{"h264_vaapi", "hevc_vaapi", "av1_vaapi"}

// nvencEncodersToTest is the list of NVENC encoders verified during preflight.
var nvencEncodersToTest = []string{"h264_nvenc", "hevc_nvenc", "av1_nvenc"}

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
	vaapiEncoderCaps          map[string]hardware.VAAPIEncoderCapability
	vaapiDeviceChecked        bool            // device-level preflight ran
	vaapiDeviceErr            error           // device-level preflight error
	nvencEncoders             map[string]bool // per-encoder preflight results ("h264_nvenc" -> true)
	nvencEncoderCaps          map[string]hardware.NVENCEncoderCapability
	nvencChecked              bool
	nvencErr                  error
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
	// finalizedProfiles keeps the finalized profile that actually launched for a handle.
	finalizedProfiles map[ports.RunHandle]ports.ProfileSpec
	// processDetails keeps the last meaningful failure summary for a handle.
	processDetails map[ports.RunHandle]string
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
		finalizedProfiles:         make(map[ports.RunHandle]ports.ProfileSpec),
		processDetails:            make(map[ports.RunHandle]string),
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
	a.vaapiEncoderCaps = make(map[string]hardware.VAAPIEncoderCapability)
	a.vaapiDeviceChecked = true

	a.Logger.Info().Str("device", a.VaapiDevice).Msg("vaapi preflight: starting")

	// 1. Device accessible
	if _, err := os.Stat(a.VaapiDevice); err != nil {
		a.vaapiDeviceErr = fmt.Errorf("vaapi device not accessible: %w", err)
		a.Logger.Error().Err(a.vaapiDeviceErr).Str("device", a.VaapiDevice).Msg("vaapi preflight: device stat failed")
		hardware.SetVAAPIEncoderCapabilities(nil)
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
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
		return a.vaapiDeviceErr
	}
	encoderList := string(checkOut)

	// 3. Test each encoder with a real 5-frame encode
	verifiedElapsed := make(map[string]time.Duration, len(vaapiEncodersToTest))
	for _, enc := range vaapiEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			a.Logger.Info().Str("encoder", enc).Msg("vaapi preflight: encoder not in ffmpeg build, skipping")
			continue
		}
		elapsed, err := a.testVaapiEncoder(enc)
		if err != nil {
			a.Logger.Warn().Err(err).Str("encoder", enc).Msg("vaapi preflight: encoder test failed")
		} else {
			a.vaapiEncoders[enc] = true
			verifiedElapsed[enc] = elapsed
			a.Logger.Info().
				Str("encoder", enc).
				Dur("probe_elapsed", elapsed).
				Msg("vaapi preflight: encoder verified")
		}
	}

	if len(a.vaapiEncoders) == 0 {
		a.vaapiDeviceErr = fmt.Errorf("vaapi preflight: no working VAAPI encoders found")
		a.Logger.Error().Err(a.vaapiDeviceErr).Msg("vaapi preflight: failed")
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
		return a.vaapiDeviceErr
	}

	a.vaapiEncoderCaps = deriveVAAPIEncoderCapabilities(
		verifiedElapsed,
		envFloatBounded("XG2G_HEVC_VAAPI_AUTO_RATIO_MAX", defaultHEVCVAAPIAutoRatioMax, 1.0, 10.0),
		envFloatBounded("XG2G_AV1_VAAPI_AUTO_RATIO_MAX", defaultAV1VAAPIAutoRatioMax, 1.0, 10.0),
	)
	for _, enc := range vaapiEncodersToTest {
		cap, ok := a.vaapiEncoderCaps[enc]
		if !ok || !cap.Verified {
			continue
		}
		a.Logger.Info().
			Str("encoder", enc).
			Dur("probe_elapsed", cap.ProbeElapsed).
			Bool("auto_eligible", cap.AutoEligible).
			Msg("vaapi preflight: encoder capability")
	}

	// Publish per-encoder results for higher layers (HTTP/profile selection).
	hardware.SetVAAPIEncoderCapabilities(a.vaapiEncoderCaps)

	hardware.SetVAAPIPreflightResult(true)
	a.Logger.Info().
		Str("device", a.VaapiDevice).
		Int("verified_encoders", len(a.vaapiEncoders)).
		Msg("vaapi preflight: passed")
	return nil
}

// PreflightNVENC validates that the visible NVIDIA runtime can execute real NVENC encodes.
func (a *LocalAdapter) PreflightNVENC() error {
	if !hardware.HasNVENC() {
		return nil
	}
	if a.nvencChecked {
		return a.nvencErr
	}

	a.nvencEncoders = make(map[string]bool)
	a.nvencEncoderCaps = make(map[string]hardware.NVENCEncoderCapability)
	a.nvencChecked = true

	a.Logger.Info().Msg("nvenc preflight: starting")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- BinPath is trusted from config
	checkCmd := exec.CommandContext(ctx, a.BinPath, "-hide_banner", "-encoders")
	checkOut, err := checkCmd.Output()
	if err != nil {
		a.nvencErr = fmt.Errorf("nvenc preflight: ffmpeg -encoders failed: %w", err)
		a.Logger.Error().Err(a.nvencErr).Msg("nvenc preflight: encoder check failed")
		hardware.SetNVENCEncoderCapabilities(nil)
		hardware.SetNVENCPreflightResult(false)
		return a.nvencErr
	}
	encoderList := string(checkOut)

	verifiedElapsed := make(map[string]time.Duration, len(nvencEncodersToTest))
	for _, enc := range nvencEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			a.Logger.Info().Str("encoder", enc).Msg("nvenc preflight: encoder not in ffmpeg build, skipping")
			continue
		}
		elapsed, err := a.testNVENCEncoder(enc)
		if err != nil {
			a.Logger.Warn().Err(err).Str("encoder", enc).Msg("nvenc preflight: encoder test failed")
		} else {
			a.nvencEncoders[enc] = true
			verifiedElapsed[enc] = elapsed
			a.Logger.Info().
				Str("encoder", enc).
				Dur("probe_elapsed", elapsed).
				Msg("nvenc preflight: encoder verified")
		}
	}

	if len(a.nvencEncoders) == 0 {
		a.nvencErr = fmt.Errorf("nvenc preflight: no working NVENC encoders found")
		a.Logger.Error().Err(a.nvencErr).Msg("nvenc preflight: failed")
		hardware.SetNVENCEncoderCapabilities(nil)
		hardware.SetNVENCPreflightResult(false)
		return a.nvencErr
	}

	a.nvencEncoderCaps = deriveNVENCEncoderCapabilities(
		verifiedElapsed,
		envFloatBounded("XG2G_HEVC_NVENC_AUTO_RATIO_MAX", defaultHEVCNVENCAutoRatioMax, 1.0, 10.0),
		envFloatBounded("XG2G_AV1_NVENC_AUTO_RATIO_MAX", defaultAV1NVENCAutoRatioMax, 1.0, 10.0),
	)
	for _, enc := range nvencEncodersToTest {
		cap, ok := a.nvencEncoderCaps[enc]
		if !ok || !cap.Verified {
			continue
		}
		a.Logger.Info().
			Str("encoder", enc).
			Dur("probe_elapsed", cap.ProbeElapsed).
			Bool("auto_eligible", cap.AutoEligible).
			Msg("nvenc preflight: encoder capability")
	}

	hardware.SetNVENCEncoderCapabilities(a.nvencEncoderCaps)
	hardware.SetNVENCPreflightResult(true)
	a.Logger.Info().
		Int("verified_encoders", len(a.nvencEncoders)).
		Msg("nvenc preflight: passed")
	return nil
}

// testVaapiEncoder runs a real 5-frame encode test for a specific VAAPI encoder.
func (a *LocalAdapter) testVaapiEncoder(encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
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
		return 0, fmt.Errorf("encode test failed: %w (output: %s)", err, string(out))
	}
	return time.Since(start), nil
}

func (a *LocalAdapter) testNVENCEncoder(encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	// #nosec G204 -- BinPath is trusted from config
	cmd := exec.CommandContext(ctx, a.BinPath,
		"-f", "lavfi",
		"-i", "testsrc=duration=0.2:size=1280x720:rate=25",
		"-c:v", encoder,
		"-frames:v", "5",
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("encode test failed: %w (output: %s)", err, string(out))
	}
	return time.Since(start), nil
}

// VaapiEncoderVerified returns true if the given encoder passed preflight.
func (a *LocalAdapter) VaapiEncoderVerified(encoder string) bool {
	return a.vaapiEncoders[encoder]
}

// VaapiEncoderAutoEligible returns true if the encoder is verified and suitable
// for generic automatic codec selection on this host.
func (a *LocalAdapter) VaapiEncoderAutoEligible(encoder string) bool {
	cap, ok := a.vaapiEncoderCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

func (a *LocalAdapter) NVENCEncoderVerified(encoder string) bool {
	return a.nvencEncoders[encoder]
}

func (a *LocalAdapter) NVENCEncoderAutoEligible(encoder string) bool {
	cap, ok := a.nvencEncoderCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

func deriveVAAPIEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.VAAPIEncoderCapability {
	return deriveHardwareEncoderCapabilities(samples, hevcRatioMax, av1RatioMax)
}

func deriveNVENCEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.NVENCEncoderCapability {
	return deriveHardwareEncoderCapabilities(samples, hevcRatioMax, av1RatioMax)
}

func deriveHardwareEncoderCapabilities(samples map[string]time.Duration, hevcRatioMax, av1RatioMax float64) map[string]hardware.HardwareEncoderCapability {
	if len(samples) == 0 {
		return nil
	}

	caps := make(map[string]hardware.HardwareEncoderCapability, len(samples))
	baseline, ok := selectHardwareAutoBaseline(samples)
	if !ok {
		return caps
	}

	for encoder, elapsed := range samples {
		cap := hardware.HardwareEncoderCapability{
			Verified:     true,
			ProbeElapsed: elapsed,
			AutoEligible: strings.HasPrefix(encoder, "h264_"),
		}
		if !cap.AutoEligible {
			ratio := float64(elapsed) / float64(baseline)
			switch encoder {
			case "hevc_vaapi", "hevc_nvenc":
				cap.AutoEligible = ratio <= hevcRatioMax
			case "av1_vaapi", "av1_nvenc":
				cap.AutoEligible = ratio <= av1RatioMax
			default:
				cap.AutoEligible = true
			}
		}
		caps[encoder] = cap
	}

	return caps
}

func selectVAAPIAutoBaseline(samples map[string]time.Duration) (time.Duration, bool) {
	return selectHardwareAutoBaseline(samples)
}

func selectHardwareAutoBaseline(samples map[string]time.Duration) (time.Duration, bool) {
	for _, key := range []string{"h264_vaapi", "h264_nvenc"} {
		if elapsed, ok := samples[key]; ok && elapsed > 0 {
			return elapsed, true
		}
	}
	var baseline time.Duration
	for _, elapsed := range samples {
		if elapsed <= 0 {
			continue
		}
		if baseline == 0 || elapsed < baseline {
			baseline = elapsed
		}
	}
	return baseline, baseline > 0
}

func (a *LocalAdapter) hardwareEncoderVerified(backend profiles.GPUBackend, encoder string) bool {
	switch backend {
	case profiles.GPUBackendVAAPI:
		return a.VaapiEncoderVerified(encoder)
	case profiles.GPUBackendNVENC:
		return a.NVENCEncoderVerified(encoder)
	default:
		return false
	}
}

func (a *LocalAdapter) hardwareEncoderAutoEligible(backend profiles.GPUBackend, encoder string) bool {
	switch backend {
	case profiles.GPUBackendVAAPI:
		return a.VaapiEncoderAutoEligible(encoder)
	case profiles.GPUBackendNVENC:
		return a.NVENCEncoderAutoEligible(encoder)
	default:
		return false
	}
}

func (a *LocalAdapter) hardwareEncoderCapability(backend profiles.GPUBackend, encoder string) (hardware.HardwareEncoderCapability, bool) {
	switch backend {
	case profiles.GPUBackendVAAPI:
		cap, ok := a.vaapiEncoderCaps[encoder]
		return cap, ok
	case profiles.GPUBackendNVENC:
		cap, ok := a.nvencEncoderCaps[encoder]
		return cap, ok
	default:
		return hardware.HardwareEncoderCapability{}, false
	}
}

func (a *LocalAdapter) preferredHardwareBackendForCodec(codec string) profiles.GPUBackend {
	var (
		bestBackend profiles.GPUBackend
		bestCap     hardware.HardwareEncoderCapability
		ok          bool
	)
	for _, backend := range []profiles.GPUBackend{profiles.GPUBackendVAAPI, profiles.GPUBackendNVENC} {
		encoder, exists := encoderNameForBackend(codec, backend)
		if !exists || !a.hardwareEncoderVerified(backend, encoder) {
			continue
		}
		cap, exists := a.hardwareEncoderCapability(backend, encoder)
		if !exists {
			cap = hardware.HardwareEncoderCapability{Verified: true}
		}
		if !ok || betterLocalHardwareCapability(backend, cap, bestBackend, bestCap) {
			bestBackend = backend
			bestCap = cap
			ok = true
		}
	}
	if !ok {
		return profiles.GPUBackendNone
	}
	return bestBackend
}

func betterLocalHardwareCapability(candidateBackend profiles.GPUBackend, candidateCap hardware.HardwareEncoderCapability, bestBackend profiles.GPUBackend, bestCap hardware.HardwareEncoderCapability) bool {
	if candidateCap.AutoEligible != bestCap.AutoEligible {
		return candidateCap.AutoEligible
	}

	candidateMeasured := candidateCap.ProbeElapsed > 0
	bestMeasured := bestCap.ProbeElapsed > 0
	if candidateMeasured != bestMeasured {
		return candidateMeasured
	}
	if candidateMeasured && candidateCap.ProbeElapsed != bestCap.ProbeElapsed {
		return candidateCap.ProbeElapsed < bestCap.ProbeElapsed
	}

	switch candidateBackend {
	case profiles.GPUBackendVAAPI:
		return bestBackend != profiles.GPUBackendVAAPI
	case profiles.GPUBackendNVENC:
		return bestBackend == profiles.GPUBackendNone
	default:
		return false
	}
}

func encoderNameForBackend(codec string, backend profiles.GPUBackend) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "h264_vaapi", true
		case profiles.GPUBackendNVENC:
			return "h264_nvenc", true
		}
	case "hevc":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "hevc_vaapi", true
		case profiles.GPUBackendNVENC:
			return "hevc_nvenc", true
		}
	case "av1":
		switch backend {
		case profiles.GPUBackendVAAPI:
			return "av1_vaapi", true
		case profiles.GPUBackendNVENC:
			return "av1_nvenc", true
		}
	}
	return "", false
}

func (a *LocalAdapter) supportedHWCodecsLocal() []string {
	codecs := make([]string, 0, 3)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		if a.preferredHardwareBackendForCodec(codec) != profiles.GPUBackendNone {
			codecs = append(codecs, codec)
		}
	}
	return codecs
}

func (a *LocalAdapter) autoHWCodecsLocal() []string {
	codecs := make([]string, 0, 3)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		for _, backend := range []profiles.GPUBackend{profiles.GPUBackendVAAPI, profiles.GPUBackendNVENC} {
			encoder, ok := encoderNameForBackend(codec, backend)
			if !ok {
				continue
			}
			if a.hardwareEncoderAutoEligible(backend, encoder) {
				codecs = append(codecs, codec)
				break
			}
		}
	}
	return codecs
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
	plan, err := a.buildArgsWithPlan(ctx, spec, inputURL)
	if err != nil {
		return "", fmt.Errorf("failed to build args: %w", err)
	}
	args := plan.args
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
	a.finalizedProfiles[handle] = plan.effectiveProfile
	delete(a.processDetails, handle)
	a.mu.Unlock()

	go a.monitorProcessWithStartTimeout(ctx, handle, cmd, stderr, spec.SessionID, argsHardwareBackend(args), a.startTimeoutForProfile(spec.Source.Type, plan.effectiveProfile))
	if sourceKey != "" {
		go a.learnFPSFromOutput(sourceKey, spec.SessionID)
	}

	metrics.RecordPipelineSpawn("ffmpeg", "admitted")
	return handle, nil
}

func (a *LocalAdapter) FinalizedProfile(handle ports.RunHandle) (ports.ProfileSpec, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	profile, ok := a.finalizedProfiles[handle]
	if !ok {
		return ports.ProfileSpec{}, false
	}
	return profile, true
}

func (a *LocalAdapter) monitorProcessWithStartTimeout(parentCtx context.Context, handle ports.RunHandle, cmd *exec.Cmd, stderr io.ReadCloser, sessionID string, hwBackend profiles.GPUBackend, startTimeout time.Duration) {
	defer func() {
		a.mu.Lock()
		delete(a.activeProcs, handle)
		delete(a.finalizedProfiles, handle)
		a.mu.Unlock()
	}()

	wd := watchdog.New(startTimeout, a.StallTimeout)

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	wdCtx, wdCancel := context.WithCancel(parentCtx)
	defer wdCancel()

	wdErrCh := make(chan error, 1)
	go func() {
		wdErrCh <- wd.Run(wdCtx)
	}()

	runtimeFailureLine := ""
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)

		// Forward lines to RingBuffer/Log and Watchdog.
		scanner := bufio.NewScanner(stderr)
		scanner.Split(scanFFmpegLogTokens)
		firstFrameLogged := false
		firstSegmentLogged := false

		for scanner.Scan() {
			line := scanner.Text()
			if !firstFrameLogged {
				if frame, ok := parseFFmpegFrameCount(line); ok && frame > 0 {
					firstFrameLogged = true
					wd.ObserveProgress()
					a.writeFirstFrameMarker(sessionID)
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
					wd.ObserveProgress()
					a.Logger.Info().
						Str("session_id", sessionID).
						Str("startup_phase", "first_segment_write").
						Str("segment_path", segmentPath).
						Msg("ffmpeg first segment write observed")
				}
			}
			sanitizedLine := sanitizeFFmpegLogLine(line)
			if detail := summarizeFFmpegFailureLine(sanitizedLine); detail != "" {
				a.recordProcessDetail(handle, detail)
			}
			if runtimeFailureLine == "" {
				switch hwBackend {
				case profiles.GPUBackendVAAPI:
					if isVAAPIRuntimeFailureLine(sanitizedLine) {
						runtimeFailureLine = sanitizedLine
					}
				case profiles.GPUBackendNVENC:
					if isNVENCRuntimeFailureLine(sanitizedLine) {
						runtimeFailureLine = sanitizedLine
					}
				}
			}
			switch ffmpegLogLevel(sanitizedLine) {
			case zerolog.WarnLevel:
				a.Logger.Warn().Str("sessionId", sessionID).Str("ffmpeg_log", sanitizedLine).Msg("ffmpeg output")
			case zerolog.InfoLevel:
				a.Logger.Info().Str("sessionId", sessionID).Str("ffmpeg_log", sanitizedLine).Msg("ffmpeg output")
			default:
				a.Logger.Debug().Str("sessionId", sessionID).Str("ffmpeg_log", sanitizedLine).Msg("ffmpeg output")
			}
			wd.ParseLine(line)
		}
		if scanErr := scanner.Err(); scanErr != nil {
			a.Logger.Warn().Err(scanErr).Str("sessionId", sessionID).Msg("ffmpeg stderr scan failed")
		}
	}()

	procErrCh := make(chan error, 1)
	go func() {
		// os/exec closes StderrPipe from Wait. Draining stderr first avoids
		// dropping the final FFmpeg failure lines that Health surfaces later.
		<-scanDone
		procErrCh <- cmd.Wait()
	}()

	var procErr error
	var resultErr error
	watchdogConsumed := false

	select {
	case procErr = <-procErrCh:
		resultErr = procErr
	case wdErr := <-wdErrCh:
		watchdogConsumed = true
		if wdErr != nil {
			metrics.IncLiveFFmpegStall("watchdog_timeout")
			a.recordProcessDetail(handle, "transcode stalled - no progress detected")
			a.Logger.Error().Err(wdErr).Str("sessionId", sessionID).Msg("watchdog triggered process termination")
			a.terminateProcessGroup(cmd, sessionID)
			procErr = <-procErrCh
			resultErr = wdErr
			break
		}
		procErr = <-procErrCh
		resultErr = procErr
	case <-parentCtx.Done():
		a.terminateProcessGroup(cmd, sessionID)
		procErr = <-procErrCh
		resultErr = parentCtx.Err()
	}

	wdCancel()
	if !watchdogConsumed {
		if wdErr := <-wdErrCh; wdErr != nil {
			metrics.IncLiveFFmpegStall("watchdog_timeout")
			a.recordProcessDetail(handle, "transcode stalled - no progress detected")
			a.Logger.Error().Err(wdErr).Str("sessionId", sessionID).Msg("watchdog reported failure")
			if resultErr == nil {
				resultErr = wdErr
			}
		}
	}

	<-scanDone

	if runtimeFailureLine != "" && (procErr != nil || resultErr != nil) {
		switch hwBackend {
		case profiles.GPUBackendVAAPI:
			a.recordVAAPIRuntimeFailure(sessionID, runtimeFailureLine)
		case profiles.GPUBackendNVENC:
			a.recordNVENCRuntimeFailure(sessionID, runtimeFailureLine)
		}
	}

	if procErr != nil {
		a.recordProcessDetail(handle, summarizeProcessExit(procErr))
		a.Logger.Debug().Err(procErr).Str("sessionId", sessionID).Msg("ffmpeg process exited")
		return
	}
	if resultErr != nil {
		return
	}
	a.clearProcessDetail(handle)
}

func (a *LocalAdapter) startTimeoutForProfile(sourceType ports.SourceType, profile ports.ProfileSpec) time.Duration {
	timeout := a.StartTimeout
	if timeout <= 0 {
		return timeout
	}
	if sourceType == ports.SourceFile || !profile.TranscodeVideo {
		return timeout
	}
	if strings.TrimSpace(profile.HWAccel) != "" {
		return timeout
	}

	overrideFloor := 30 * time.Second
	if profile.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		overrideFloor = 60 * time.Second
	} else if !strings.EqualFold(strings.TrimSpace(profile.Name), "safari") && !strings.EqualFold(strings.TrimSpace(profile.Name), "safari_hq") {
		return timeout
	}

	overrideMs := envIntBounded(
		"XG2G_SAFARI_CPU_START_TIMEOUT_MS",
		int(overrideFloor/time.Millisecond),
		int(timeout/time.Millisecond),
		120000,
	)
	override := time.Duration(overrideMs) * time.Millisecond
	if override > timeout {
		return override
	}
	return timeout
}

func argsHardwareBackend(args []string) profiles.GPUBackend {
	for i := 0; i < len(args); i++ {
		if args[i] == "-vaapi_device" {
			return profiles.GPUBackendVAAPI
		}
		switch args[i] {
		case "h264_nvenc", "hevc_nvenc", "av1_nvenc":
			return profiles.GPUBackendNVENC
		}
	}
	return profiles.GPUBackendNone
}

func (a *LocalAdapter) writeFirstFrameMarker(sessionID string) {
	if !ports.IsSafeSessionID(sessionID) {
		return
	}
	markerPath := ports.SessionFirstFrameMarkerPath(a.HLSRoot, sessionID)
	if markerPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o750); err != nil {
		a.Logger.Warn().
			Err(err).
			Str("session_id", sessionID).
			Str("marker_path", markerPath).
			Msg("failed to prepare first-frame marker directory")
		return
	}
	if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		a.Logger.Warn().
			Err(err).
			Str("session_id", sessionID).
			Str("marker_path", markerPath).
			Msg("failed to write first-frame marker")
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
	delete(a.processDetails, handle)
	a.mu.Unlock()

	if !exists {
		return nil // Idempotent
	}

	if cmd.Process != nil {
		return a.killProcessGroup(cmd)
	}

	return nil
}

func (a *LocalAdapter) terminateProcessGroup(cmd *exec.Cmd, sessionID string) {
	if err := a.killProcessGroup(cmd); err != nil {
		a.Logger.Warn().Err(err).Str("sessionId", sessionID).Msg("failed to terminate ffmpeg process group")
	}
}

func (a *LocalAdapter) killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Use procgroup for deterministic tree reaping.
	return procgroup.KillGroup(cmd.Process.Pid, 2*time.Second, a.KillTimeout)
}

func (a *LocalAdapter) Health(ctx context.Context, handle ports.RunHandle) ports.HealthStatus {
	a.mu.Lock()
	_, exists := a.activeProcs[handle]
	a.mu.Unlock()
	if !exists {
		// monitorProcess has finished — scanner drained, detail is final.
		return ports.HealthStatus{
			Healthy:   false,
			Message:   a.processStatusMessage(handle, "process not found"),
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

func envFloatBounded(key string, defaultValue, minValue, maxValue float64) float64 {
	raw := strings.TrimSpace(config.ParseString(key, ""))
	if raw == "" {
		return defaultValue
	}
	n, err := strconv.ParseFloat(raw, 64)
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

func (a *LocalAdapter) recordProcessDetail(handle ports.RunHandle, detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.processDetails[handle]
	if processDetailPriority(detail) >= processDetailPriority(current) {
		a.processDetails[handle] = detail
	}
}

func (a *LocalAdapter) clearProcessDetail(handle ports.RunHandle) {
	a.mu.Lock()
	delete(a.processDetails, handle)
	a.mu.Unlock()
}

func (a *LocalAdapter) processStatusMessage(handle ports.RunHandle, fallback string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if detail := strings.TrimSpace(a.processDetails[handle]); detail != "" {
		delete(a.processDetails, handle)
		return detail
	}
	return fallback
}

func processDetailPriority(detail string) int {
	switch detail {
	case "transcode stalled - no progress detected":
		return 50
	case "copy output missing codec parameters":
		return 45
	case "upstream stream ended prematurely":
		return 40
	case "failed to open upstream input", "upstream input/output error":
		return 30
	case "invalid upstream input data":
		return 20
	case "process exited unexpectedly":
		return 10
	default:
		return 0
	}
}

func summarizeFFmpegFailureLine(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(lower, "non-existing pps"),
		strings.Contains(lower, "non-existing sps"),
		strings.Contains(lower, "could not find codec parameters for stream") && strings.Contains(lower, "unspecified size"),
		strings.Contains(lower, "could not write header (incorrect codec parameters ?)"),
		strings.Contains(lower, "dimensions not set"):
		return "copy output missing codec parameters"
	case strings.Contains(lower, "stream ends prematurely"):
		return "upstream stream ended prematurely"
	case strings.Contains(lower, "error opening input files"),
		strings.Contains(lower, "error opening input file"),
		strings.Contains(lower, "error opening input:"):
		if strings.Contains(lower, "input/output error") {
			return "upstream input/output error"
		}
		return "failed to open upstream input"
	case strings.Contains(lower, "invalid data found when processing input"):
		return "invalid upstream input data"
	}
	return ""
}

func isVAAPIRuntimeFailureLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "vaapi") && looksLikeFFmpegWarning(lower) {
		return true
	}
	definitiveKeywords := []string{
		"libva error",
		"no usable encoding entrypoint",
		"failed to end picture",
		"failed to sync surface",
		"failed to export surface",
		"failed to upload",
		"hardware device reference is required",
		"va_create",
	}
	for _, keyword := range definitiveKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	if (strings.Contains(lower, "hwupload") || strings.Contains(lower, "renderd128")) && looksLikeFFmpegWarning(lower) {
		return true
	}
	return false
}

func isNVENCRuntimeFailureLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "nvenc") && looksLikeFFmpegWarning(lower) {
		return true
	}
	definitiveKeywords := []string{
		"cannot load libnvidia-encode.so.1",
		"driver does not support the required nvenc api version",
		"no capable devices found",
		"no nvenc capable devices found",
		"openencode session failed",
		"provided device doesn't support required nvenc features",
		"cannot init encoder",
		"nvidia",
	}
	for _, keyword := range definitiveKeywords {
		if strings.Contains(lower, keyword) && looksLikeFFmpegWarning(lower) {
			return true
		}
	}
	return false
}

func (a *LocalAdapter) recordVAAPIRuntimeFailure(sessionID, failureLine string) {
	if !hardware.IsVAAPIReady() {
		return
	}
	failures, demoted := hardware.RecordVAAPIRuntimeFailure()
	event := a.Logger.Warn().
		Str("session_id", sessionID).
		Int("vaapi_runtime_failures", failures)
	if failureLine != "" {
		event = event.Str("ffmpeg_log", failureLine)
	}
	if demoted {
		event.Msg("vaapi runtime failure threshold reached; gpu demoted to cpu fallback")
		return
	}
	event.Msg("recorded vaapi runtime failure")
}

func (a *LocalAdapter) recordNVENCRuntimeFailure(sessionID, failureLine string) {
	if !hardware.IsNVENCReady() {
		return
	}
	failures, demoted := hardware.RecordNVENCRuntimeFailure()
	event := a.Logger.Warn().
		Str("session_id", sessionID).
		Int("nvenc_runtime_failures", failures)
	if failureLine != "" {
		event = event.Str("ffmpeg_log", failureLine)
	}
	if demoted {
		event.Msg("nvenc runtime failure threshold reached; gpu demoted to cpu fallback")
		return
	}
	event.Msg("recorded nvenc runtime failure")
}

func ffmpegLogLevel(line string) zerolog.Level {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return zerolog.DebugLevel
	}
	if isFFmpegProgressLine(lower) {
		return zerolog.DebugLevel
	}
	if summarizeFFmpegFailureLine(lower) != "" || looksLikeFFmpegWarning(lower) {
		return zerolog.WarnLevel
	}
	return zerolog.InfoLevel
}

func isFFmpegProgressLine(lower string) bool {
	switch {
	case strings.HasPrefix(lower, "frame="),
		strings.HasPrefix(lower, "fps="),
		strings.HasPrefix(lower, "stream_"),
		strings.HasPrefix(lower, "bitrate="),
		strings.HasPrefix(lower, "total_size="),
		strings.HasPrefix(lower, "out_time_us="),
		strings.HasPrefix(lower, "out_time_ms="),
		strings.HasPrefix(lower, "out_time="),
		strings.HasPrefix(lower, "dup_frames="),
		strings.HasPrefix(lower, "drop_frames="),
		strings.HasPrefix(lower, "speed="),
		strings.HasPrefix(lower, "progress="):
		return true
	case strings.Contains(lower, " opening '") && strings.Contains(lower, "' for writing"):
		return true
	case strings.Contains(lower, "opening \"") && strings.Contains(lower, "\" for writing"):
		return true
	case strings.Contains(lower, "press [q] to stop"):
		return true
	default:
		return false
	}
}

func looksLikeFFmpegWarning(lower string) bool {
	keywords := []string{
		" error",
		"error ",
		"failed",
		"invalid",
		"non-existing",
		"no frame",
		"decode_slice_header",
		"corrupt",
		"unable to",
		"could not",
		"connection refused",
		"broken pipe",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func summarizeProcessExit(procErr error) string {
	if procErr == nil {
		return ""
	}
	var exitErr *exec.ExitError
	if errors.As(procErr, &exitErr) {
		return fmt.Sprintf("process exit code %d", exitErr.ExitCode())
	}
	return "process exited unexpectedly"
}
