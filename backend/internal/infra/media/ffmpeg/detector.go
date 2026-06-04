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
	metricsgpu "github.com/ManuGH/xg2g/internal/metrics/gpu"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/rs/zerolog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Decode-verify thresholds (B1). "verified" means the encoded output decoded to
// a complete, non-black, non-flat frame sequence — not merely "the encoder
// exited 0 while discarding its output". The luma-range check is WITHIN-frame
// (catches a flat/uniform field); it does NOT catch a content-rich freeze (a
// dead stream repeating one good frame). That is a runtime pathology a fresh
// preflight encode cannot exhibit anyway, and is deferred to the live runtime
// watchdog (observeRuntimePathCorrectness) — an empirical YDIF temporal check
// was rejected because it did not separate freeze from motion on synthetic
// clips (frozen YDIF 3.25 > animated 2.80).
const (
	decodeVerifyFrames   = 10
	decodeVerifyMinYAvg  = 32.0
	decodeVerifyMinRange = 16.0
)

// signalStatsLumaVF is the ffmpeg -vf used for EVERY luma (YAVG) measurement
// (decode-verify, the synthetic path-correctness probe, and the runtime
// watchdog). The leading format=yuv420p forces an 8-bit decode BEFORE signalstats
// so YAVG is always reported on the 0-255 scale that every threshold here assumes
// (decodeVerifyMinYAvg, the synthetic-probe 32, and the runtime watchdog's
// XG2G_RUNTIME_PATH_CORRECTNESS_MIN_YAVG default 8). Without it a 10-bit p010 AV1
// decode makes signalstats report on the 0-1023 scale -- empirically (staging
// av1_vaapi p010) raw signalstats YAVG 497.5 for a bright pattern that is 124.4 at
// 8-bit, ~4x inflated. That made (a) the runtime black-detector ~4x too lenient on
// the exact 10-bit interlaced-AV1 path and (b) the measurement ffmpeg-build
// dependent (one build reported ~126, another ~497 for similar content). yuv420p
// makes it scale- and build-stable. The encoded stream is untouched; this is the
// measurement decode only.
const signalStatsLumaVF = "format=yuv420p,signalstats,metadata=mode=print"

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

	// B1 decode-verify seams + state.
	decodeVerifyFn    func(context.Context, string, string, int) (hardware.EncoderVerdict, string)
	decodeStatsFn     func(context.Context, string, string) (int, float64, float64, error)
	softwareDecoderFn func(string) (string, bool)
	minDecodeYAvg     float64 // overridable threshold; <=0 means decodeVerifyMinYAvg
	decodersOnce      sync.Once
	decodersAvail     map[string]bool
}

func newDetector(binPath string, logger zerolog.Logger, vaapiDevice, hlsRoot string) *Detector {
	return &Detector{BinPath: binPath, Logger: logger, VaapiDevice: vaapiDevice, HLSRoot: hlsRoot}
}

// PreflightVAAPI validates that the configured VAAPI device is functional.
// Tests each available encoder (h264_vaapi, hevc_vaapi) independently.
// Results are cached per-encoder: buildArgs checks the specific encoder.
// publishEncoderGauges mirrors the per-encoder verified/auto-eligible verdicts
// into Prometheus gauges so fleet hardware-encode capability is observable.
// A nil caps map sets every listed encoder to 0 (not verified / not eligible).
func publishEncoderGauges(encoders []string, caps map[string]hardware.HardwareEncoderCapability) {
	for _, enc := range encoders {
		c, ok := caps[enc]
		metricsgpu.SetEncoderVerified(enc, ok && c.Verified)
		metricsgpu.SetEncoderAutoEligible(enc, ok && c.AutoEligible)
		metricsgpu.SetEncoderUnverifiable(enc, ok && c.Verdict == hardware.VerdictUnverifiable)
	}
}

