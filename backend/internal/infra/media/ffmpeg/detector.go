// Package-internal Detector owns FFmpeg encoder-capability detection state
// (VAAPI/NVENC preflight + synthetic benchmarks), extracted from LocalAdapter.
package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg/capability"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Detector struct {
	BinPath     string
	Logger      zerolog.Logger
	VaapiDevice string
	HLSRoot     string

	pathCorrectnessChecked bool
	pathProbeFn            func(context.Context, pathProbeRequest) (hardware.HardwarePathCapability, error)
	signalStatsYAvgFn      func(context.Context, string) (float64, error)
	recordProcessDetail    func(ports.RunHandle, string)
	terminateProcessGroup  func(*exec.Cmd, string)

	vaapiEncoders            map[string]bool
	vaapiEncoderCaps         map[string]hardware.VAAPIEncoderCapability
	vaapiDeviceChecked       bool
	vaapiDeviceErr           error
	nvencEncoders            map[string]bool
	nvencEncoderCaps         map[string]hardware.NVENCEncoderCapability
	nvencChecked             bool
	nvencErr                 error
	profileBenchmarksChecked bool
	profileProbeFn           func(context.Context, profileProbeRequest) (time.Duration, error)
}

func newDetector(binPath string, logger zerolog.Logger, vaapiDevice, hlsRoot string) *Detector {
	return &Detector{BinPath: binPath, Logger: logger, VaapiDevice: vaapiDevice, HLSRoot: hlsRoot}
}

// PreflightVAAPI validates that the configured VAAPI device is functional.
// Tests each available encoder (h264_vaapi, hevc_vaapi) independently.
// Results are cached per-encoder: buildArgs checks the specific encoder.
func (d *Detector) PreflightVAAPI() error {
	if d.VaapiDevice == "" {
		return nil
	}
	if d.vaapiDeviceChecked {
		return d.vaapiDeviceErr
	}

	d.vaapiEncoders = make(map[string]bool)
	d.vaapiEncoderCaps = make(map[string]hardware.VAAPIEncoderCapability)
	d.vaapiDeviceChecked = true

	d.Logger.Info().Str("device", d.VaapiDevice).Msg("vaapi preflight: starting")

	// 1. Device accessible
	if _, err := os.Stat(d.VaapiDevice); err != nil {
		d.vaapiDeviceErr = fmt.Errorf("vaapi device not accessible: %w", err)
		d.Logger.Error().Err(d.vaapiDeviceErr).Str("device", d.VaapiDevice).Msg("vaapi preflight: device stat failed")
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
		return d.vaapiDeviceErr
	}

	// 2. Enumerate available VAAPI encoders
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- BinPath is trusted from config
	checkCmd := exec.CommandContext(ctx, d.BinPath, "-hide_banner", "-encoders")
	checkOut, err := checkCmd.Output()
	if err != nil {
		d.vaapiDeviceErr = fmt.Errorf("vaapi preflight: ffmpeg -encoders failed: %w", err)
		d.Logger.Error().Err(d.vaapiDeviceErr).Msg("vaapi preflight: encoder check failed")
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
		return d.vaapiDeviceErr
	}
	encoderList := string(checkOut)

	// 3. Test each encoder with a real 5-frame encode
	verifiedElapsed := make(map[string]time.Duration, len(vaapiEncodersToTest))
	for _, enc := range vaapiEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			d.Logger.Info().Str("encoder", enc).Msg("vaapi preflight: encoder not in ffmpeg build, skipping")
			continue
		}
		elapsed, err := d.testVaapiEncoder(enc)
		if err != nil {
			d.Logger.Warn().Err(err).Str("encoder", enc).Msg("vaapi preflight: encoder test failed")
		} else {
			d.vaapiEncoders[enc] = true
			verifiedElapsed[enc] = elapsed
			d.Logger.Info().
				Str("encoder", enc).
				Dur("probe_elapsed", elapsed).
				Msg("vaapi preflight: encoder verified")
		}
	}

	if len(d.vaapiEncoders) == 0 {
		d.vaapiDeviceErr = fmt.Errorf("vaapi preflight: no working VAAPI encoders found")
		d.Logger.Error().Err(d.vaapiDeviceErr).Msg("vaapi preflight: failed")
		hardware.SetVAAPIEncoderCapabilities(nil)
		hardware.SetVAAPIPreflightResult(false)
		return d.vaapiDeviceErr
	}

	d.vaapiEncoderCaps = capability.DeriveVAAPIEncoderCapabilities(
		verifiedElapsed,
		envFloatBounded("XG2G_HEVC_VAAPI_AUTO_RATIO_MAX", capability.DefaultHEVCVAAPIAutoRatioMax, 1.0, 10.0),
		envFloatBounded("XG2G_AV1_VAAPI_AUTO_RATIO_MAX", capability.DefaultAV1VAAPIAutoRatioMax, 1.0, 10.0),
	)
	for _, enc := range vaapiEncodersToTest {
		cap, ok := d.vaapiEncoderCaps[enc]
		if !ok || !cap.Verified {
			continue
		}
		d.Logger.Info().
			Str("encoder", enc).
			Dur("probe_elapsed", cap.ProbeElapsed).
			Bool("auto_eligible", cap.AutoEligible).
			Msg("vaapi preflight: encoder capability")
	}

	// Publish per-encoder results for higher layers (HTTP/profile selection).
	hardware.SetVAAPIEncoderCapabilities(d.vaapiEncoderCaps)

	hardware.SetVAAPIPreflightResult(true)
	d.Logger.Info().
		Str("device", d.VaapiDevice).
		Int("verified_encoders", len(d.vaapiEncoders)).
		Msg("vaapi preflight: passed")
	return nil
}