// applyEncoderVerdicts overlays the three-state verdict + per-bit-depth results
// onto the derived capability map. The admission field cap.Verified — the SINGLE
// field every gate reads (autocodec via IsHardwareEncoderReady -> cap.Verified;
// plan_codec via the verified set, which is set from the same production-format
// verdict) and the ONLY field B3 may export as "admitted" — is DERIVED here from
// the bit depth production drives this codec at (AV1: 10-bit p010; others: 8-bit
// nv12). It is never set independently, so the exported fingerprint cannot
// diverge from what admission applies. Verified8Bit/Verified10Bit are per-depth
// detail (both fully decode-verified) and gate NOTHING by themselves.
func applyEncoderVerdicts(caps map[string]hardware.VAAPIEncoderCapability, encoders []string, verdicts map[string]hardware.EncoderVerdict, reasons map[string]string, verified8, verified10 map[string]bool) {
	for _, enc := range encoders {
		verdict, ok := verdicts[enc]
		if !ok {
			continue
		}
		cap := caps[enc] // zero value for non-verified encoders
		cap.Verdict = verdict
		cap.Reason = reasons[enc]
		cap.Verified8Bit = verified8[enc]
		cap.Verified10Bit = verified10[enc]
		if normalizeRequestedCodec(enc) == "av1" {
			cap.Verified = cap.Verified10Bit
		} else {
			cap.Verified = cap.Verified8Bit
		}
		if !cap.Verified {
			cap.AutoEligible = false
		}
		caps[enc] = cap
	}
}