// PreflightNVENC validates that the visible NVIDIA runtime can execute real NVENC encodes.
func (d *Detector) PreflightNVENC() error {
	if !hardware.HasNVENC() {
		return nil
	}
	if d.nvencChecked {
		return d.nvencErr
	}

	d.nvencEncoders = make(map[string]bool)
	d.nvencEncoderCaps = make(map[string]hardware.NVENCEncoderCapability)
	d.nvencChecked = true

	d.Logger.Info().Msg("nvenc preflight: starting")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G204 -- BinPath is trusted from config
	checkCmd := exec.CommandContext(ctx, d.BinPath, "-hide_banner", "-encoders")
	checkOut, err := checkCmd.Output()
	if err != nil {
		d.nvencErr = fmt.Errorf("nvenc preflight: ffmpeg -encoders failed: %w", err)
		d.Logger.Error().Err(d.nvencErr).Msg("nvenc preflight: encoder check failed")
		hardware.SetNVENCEncoderCapabilities(nil)
		hardware.SetNVENCPreflightResult(false)
		return d.nvencErr
	}
	encoderList := string(checkOut)

	verifiedElapsed := make(map[string]time.Duration, len(nvencEncodersToTest))
	for _, enc := range nvencEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			d.Logger.Info().Str("encoder", enc).Msg("nvenc preflight: encoder not in ffmpeg build, skipping")
			continue
		}
		elapsed, err := d.testNVENCEncoder(enc)
		if err != nil {
			d.Logger.Warn().Err(err).Str("encoder", enc).Msg("nvenc preflight: encoder test failed")
		} else {
			d.nvencEncoders[enc] = true
			verifiedElapsed[enc] = elapsed
			d.Logger.Info().
				Str("encoder", enc).
				Dur("probe_elapsed", elapsed).
				Msg("nvenc preflight: encoder verified")
		}
	}

	if len(d.nvencEncoders) == 0 {
		d.nvencErr = fmt.Errorf("nvenc preflight: no working NVENC encoders found")
		d.Logger.Error().Err(d.nvencErr).Msg("nvenc preflight: failed")
		hardware.SetNVENCEncoderCapabilities(nil)
		hardware.SetNVENCPreflightResult(false)
		return d.nvencErr
	}

	d.nvencEncoderCaps = capability.DeriveNVENCEncoderCapabilities(
		verifiedElapsed,
		envFloatBounded("XG2G_HEVC_NVENC_AUTO_RATIO_MAX", capability.DefaultHEVCNVENCAutoRatioMax, 1.0, 10.0),
		envFloatBounded("XG2G_AV1_NVENC_AUTO_RATIO_MAX", capability.DefaultAV1NVENCAutoRatioMax, 1.0, 10.0),
	)
	for _, enc := range nvencEncodersToTest {
		cap, ok := d.nvencEncoderCaps[enc]
		if !ok || !cap.Verified {
			continue
		}
		d.Logger.Info().
			Str("encoder", enc).
			Dur("probe_elapsed", cap.ProbeElapsed).
			Bool("auto_eligible", cap.AutoEligible).
			Msg("nvenc preflight: encoder capability")
	}

	hardware.SetNVENCEncoderCapabilities(d.nvencEncoderCaps)
	hardware.SetNVENCPreflightResult(true)
	d.Logger.Info().
		Int("verified_encoders", len(d.nvencEncoders)).
		Msg("nvenc preflight: passed")
	return nil
}