func (d *Detector) PreflightVAAPI() error {
	publishEncoderGauges(vaapiEncodersToTest, nil)
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

	// 3. Probe each encoder, then decode-verify its output (three-state verdict:
	// verified / withheld / unverifiable). Only "verified" encoders are admitted.
	verifiedElapsed := make(map[string]time.Duration, len(vaapiEncodersToTest))
	verdicts := make(map[string]hardware.EncoderVerdict, len(vaapiEncodersToTest))
	reasons := make(map[string]string, len(vaapiEncodersToTest))
	verified8 := make(map[string]bool, len(vaapiEncodersToTest))
	verified10 := make(map[string]bool, len(vaapiEncodersToTest))
	for _, enc := range vaapiEncodersToTest {
		if !strings.Contains(encoderList, enc) {
			d.Logger.Info().Str("encoder", enc).Msg("vaapi preflight: encoder not in ffmpeg build, skipping")
			verdicts[enc] = hardware.VerdictWithheld
			reasons[enc] = "encoder not in ffmpeg build"
			continue
		}
		// Judge each encoder at the bit depth production drives it at: AV1 at
		// 10-bit (p010le, see encode_args), H.264/HEVC at 8-bit (nv12). For AV1
		// also probe 8-bit so the record shows whether a host does AV1 8-bit but
		// not 10-bit (a p010 driver-promotion that fails elsewhere in the fleet).
		isAV1 := normalizeRequestedCodec(enc) == "av1"
		prodFormat := "nv12"
		if isAV1 {
			prodFormat = "p010le"
		}
		elapsed, verdict, reason := d.probeAndVerifyVaapiEncoder(enc, prodFormat)
		verdicts[enc] = verdict
		reasons[enc] = reason
		if isAV1 {
			verified10[enc] = verdict == hardware.VerdictVerified
			if _, v8, _ := d.probeAndVerifyVaapiEncoder(enc, "nv12"); v8 == hardware.VerdictVerified {
				verified8[enc] = true
			}
		} else {
			verified8[enc] = verdict == hardware.VerdictVerified
		}
		if verdict == hardware.VerdictVerified {
			d.vaapiEncoders[enc] = true
			verifiedElapsed[enc] = elapsed
			d.Logger.Info().Str("encoder", enc).Str("upload_format", prodFormat).Dur("probe_elapsed", elapsed).Bool("verified_8bit", verified8[enc]).Bool("verified_10bit", verified10[enc]).Msg("vaapi preflight: encoder verified")
		} else {
			d.Logger.Warn().Str("encoder", enc).Str("upload_format", prodFormat).Str("verdict", string(verdict)).Str("reason", reason).Msg("vaapi preflight: encoder not admitted")
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
	if d.vaapiEncoderCaps == nil {
		d.vaapiEncoderCaps = make(map[string]hardware.VAAPIEncoderCapability, len(vaapiEncodersToTest))
	}
	applyEncoderVerdicts(d.vaapiEncoderCaps, vaapiEncodersToTest, verdicts, reasons, verified8, verified10)
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
	publishEncoderGauges(vaapiEncodersToTest, d.vaapiEncoderCaps)

	hardware.SetVAAPIPreflightResult(true)
	d.Logger.Info().
		Str("device", d.VaapiDevice).
		Int("verified_encoders", len(d.vaapiEncoders)).
		Msg("vaapi preflight: passed")
	return nil
}

// PreflightNVENC validates that the visible NVIDIA runtime can execute real NVENC encodes.
func (d *Detector) PreflightNVENC() error {
	publishEncoderGauges(nvencEncodersToTest, nil)
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
	// Fail-closed: NVENC has no decode-verify (testNVENCEncoder trusts exit 0 on a
	// discarded "-f null -" output), so av1_nvenc must not be admitted on an
	// encoder-ran signal alone — that would be a black-screen risk. See
	// clampUnverifiedNVENCAV1.
	clampUnverifiedNVENCAV1(d.nvencEncoderCaps, d.nvencEncoders)
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
	publishEncoderGauges(nvencEncodersToTest, d.nvencEncoderCaps)
	hardware.SetNVENCPreflightResult(true)
	d.Logger.Info().
		Int("verified_encoders", len(d.nvencEncoders)).
		Msg("nvenc preflight: passed")
	return nil
}

// clampUnverifiedNVENCAV1 enforces the fail-closed latch for NVENC AV1.
//
// NVENC has NO decode-verify: testNVENCEncoder discards output to "-f null -" and
// treats exit 0 as success, so DeriveHardwareEncoderCapabilities marks av1_nvenc
// Verified=true on "the encoder ran", NOT "the output is decodable". That is the
// exact gap B1 closed for VAAPI via decodeVerifyEncode, still wide open on NVENC —
// and unlike the VAAPI false-WITHHELD (too cautious, safe), an unvalidated av1_nvenc
// is false-VERIFIED: it can serve corrupt or black AV1 to a client that has no
// software AV1 fallback. Until NVENC grows its own decode-verify, av1_nvenc is
// reported UNVERIFIABLE and is NOT admitted — the honest three-state verdict,
// mirroring the VAAPI verify-oracle fix. h264_nvenc/hevc_nvenc stay verified
// (universally decodable, far lower consequence), so an NVIDIA host keeps hardware
// HEVC/H264 and only loses unvalidated AV1.
//
// It closes BOTH admission paths: the per-encoder cap (Verified=false ->
// SetNVENCEncoderCapabilities drops it -> IsNVENCEncoderReady/IsHardwareEncoderReady
// fail) and the detector bool map (delete -> NVENCEncoderVerified, the plan_codec
// gate, fails). The cap is retained with Verdict=unverifiable so the
// xg2g_gpu_encoder_unverifiable gauge surfaces the asymmetry instead of it reading
// as a plain "not present".
func clampUnverifiedNVENCAV1(caps map[string]hardware.NVENCEncoderCapability, verified map[string]bool) {
	const enc = "av1_nvenc"
	cap, ok := caps[enc]
	if !ok {
		return
	}
	delete(verified, enc)
	caps[enc] = hardware.NVENCEncoderCapability{
		Verdict:      hardware.VerdictUnverifiable,
		Reason:       "nvenc has no decode-verify; av1 output cannot be validated (black-screen risk) — admitted only once NVENC output validation exists",
		ProbeElapsed: cap.ProbeElapsed,
		// Verified / AutoEligible deliberately left false: fail-closed.
	}
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

// probeAndVerifyVaapiEncoder encodes a short synthetic clip with the given VAAPI
// encoder at the given upload format (e.g. "nv12" for 8-bit, "p010le" for
// 10-bit) to a real file, then decode-verifies the output. It returns the encode
// elapsed time and a three-state verdict (verified/withheld/unverifiable).
func (d *Detector) probeAndVerifyVaapiEncoder(encoder, uploadFormat string) (time.Duration, hardware.EncoderVerdict, string) {
	// Probe artifacts go under a guaranteed-writable working dir (HLSRoot, which
	// the media pipeline already writes segments to) so a read-only/distroless
	// rootfs with a non-writable /tmp does not turn a filesystem problem into a
	// false "unverifiable". A genuine setup failure is reported with a
	// filesystem-specific reason that is never confused with "no software decoder".
	tmpBase := d.HLSRoot
	if tmpBase == "" {
		tmpBase = os.TempDir()
	} else if mkErr := os.MkdirAll(tmpBase, 0o750); mkErr != nil {
		tmpBase = os.TempDir()
	}
	tmpDir, err := os.MkdirTemp(tmpBase, "encode-verify-*")
	if err != nil {
		return 0, hardware.VerdictUnverifiable, "probe setup failed (cannot create temp dir): " + err.Error()
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // cleans up on every path incl. withheld/unverifiable/crash
	outPath := filepath.Join(tmpDir, "probe.mkv")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dur := float64(decodeVerifyFrames)/25.0 + 0.04
	start := time.Now()
	// #nosec G204 -- BinPath/VaapiDevice are trusted config; outPath is a local temp.
	cmd := exec.CommandContext(ctx, d.BinPath,
		"-y",
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", fmt.Sprintf("testsrc2=size=1280x720:rate=25:duration=%0.2f", dur),
		"-vf", "format="+uploadFormat+",hwupload",
		"-c:v", encoder,
		"-frames:v", strconv.Itoa(decodeVerifyFrames),
		outPath,
	)
	if out, encErr := cmd.CombinedOutput(); encErr != nil {
		// Encoder failed to open/run: the hardware cannot produce this output.
		return time.Since(start), hardware.VerdictWithheld, fmt.Sprintf("encode failed: %v (%s)", encErr, tailSummary(string(out), 2))
	}
	elapsed := time.Since(start)
	verdict, reason := d.decodeVerifyEncode(ctx, outPath, normalizeRequestedCodec(encoder), decodeVerifyFrames)
	return elapsed, verdict, reason
}

// decodeVerifyEncode decode-verifies an encoded file with a forced *software*
// decoder, returning a three-state verdict. "verified" requires the output to
// decode to the full frame count, be non-black (mean YAVG >= threshold) and
// carry spatial content (max luma range >= decodeVerifyMinRange). If no software
// decoder for the codec exists it returns "unverifiable" (fail-closed: never
// trust "bytes were produced").
func (d *Detector) decodeVerifyEncode(ctx context.Context, path, codec string, expectedFrames int) (hardware.EncoderVerdict, string) {
	if d.decodeVerifyFn != nil {
		return d.decodeVerifyFn(ctx, path, codec, expectedFrames)
	}
	swDec, ok := d.softwareDecoderFor(codec)
	if !ok {
		return hardware.VerdictUnverifiable, fmt.Sprintf("no software %s decoder available to verify output", codec)
	}
	frames, meanYAvg, maxRange, err := d.decodeStats(ctx, path, swDec)
	if err != nil {
		// Decoder is present but decoding failed -> the bitstream is bad.
		return hardware.VerdictWithheld, fmt.Sprintf("software decode (%s) failed: %v", swDec, err)
	}
	minYAvg := d.minDecodeYAvg
	if minYAvg <= 0 {
		minYAvg = decodeVerifyMinYAvg
	}
	switch {
	case frames < expectedFrames:
		return hardware.VerdictWithheld, fmt.Sprintf("partial decode: %d/%d frames", frames, expectedFrames)
	case meanYAvg < minYAvg:
		return hardware.VerdictWithheld, fmt.Sprintf("output too dark: mean YAVG %.1f < %.1f", meanYAvg, minYAvg)
	case maxRange < decodeVerifyMinRange:
		return hardware.VerdictWithheld, fmt.Sprintf("flat output: max luma range %.1f < %.1f", maxRange, decodeVerifyMinRange)
	}
	return hardware.VerdictVerified, ""
}

// decodeStats software-decodes path with the given decoder and returns the
// decoded frame count, mean luma (YAVG) and the maximum per-frame luma range
// (YMAX-YMIN) via the signalstats filter.
func (d *Detector) decodeStats(ctx context.Context, path, swDecoder string) (frames int, meanYAvg float64, maxRange float64, err error) {
	if d.decodeStatsFn != nil {
		return d.decodeStatsFn(ctx, path, swDecoder)
	}
	// #nosec G204 -- BinPath/swDecoder are trusted; path is a local temp file.
	cmd := exec.CommandContext(ctx, d.BinPath,
		"-v", "info",
		"-c:v", swDecoder,
		"-i", path,
		"-vf", signalStatsLumaVF,
		"-f", "null", "-",
	)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return 0, 0, 0, fmt.Errorf("%w (%s)", runErr, tailSummary(string(out), 2))
	}
	var sum, curMin float64
	haveMin := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "lavfi.signalstats.YAVG="):
			if v, ok := parseSignalStat(line, "lavfi.signalstats.YAVG="); ok {
				frames++
				sum += v
			}
		case strings.Contains(line, "lavfi.signalstats.YMIN="):
			if v, ok := parseSignalStat(line, "lavfi.signalstats.YMIN="); ok {
				curMin, haveMin = v, true
			}
		case strings.Contains(line, "lavfi.signalstats.YMAX="):
			if v, ok := parseSignalStat(line, "lavfi.signalstats.YMAX="); ok && haveMin && v-curMin > maxRange {
				maxRange = v - curMin
			}
		}
	}
	if frames == 0 {
		return 0, 0, 0, errors.New("no decoded frames")
	}
	return frames, sum / float64(frames), maxRange, nil
}

func parseSignalStat(line, prefix string) (float64, bool) {
	_, after, ok := strings.Cut(line, prefix)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(after), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// softwareDecoderFor returns a *trusted* software decoder ffmpeg can use to
// validate output for the given codec, and whether one is available at all.
//
// For AV1 the native ffmpeg "av1" decoder is deliberately EXCLUDED from the
// trusted set. It is a parser / hardware front-end with no working standalone
// software decode path ("ffmpeg -h decoder=av1" lists only vaapi/vulkan), so it
// cannot decode real-world AV1 at ANY bit depth — it fails outright on both the
// encoder's 10-bit p010 output and 8-bit nv12 ("Error submitting packet to
// decoder: Function not implemented"). Trusting it as a verify-oracle turned "we
// have no decoder that can check this" into a false VerdictWithheld ("the HW
// can't encode AV1") instead of the honest VerdictUnverifiable ("we couldn't
// verify"). That confusion is exactly what the three-state verdict exists to
// prevent, so only libdav1d/libaom-av1 may verify AV1; their absence yields
// unverifiable, never withheld. The native hevc/h264 decoders are complete and
// stay trusted.
func (d *Detector) softwareDecoderFor(codec string) (string, bool) {
	if d.softwareDecoderFn != nil {
		return d.softwareDecoderFn(codec)
	}
	candidates := map[string][]string{
		// No native "av1": it is a parser/HW front-end that can't software-decode AV1
		// at any bit depth, so its presence must not flip an unverifiable host to a
		// false withheld.
		"av1":  {"libdav1d", "libaom-av1"},
		"hevc": {"hevc"},
		"h264": {"h264"},
	}
	avail := d.availableDecoders()
	for _, dec := range candidates[normalizeRequestedCodec(codec)] {
		if avail[dec] {
			return dec, true
		}
	}
	return "", false
}

// availableDecoders parses `ffmpeg -decoders` once and caches the decoder names.
func (d *Detector) availableDecoders() map[string]bool {
	d.decodersOnce.Do(func() {
		set := map[string]bool{}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// #nosec G204 -- BinPath is trusted from config.
		out, err := exec.CommandContext(ctx, d.BinPath, "-hide_banner", "-decoders").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				// Lines look like " V....D libdav1d AV1 ...": flags then name.
				if len(fields) >= 2 && strings.HasPrefix(fields[0], "V") {
					set[fields[1]] = true
				}
			}
		}
		d.decodersAvail = set
	})
	return d.decodersAvail
}

// tailSummary returns the last n non-empty lines of s joined by "; ".
func tailSummary(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
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

// VaapiEncoder10BitVerified reports whether the encoder's output was
// decode-verified at 10-bit (p010le) — the depth production drives AV1 at.
func (d *Detector) VaapiEncoder10BitVerified(encoder string) bool {
	return d.vaapiEncoderCaps[encoder].Verified10Bit
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
	filter := "format=" + vaapiInterlacedProbeUploadFormat(encoder) + ",setfield=tff,hwupload,deinterlace_vaapi"
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
		"-vf", signalStatsLumaVF,
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