// PreflightTranscodeProfiles measures a small set of synthetic startup probes
// so host decisions can distinguish between audio-only, progressive, deinterlaced,
// and UHD realtime paths.
func (d *Detector) PreflightTranscodeProfiles() {
	if d.profileBenchmarksChecked {
		return
	}
	d.profileBenchmarksChecked = true

	d.Logger.Info().Msg("transcode profile preflight: starting")

	cpuSamples := d.measureProfileBenchmarks("cpu", "libx264")
	hardware.SetCPUProfileBenchmarks(capability.DeriveProfileCapabilities(cpuSamples))

	if d.VaapiEncoderVerified("h264_vaapi") {
		vaapiSamples := d.measureProfileBenchmarks("vaapi", "h264_vaapi")
		hardware.SetVAAPIProfileBenchmarks(capability.DeriveProfileCapabilities(vaapiSamples))
	} else {
		hardware.SetVAAPIProfileBenchmarks(nil)
	}

	if d.NVENCEncoderVerified("h264_nvenc") {
		nvencSamples := d.measureProfileBenchmarks("nvenc", "h264_nvenc")
		hardware.SetNVENCProfileBenchmarks(capability.DeriveProfileCapabilities(nvencSamples))
	} else {
		hardware.SetNVENCProfileBenchmarks(nil)
	}
}

// testVaapiEncoder runs a real 5-frame encode test for a specific VAAPI encoder.
func (d *Detector) testVaapiEncoder(encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	// #nosec G204 -- BinPath and VaapiDevice are trusted from config
	cmd := exec.CommandContext(ctx, d.BinPath,
		"-vaapi_device", d.VaapiDevice,
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

func (d *Detector) testNVENCEncoder(encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	// #nosec G204 -- BinPath is trusted from config
	cmd := exec.CommandContext(ctx, d.BinPath,
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

func (d *Detector) measureProfileBenchmarks(backend, encoder string) map[string]time.Duration {
	profilesToBenchmark := profileBenchmarksForBackend(backend)
	samples := make(map[string]time.Duration, len(profilesToBenchmark))
	for _, profileID := range profilesToBenchmark {
		elapsed, err := d.testProfileBenchmark(backend, encoder, profileID)
		if err != nil {
			d.Logger.Warn().
				Err(err).
				Str("backend", backend).
				Str("encoder", encoder).
				Str("profile_benchmark", profileID).
				Msg("transcode profile preflight: synthetic profile probe failed")
			continue
		}
		samples[profileID] = elapsed
		d.Logger.Info().
			Str("backend", backend).
			Str("encoder", encoder).
			Str("profile_benchmark", profileID).
			Dur("probe_elapsed", elapsed).
			Msg("transcode profile preflight: synthetic profile probe verified")
	}
	return samples
}

func (d *Detector) testProfileBenchmark(backend, encoder, profileID string) (time.Duration, error) {
	req := profileProbeRequest{
		ProfileID: profileID,
		Backend:   backend,
		Encoder:   encoder,
	}
	if d.profileProbeFn != nil {
		return d.profileProbeFn(context.Background(), req)
	}

	if profileID == playbackports.BenchmarkProfileAudioAACStereo {
		return d.testAudioAACProfile()
	}

	switch backend {
	case "cpu":
		return d.testCPUH264Profile(profileID, encoder)
	case "vaapi":
		return d.testVAAPIH264Profile(profileID, encoder)
	case "nvenc":
		return d.testNVENCH264Profile(profileID, encoder)
	default:
		return 0, fmt.Errorf("unsupported benchmark backend %q", backend)
	}
}

func (d *Detector) testAudioAACProfile() (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	args := []string{
		"-f", "lavfi",
		"-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-t", "0.2",
		"-vn",
		"-c:a", "aac",
		"-b:a", "256k",
		"-ac", "2",
		"-ar", "48000",
		"-f", "null", "-",
	}
	return runProfileBenchmarkCommand(ctx, d.BinPath, args)
}

func (d *Detector) testCPUH264Profile(profileID, encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), profileBenchmarkTimeout(profileID))
	defer cancel()

	args := []string{
		"-f", "lavfi",
		"-i", profileBenchmarkInput(profileID),
	}
	if filter := cpuProfileBenchmarkFilter(profileID); filter != "" {
		args = append(args, "-vf", filter)
	}
	args = append(args,
		"-c:v", encoder,
		"-preset", "veryfast",
		"-frames:v", "5",
		"-f", "null", "-",
	)
	return runProfileBenchmarkCommand(ctx, d.BinPath, args)
}

func (d *Detector) testVAAPIH264Profile(profileID, encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), profileBenchmarkTimeout(profileID))
	defer cancel()

	filter, err := vaapiProfileBenchmarkFilter(profileID)
	if err != nil {
		return 0, err
	}
	args := []string{
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", profileBenchmarkInput(profileID),
		"-vf", filter,
		"-c:v", encoder,
		"-frames:v", "5",
		"-f", "null", "-",
	}
	return runProfileBenchmarkCommand(ctx, d.BinPath, args)
}

func (d *Detector) testNVENCH264Profile(profileID, encoder string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), profileBenchmarkTimeout(profileID))
	defer cancel()

	args := []string{
		"-f", "lavfi",
		"-i", profileBenchmarkInput(profileID),
	}
	if filter := nvencProfileBenchmarkFilter(profileID); filter != "" {
		args = append(args, "-vf", filter)
	}
	args = append(args,
		"-c:v", encoder,
		"-frames:v", "5",
		"-f", "null", "-",
	)
	return runProfileBenchmarkCommand(ctx, d.BinPath, args)
}

// VaapiEncoderVerified returns true if the given encoder passed preflight.
func (d *Detector) VaapiEncoderVerified(encoder string) bool {
	return d.vaapiEncoders[encoder]
}

// VaapiEncoderAutoEligible returns true if the encoder is verified and suitable
// for generic automatic codec selection on this host.
func (d *Detector) VaapiEncoderAutoEligible(encoder string) bool {
	cap, ok := d.vaapiEncoderCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

func (d *Detector) NVENCEncoderVerified(encoder string) bool {
	return d.nvencEncoders[encoder]
}

func (d *Detector) NVENCEncoderAutoEligible(encoder string) bool {
	cap, ok := d.nvencEncoderCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

func (d *Detector) hardwareEncoderVerified(backend profiles.GPUBackend, encoder string) bool {
	switch backend {
	case profiles.GPUBackendVAAPI:
		return d.VaapiEncoderVerified(encoder)
	case profiles.GPUBackendNVENC:
		return d.NVENCEncoderVerified(encoder)
	default:
		return false
	}
}

func (d *Detector) hardwareEncoderAutoEligible(backend profiles.GPUBackend, encoder string) bool {
	switch backend {
	case profiles.GPUBackendVAAPI:
		return d.VaapiEncoderAutoEligible(encoder)
	case profiles.GPUBackendNVENC:
		return d.NVENCEncoderAutoEligible(encoder)
	default:
		return false
	}
}

func (d *Detector) hardwareEncoderCapability(backend profiles.GPUBackend, encoder string) (hardware.HardwareEncoderCapability, bool) {
	switch backend {
	case profiles.GPUBackendVAAPI:
		cap, ok := d.vaapiEncoderCaps[encoder]
		return cap, ok
	case profiles.GPUBackendNVENC:
		cap, ok := d.nvencEncoderCaps[encoder]
		return cap, ok
	default:
		return hardware.HardwareEncoderCapability{}, false
	}
}

func (d *Detector) preferredHardwareBackendForCodec(codec string) profiles.GPUBackend {
	var (
		bestBackend profiles.GPUBackend
		bestCap     hardware.HardwareEncoderCapability
		ok          bool
	)
	for _, backend := range []profiles.GPUBackend{profiles.GPUBackendVAAPI, profiles.GPUBackendNVENC} {
		encoder, exists := capability.EncoderNameForBackend(codec, backend)
		if !exists || !d.hardwareEncoderVerified(backend, encoder) {
			continue
		}
		cap, exists := d.hardwareEncoderCapability(backend, encoder)
		if !exists {
			cap = hardware.HardwareEncoderCapability{Verified: true}
		}
		if !ok || capability.BetterLocalHardwareCapability(backend, cap, bestBackend, bestCap) {
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

// PreflightPathCorrectness validates a small set of host-specific media paths
// whose encoder availability alone is not sufficient to trust output quality.
func (d *Detector) PreflightPathCorrectness() {
	if d.pathCorrectnessChecked {
		return
	}
	d.pathCorrectnessChecked = true

	capabilities := make(map[string]hardware.HardwarePathCapability)
	for _, req := range []pathProbeRequest{
		{PathID: hardware.PathVAAPIFullInterlacedHEVC, Backend: "vaapi", Encoder: "hevc_vaapi"},
		{PathID: hardware.PathVAAPIEncodeOnlyInterlacedHEVC, Backend: "vaapi", Encoder: "hevc_vaapi"},
		{PathID: hardware.PathVAAPIEncodeOnlyInterlacedAV1, Backend: "vaapi", Encoder: "av1_vaapi"},
	} {
		if req.Backend != "vaapi" || !d.VaapiEncoderVerified(req.Encoder) {
			continue
		}
		capability, err := d.testPathCorrectness(req)
		if err != nil {
			capability = hardware.HardwarePathCapability{
				Status: hardware.PathStatusPreflightFailed,
				Reason: err.Error(),
			}
			d.Logger.Warn().
				Err(err).
				Str("path_id", req.PathID).
				Str("encoder", req.Encoder).
				Msg("path correctness preflight failed")
		} else {
			d.Logger.Info().
				Str("path_id", req.PathID).
				Str("encoder", req.Encoder).
				Str("status", capability.Status).
				Str("reason", capability.Reason).
				Msg("path correctness preflight result")
		}
		capabilities[req.PathID] = capability
	}

	hardware.SetPathCapabilities(capabilities)
}

func (d *Detector) testPathCorrectness(req pathProbeRequest) (hardware.HardwarePathCapability, error) {
	if d.pathProbeFn != nil {
		return d.pathProbeFn(context.Background(), req)
	}
	switch req.PathID {
	case hardware.PathVAAPIFullInterlacedHEVC, hardware.PathVAAPIFullInterlacedAV1:
		return d.testVAAPIInterlacedPathCorrectness(req.Encoder, true)
	case hardware.PathVAAPIEncodeOnlyInterlacedHEVC, hardware.PathVAAPIEncodeOnlyInterlacedAV1:
		return d.testVAAPIInterlacedPathCorrectness(req.Encoder, false)
	default:
		return hardware.HardwarePathCapability{}, fmt.Errorf("unsupported path correctness probe %q", req.PathID)
	}
}

func (d *Detector) testVAAPIInterlacedPathCorrectness(encoder string, full bool) (hardware.HardwarePathCapability, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "xg2g-path-correctness-*")
	if err != nil {
		return hardware.HardwarePathCapability{}, fmt.Errorf("mktemp path correctness probe: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	outPath := filepath.Join(tempDir, "probe.mkv")
	filter := "format=nv12,setfield=tff,hwupload,deinterlace_vaapi"
	if !full {
		filter = vaapiEncodeOnlyInterlacedCorrectnessFilter(encoder)
	}
	encodeArgs := []string{
		"-y",
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", "testsrc2=duration=0.4:size=1920x1080:rate=25",
		"-vf", filter,
		"-c:v", encoder,
		"-frames:v", "5",
		outPath,
	}
	if _, err := runProfileBenchmarkCommand(ctx, d.BinPath, encodeArgs); err != nil {
		return hardware.HardwarePathCapability{}, fmt.Errorf("encode correctness probe failed: %w", err)
	}

	lumaYAvg, err := d.measureSignalStatsYAvg(ctx, outPath)
	if err != nil {
		if !full && normalizeRequestedCodec(encoder) == "av1" && isAV1SignalStatsDecodeUnavailable(err) && outputFileHasBytes(outPath) {
			return hardware.HardwarePathCapability{
				Verified: true,
				Status:   hardware.PathStatusVerified,
				Reason:   "synthetic av1 encode verified; local signalstats decode unavailable",
			}, nil
		}
		return hardware.HardwarePathCapability{}, fmt.Errorf("signalstats correctness probe failed: %w", err)
	}
	if lumaYAvg < 32 {
		return hardware.HardwarePathCapability{
			Status: hardware.PathStatusBrokenOutput,
			Reason: fmt.Sprintf("synthetic yavg %.2f below threshold", lumaYAvg),
		}, nil
	}

	return hardware.HardwarePathCapability{
		Verified: true,
		Status:   hardware.PathStatusVerified,
		Reason:   fmt.Sprintf("synthetic yavg %.2f", lumaYAvg),
	}, nil
}

func (d *Detector) measureSignalStatsYAvg(ctx context.Context, mediaPath string) (float64, error) {
	if d.signalStatsYAvgFn != nil {
		return d.signalStatsYAvgFn(ctx, mediaPath)
	}
	args := []string{
		"-v", "info",
		"-hwaccel", "none",
		"-i", mediaPath,
		"-vf", "signalstats,metadata=mode=print",
		"-frames:v", "1",
		"-f", "null", "-",
	}
	// #nosec G204 -- BinPath is trusted from config and mediaPath is a local temp path.
	cmd := exec.CommandContext(ctx, d.BinPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("measure signalstats: %w (output: %s)", err, string(out))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		const prefix = "lavfi.signalstats.YAVG="
		_, after, ok := strings.Cut(line, prefix)
		if !ok {
			continue
		}
		value := strings.TrimSpace(after)
		yavg, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("parse signalstats yavg %q: %w", value, parseErr)
		}
		return yavg, nil
	}
	return 0, errors.New("lavfi.signalstats.YAVG not found")
}

func (d *Detector) observeRuntimePathCorrectness(ctx context.Context, handle ports.RunHandle, cmd *exec.Cmd, sessionID, pathID string) {
	if strings.TrimSpace(pathID) == "" || !ports.IsSafeSessionID(sessionID) {
		return
	}

	playlistPath := filepath.Join(ports.SessionHLSDir(d.HLSRoot, sessionID), "index.m3u8")
	deadline := time.Now().Add(20 * time.Second)
	minYAvg := envFloatBounded("XG2G_RUNTIME_PATH_CORRECTNESS_MIN_YAVG", defaultRuntimePathCorrectnessMinYAvg, 1.0, 64.0)
	requiredLowObservations := envIntBounded("XG2G_RUNTIME_PATH_CORRECTNESS_LOW_OBS", defaultRuntimePathCorrectnessChecks, 1, 4)
	lowObservations := 0

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return
		}

		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		yavg, err := d.measureSignalStatsYAvg(probeCtx, playlistPath)
		cancel()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
			continue
		}

		d.Logger.Info().
			Str("session_id", sessionID).
			Str("path_id", pathID).
			Float64("yavg", yavg).
			Msg("runtime path correctness observation")

		if yavg < minYAvg {
			lowObservations++
			if lowObservations < requiredLowObservations {
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
				}
				continue
			}

			reason := fmt.Sprintf("runtime yavg %.2f below threshold %.2f", yavg, minYAvg)
			d.updateRuntimePathCapability(pathID, hardware.HardwarePathCapability{
				Status: hardware.PathStatusBrokenOutput,
				Reason: reason,
			})
			if d.recordProcessDetail != nil {
				d.recordProcessDetail(handle, "runtime path correctness failed - black output detected")
			}
			d.Logger.Error().
				Str("session_id", sessionID).
				Str("path_id", pathID).
				Float64("yavg", yavg).
				Float64("threshold", minYAvg).
				Msg("runtime path correctness marked path as broken_output")
			if d.terminateProcessGroup != nil {
				d.terminateProcessGroup(cmd, sessionID)
			}
			return
		}

		reason := fmt.Sprintf("runtime yavg %.2f", yavg)
		d.updateRuntimePathCapability(pathID, hardware.HardwarePathCapability{
			Verified: true,
			Status:   hardware.PathStatusVerified,
			Reason:   reason,
		})
		d.Logger.Info().
			Str("session_id", sessionID).
			Str("path_id", pathID).
			Float64("yavg", yavg).
			Msg("runtime path correctness verified path")
		return
	}
}

func (d *Detector) updateRuntimePathCapability(pathID string, capability hardware.HardwarePathCapability) {
	current := hardware.HardwarePathCapabilities()
	if current == nil {
		current = make(map[string]hardware.HardwarePathCapability)
	}
	current[pathID] = capability
	hardware.SetPathCapabilities(current)
}
