package ffmpeg

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	codecdecision "github.com/ManuGH/xg2g/internal/decision"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	infraffmpeg "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

const experimentalInterlacedVAAPICodecsEnv = "XG2G_EXPERIMENTAL_ALLOW_UNVERIFIED_INTERLACED_VAAPI_CODECS"

type inputPlan struct {
	args     []string
	inputURL string
}

type codecPlan struct {
	resolvedCodec string
	useHW         bool
	hwBackend     profiles.GPUBackend
	fullVAAPI     bool
	preInputArgs  []string
	pathID        string
}

type outputPlan struct {
	args             []string
	effectiveProfile ports.ProfileSpec
}

type finalizedPlan struct {
	args             []string
	effectiveProfile ports.ProfileSpec
	pathID           string
}

type liveSegmentLayout struct {
	segmentDurationSec     int
	initSegmentDurationSec int
	listSize               int
}

func (a *LocalAdapter) buildArgsWithPlan(ctx context.Context, spec ports.StreamSpec, inputURL string) (finalizedPlan, error) {
	inputPhase, err := a.planInput(spec, inputURL)
	if err != nil {
		return finalizedPlan{}, err
	}
	if spec.Mode == ports.ModeLive {
		spec = a.FinalizePlan(ctx, spec, inputPhase.inputURL)
	}

	codecPhase, err := a.planCodec(spec)
	if err != nil {
		return finalizedPlan{}, err
	}

	args := append([]string{}, codecPhase.preInputArgs...)
	args = append(args, inputPhase.args...)
	args = append(args, "-progress", "pipe:2")
	result := finalizedPlan{
		args:             args,
		effectiveProfile: spec.Profile,
		pathID:           codecPhase.pathID,
	}

	if spec.Mode == ports.ModeLive {
		liveOutput, err := a.planLiveOutput(ctx, spec, inputPhase, codecPhase)
		if err != nil {
			return finalizedPlan{}, err
		}
		result.args = append(result.args, liveOutput.args...)
		result.effectiveProfile = liveOutput.effectiveProfile
	}

	return result, nil
}

func (a *LocalAdapter) planCodec(spec ports.StreamSpec) (codecPlan, error) {
	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, normalizeRequestedCodec(spec.Profile.VideoCodec)) {
		resolvedCodec := normalizeRequestedCodec(spec.Profile.VideoCodec)
		if resolvedCodec == "" {
			resolvedCodec = "h264"
		}
		metrics.RecordDecisionSummary(
			spec.Profile.Name,
			"direct",
			resolvedCodec,
			false,
			"direct_play_supported",
		)

		a.Logger.Info().
			Str("event", "decision.summary").
			Str("profile", spec.Profile.Name).
			Str("requested_codec", resolvedCodec).
			Strs("supported_hw_codecs", a.supportedHWCodecs()).
			Bool("hwaccel_available", false).
			Str("path", "direct").
			Str("output_codec", resolvedCodec).
			Bool("use_hwaccel", false).
			Str("reason", "direct_play_supported").
			Msg("decision summary")

		return codecPlan{
			resolvedCodec: resolvedCodec,
			useHW:         false,
			hwBackend:     profiles.GPUBackendNone,
			fullVAAPI:     false,
			preInputArgs:  nil,
		}, nil
	}

	requestedBackend := backendForHWAccel(spec.Profile.HWAccel)
	useHWPath := requestedBackend != profiles.GPUBackendNone

	hardHWRequest := requestedBackend != profiles.GPUBackendNone && !isPreferHWProfile(spec.Profile.Name)
	decisionIn := codecdecision.Input{
		Profile:        spec.Profile.Name,
		RequestedCodec: spec.Profile.VideoCodec,
		RequireHW:      hardHWRequest,
		Server: codecdecision.ServerCapabilities{
			HWAccelAvailable:  useHWPath,
			SupportedHWCodecs: a.supportedHWCodecs(),
			AutoHWCodecs:      a.autoHWCodecs(),
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
		Strs("auto_hw_codecs", decisionInSummary.AutoHWCodecs).
		Bool("hwaccel_available", decisionInSummary.HWAccelAvailable).
		Str("path", decisionOutSummary.Path).
		Str("output_codec", decisionOutSummary.OutputCodec).
		Bool("use_hwaccel", decisionOutSummary.UseHWAccel).
		Str("reason", decisionOutSummary.Reason).
		Msg("decision summary")

	if neg.Path == codecdecision.PathReject && !hardHWRequest {
		return codecPlan{}, fmt.Errorf("codec negotiation rejected (profile=%s codec=%s reason=%s)", spec.Profile.Name, spec.Profile.VideoCodec, neg.Reason)
	}

	resolvedCodec := neg.OutputCodec
	if resolvedCodec == "" && hardHWRequest {
		resolvedCodec = normalizeRequestedCodec(spec.Profile.VideoCodec)
	}
	if resolvedCodec == "" {
		resolvedCodec = "h264"
	}

	useHW := neg.Path == codecdecision.PathTranscodeHW
	if hardHWRequest {
		useHW = true
	}
	hwBackend := profiles.GPUBackendNone
	fullVAAPI := false
	pathID := ""

	preInputArgs := make([]string, 0, 6)
	if useHW {
		hwBackend = requestedBackend
		if hwBackend == profiles.GPUBackendNone {
			hwBackend = a.preferredHardwareBackendForCodec(resolvedCodec)
		}
		if hwBackend == profiles.GPUBackendNone {
			return codecPlan{}, fmt.Errorf("hardware path selected but no verified backend is available for codec %s", resolvedCodec)
		}

		switch hwBackend {
		case profiles.GPUBackendVAAPI:
			if spec.Profile.Deinterlace && !vaapiInterlacedCodecIsSafe(resolvedCodec) {
				if experimentalAllowUnverifiedInterlacedVAAPICodec(resolvedCodec) {
					a.Logger.Warn().
						Str("requested_codec", resolvedCodec).
						Str("override_env", experimentalInterlacedVAAPICodecsEnv).
						Msg("allowing unverified interlaced vaapi codec via experimental override")
				} else if !a.anyVerifiedVAAPIInterlacedPathForCodec(resolvedCodec, profiles.IsFullVAAPIProfile(spec.Profile.HWAccel)) {
					a.Logger.Warn().
						Str("requested_codec", resolvedCodec).
						Str("fallback_codec", "h264").
						Msg("interlaced vaapi codec downgraded until path correctness is verified")
					resolvedCodec = "h264"
				}
			}
			if a.VaapiDevice == "" {
				return codecPlan{}, fmt.Errorf("vaapi requested by profile but no vaapi device configured on adapter")
			}
			reqEncoder, ok := codecToVAAPIEncoder(resolvedCodec)
			if !ok {
				return codecPlan{}, fmt.Errorf("unsupported vaapi codec resolved by decision engine: %s", resolvedCodec)
			}
			if !a.VaapiEncoderVerified(reqEncoder) {
				return codecPlan{}, fmt.Errorf("vaapi encoder %s not verified by preflight (device=%s, deviceErr=%v)", reqEncoder, a.VaapiDevice, a.vaapiDeviceErr)
			}
			preInputArgs = append(preInputArgs, "-vaapi_device", a.VaapiDevice)
			fullVAAPI = profiles.IsFullVAAPIProfile(spec.Profile.HWAccel)
			if normalizeRequestedCodec(resolvedCodec) == "av1" {
				// Keep AV1 on the encode-only path even when a caller requested
				// full VAAPI. This preserves a software-domain normalization step
				// before hwupload, which is required to avoid malformed 1080p AV1
				// output on current AMD VAAPI stacks.
				fullVAAPI = false
			}
			if spec.Profile.Deinterlace {
				fullPathID := vaapiPathCorrectnessIDFor(resolvedCodec, true)
				if fullVAAPI && fullPathID != "" {
					capability, ok := hardware.HardwarePathCapabilityFor(fullPathID)
					if !ok || capability.Status != hardware.PathStatusVerified {
						fullVAAPI = false
						a.Logger.Info().
							Str("path_id", fullPathID).
							Str("status", capability.Status).
							Str("reason", capability.Reason).
							Msg("vaapi full pipeline disabled by path correctness matrix")
					}
				}
				if fullVAAPI {
					pathID = fullPathID
				} else {
					pathID = vaapiPathCorrectnessIDFor(resolvedCodec, false)
				}
			}
			if fullVAAPI {
				preInputArgs = append(preInputArgs,
					"-hwaccel", "vaapi",
					"-hwaccel_output_format", "vaapi",
				)
			}
		case profiles.GPUBackendNVENC:
			reqEncoder, ok := codecToNVENCEncoder(resolvedCodec)
			if !ok {
				return codecPlan{}, fmt.Errorf("unsupported nvenc codec resolved by decision engine: %s", resolvedCodec)
			}
			if !a.NVENCEncoderVerified(reqEncoder) {
				return codecPlan{}, fmt.Errorf("nvenc encoder %s not verified by preflight (nvencErr=%v)", reqEncoder, a.nvencErr)
			}
		default:
			return codecPlan{}, fmt.Errorf("unsupported hardware backend %q", hwBackend)
		}
	}

	return codecPlan{
		resolvedCodec: resolvedCodec,
		useHW:         useHW,
		hwBackend:     hwBackend,
		fullVAAPI:     fullVAAPI,
		preInputArgs:  preInputArgs,
		pathID:        pathID,
	}, nil
}

func vaapiPathCorrectnessIDFor(codec string, full bool) string {
	switch strings.TrimSpace(codec) {
	case "hevc":
		if full {
			return hardware.PathVAAPIFullInterlacedHEVC
		}
		return hardware.PathVAAPIEncodeOnlyInterlacedHEVC
	case "av1":
		if full {
			return hardware.PathVAAPIFullInterlacedAV1
		}
		return hardware.PathVAAPIEncodeOnlyInterlacedAV1
	default:
		return ""
	}
}

func vaapiInterlacedCodecIsSafe(codec string) bool {
	switch strings.TrimSpace(codec) {
	case "hevc", "av1":
		return false
	default:
		return true
	}
}

func (a *LocalAdapter) anyVerifiedVAAPIInterlacedPathForCodec(codec string, fullRequested bool) bool {
	codec = normalizeRequestedCodec(codec)
	if codec == "av1" {
		// AV1 VAAPI is intentionally forced through encode-only later in the
		// planner, so a verified full path alone must not unlock it.
		fullRequested = false
	}

	candidates := []bool{false}
	if fullRequested {
		candidates = []bool{true, false}
	}
	for _, full := range candidates {
		pathID := vaapiPathCorrectnessIDFor(codec, full)
		if pathID == "" {
			continue
		}
		capability, ok := hardware.HardwarePathCapabilityFor(pathID)
		if ok && capability.Status == hardware.PathStatusVerified {
			return true
		}
	}
	return false
}

func experimentalAllowUnverifiedInterlacedVAAPICodec(codec string) bool {
	codec = normalizeRequestedCodec(codec)
	if codec != "hevc" && codec != "av1" {
		return false
	}
	raw := strings.TrimSpace(config.ParseString(experimentalInterlacedVAAPICodecsEnv, ""))
	if raw == "" {
		return false
	}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		if normalizeRequestedCodec(item) == codec {
			return true
		}
	}
	return false
}

func strictLiveIngestCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hevc", "av1":
		return true
	default:
		return false
	}
}

func (a *LocalAdapter) planInput(spec ports.StreamSpec, inputURL string) (inputPlan, error) {
	fflags := strings.TrimSpace(a.IngestFFlags)
	if fflags == "" {
		fflags = "+genpts+discardcorrupt+flush_packets"
	}
	analyzeDuration := strings.TrimSpace(a.AnalyzeDuration)
	probeSize := strings.TrimSpace(a.ProbeSize)
	baseInputArgs := make([]string, 0, 20)
	strictIngest := spec.Source.Type != ports.SourceFile &&
		spec.Profile.TranscodeVideo &&
		strictLiveIngestCodec(spec.Profile.VideoCodec)
	if v := strings.TrimSpace(a.IngestErrDetect); v != "" && !strictIngest {
		baseInputArgs = append(baseInputArgs, "-err_detect", v)
	}
	if v := strings.TrimSpace(a.IngestMaxErrorRate); v != "" && !strictIngest {
		baseInputArgs = append(baseInputArgs, "-max_error_rate", v)
	}
	baseInputArgs = append(baseInputArgs, "-ignore_unknown")
	if spec.Source.Type != ports.SourceFile {
		if liveAnalyze := strings.TrimSpace(a.LiveAnalyzeDuration); liveAnalyze != "" {
			analyzeDuration = liveAnalyze
		}
		if liveProbe := strings.TrimSpace(a.LiveProbeSize); liveProbe != "" {
			probeSize = liveProbe
		}
		// Stream relay MPEG-TS often needs a much deeper initial probe before
		// FFmpeg can resolve video dimensions and audio layout reliably.
		// Keep that only for video-transcode paths; passthrough/remux sessions
		// already know enough about the stream and pay a visible startup penalty.
		if isStreamRelayURL(inputURL) {
			if spec.Profile.TranscodeVideo {
				analyzeDuration = "10000000"
				probeSize = "20M"
			}
		}
		if !strings.Contains(fflags, "igndts") {
			fflags += "+igndts"
		}
		if a.LiveNoBuffer && !strings.Contains(fflags, "nobuffer") {
			fflags += "+nobuffer"
		}

		headers := "Icy-MetaData: 1\r\n"
		if u, err := url.Parse(inputURL); err == nil && u.User != nil {
			if pwd, ok := u.User.Password(); ok {
				auth := u.User.Username() + ":" + pwd
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			} else {
				auth := u.User.Username() + ":"
				headers += "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth)) + "\r\n"
			}
			// Credentials now travel in the Authorization header; strip them from
			// the URL so they cannot leak into ffmpeg's argv (/proc/<pid>/cmdline)
			// or any logged command line.
			u.User = nil
			inputURL = u.String()
		}

		baseInputArgs = append(baseInputArgs,
			"-avoid_negative_ts", "make_zero",
			"-user_agent", "VLC/3.0.21 LibVLC/3.0.21",
			"-headers", headers,
		)
		if v := strings.TrimSpace(a.IngestFlags2); v != "" && !strictIngest {
			baseInputArgs = append(baseInputArgs, "-flags2", v)
		}
	}
	baseInputArgs = append([]string{"-fflags", fflags}, baseInputArgs...)
	if analyzeDuration != "" {
		baseInputArgs = append(baseInputArgs, "-analyzeduration", analyzeDuration)
	}
	if probeSize != "" {
		baseInputArgs = append(baseInputArgs, "-probesize", probeSize)
	}

	netInputArgs := append([]string{}, baseInputArgs...)
	if whitelist, ok := infraffmpeg.InputProtocolWhitelist(inputURL); ok {
		netInputArgs = append(netInputArgs, "-protocol_whitelist", whitelist)
	}
	netInputArgs = append(netInputArgs,
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-reconnect_on_network_error", "1",
		"-reconnect_on_http_error", "4xx,5xx",
	)

	phase := inputPlan{inputURL: inputURL}
	switch spec.Source.Type {
	case ports.SourceTuner:
		if phase.inputURL == "" {
			return inputPlan{}, fmt.Errorf("missing stream url for tuner source")
		}
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceURL:
		if phase.inputURL == "" {
			phase.inputURL = spec.Source.ID
		}
		phase.args = append(phase.args, netInputArgs...)
		phase.args = append(phase.args, "-i", phase.inputURL)
	case ports.SourceFile:
		phase.args = append(phase.args, baseInputArgs...)
		phase.args = append(phase.args, "-re", "-i", spec.Source.ID)
	default:
		return inputPlan{}, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}

	return phase, nil
}

func (a *LocalAdapter) planLiveOutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan) (outputPlan, error) {
	layout, err := a.planLiveSegmentLayout(spec)
	if err != nil {
		return outputPlan{}, err
	}
	spec.Profile.VideoCodec = codec.resolvedCodec
	fps := a.resolveLiveFPS(ctx, spec, input.inputURL)
	fps = a.adjustLiveFPSForRuntimeServiceOverride(spec, input.inputURL, fps)
	targetOutputFPS := targetLiveOutputFPS(spec)
	gopFPS := fps
	if targetOutputFPS > 0 {
		gopFPS = targetOutputFPS
	}
	gop := gopFPS * layout.segmentDurationSec

	out := outputPlan{effectiveProfile: spec.Profile}
	out.args = append(out.args,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
	)
	if targetOutputFPS > 0 {
		out.args = append(out.args, "-r", strconv.Itoa(targetOutputFPS))
	}

	out.args = a.buildLiveVideoOutputArgs(out.args, spec, input.inputURL, codec, gop, layout.segmentDurationSec)
	out.args = appendLiveVideoContainerTags(out.args, spec, codec.resolvedCodec)
	out.args = appendLiveAudioArgs(out.args, spec)
	out.args = a.appendLiveHLSArgs(out.args, spec, layout)
	out.args = append(out.args, a.prepareLiveOutputPath(spec.SessionID))

	return out, nil
}

func (a *LocalAdapter) planLiveSegmentLayout(spec ports.StreamSpec) (liveSegmentLayout, error) {
	layout := liveSegmentLayout{
		segmentDurationSec: a.SegmentSeconds,
		listSize:           10,
	}
	if shouldUseShortFMP4StartupSegments(spec) && layout.segmentDurationSec > safariDirtyHLSTimeSec {
		layout.segmentDurationSec = safariDirtyHLSTimeSec
		layout.initSegmentDurationSec = safariDirtyHLSInitTimeSec
		if layout.initSegmentDurationSec > layout.segmentDurationSec {
			layout.initSegmentDurationSec = layout.segmentDurationSec
		}
	}
	if layout.segmentDurationSec <= 0 {
		return liveSegmentLayout{}, fmt.Errorf("invalid hls segment seconds: %d", layout.segmentDurationSec)
	}
	if a.DVRWindow > 0 {
		layout.listSize = int(math.Ceil(a.DVRWindow.Seconds() / float64(layout.segmentDurationSec)))
		if layout.listSize < 3 {
			layout.listSize = 3
		}
	}
	return layout, nil
}

func shouldUseShortFMP4StartupSegments(spec ports.StreamSpec) bool {
	if spec.Mode != ports.ModeLive || !spec.Profile.TranscodeVideo {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return false
	}

	switch profiles.NormalizeRequestedProfileID(spec.Profile.Name) {
	case profiles.ProfileSafariDirty,
		profiles.ProfileSafariHEVCHW,
		profiles.ProfileSafariHEVCHWLL,
		profiles.ProfileAV1HW:
		// Native iOS fMP4 transcodes benefit from a denser startup GOP cadence.
		// The default 6-second layout makes first attach visibly sluggish.
		return true
	default:
		return false
	}
}

func (a *LocalAdapter) defaultLiveFPS(spec ports.StreamSpec, inputURL string) int {
	fps := a.FPSFallback
	if fps <= 0 {
		fps = 25
	}
	if (spec.Source.Type != ports.SourceTuner && !isStreamRelayURL(inputURL)) && !spec.Profile.Deinterlace {
		fps = 30
	}
	if spec.Profile.Deinterlace || strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") {
		fps = a.FPSFallbackInter
	}
	if fps <= 0 {
		fps = 25
	}
	return fps
}

func (a *LocalAdapter) resolveLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL string) int {
	fps := a.defaultLiveFPS(spec, inputURL)
	sourceKey := fpsCacheKey(spec.Source, inputURL)
	if resolved, skipProbe := a.resolveSkippedLiveFPS(ctx, spec, inputURL, sourceKey, fps); skipProbe {
		return resolved
	}
	return a.resolveProbedLiveFPS(ctx, spec, inputURL, sourceKey, fps)
}

func (a *LocalAdapter) resolveSkippedLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, fallback int) (int, bool) {
	if isStreamRelayURL(inputURL) {
		logEvt := a.Logger.Info().
			Str("session_id", spec.SessionID).
			Str("startup_phase", "fps_probe_skipped_streamrelay").
			Str("source_key", sourceKey).
			Str("input_url", sanitizeURLForLog(inputURL))
		if cachedFPS, ok := a.cachedFPS(sourceKey); ok {
			logEvt.Int("cached_fps", cachedFPS).
				Msg("skipping fps probe for stream relay input; using cached fps")
			return cachedFPS, true
		}
		logEvt.Int("fallback_fps", fallback).
			Msg("skipping fps probe for stream relay input; using fallback fps")
		return fallback, true
	}
	if sourceKey == "" {
		return fallback, false
	}
	cachedFPS, ok := a.cachedFPS(sourceKey)
	if !ok {
		return fallback, false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_cache_available").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("fps cache available before probe")
	if !a.SkipFPSProbeOnCache {
		return fallback, false
	}
	if a.shouldAbortCachedFPSSkip(ctx, spec, inputURL, sourceKey, cachedFPS) {
		return fallback, false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_skipped").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("skipping fps probe because cached fps is available")
	return cachedFPS, true
}

func (a *LocalAdapter) shouldAbortCachedFPSSkip(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, cachedFPS int) bool {
	if a.SkipFPSProbeWarmup <= 0 || !isHTTPInputURL(inputURL) {
		return false
	}
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_warmup_started").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Dur("warmup_duration", a.SkipFPSProbeWarmup).
		Msg("starting cache-hit stream warmup")
	warmupResult, warmupErr := a.warmupInputStream(ctx, inputURL, a.SkipFPSProbeWarmup)
	warmupEvt := a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_warmup_finished").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Int("warmup_bytes", warmupResult.bytes).
		Int("http_status", warmupResult.httpStatus).
		Int64("latency_ms", warmupResult.latencyMs)
	if warmupErr != nil {
		warmupEvt = warmupEvt.Err(warmupErr)
	}
	warmupEvt.Msg("cache-hit stream warmup finished")
	if warmupErr == nil {
		return false
	}
	a.Logger.Warn().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_skip_aborted").
		Str("source_key", sourceKey).
		Int("cached_fps", cachedFPS).
		Msg("cache-hit stream warmup failed, falling back to fps probe")
	return true
}

func (a *LocalAdapter) resolveProbedLiveFPS(ctx context.Context, spec ports.StreamSpec, inputURL, sourceKey string, fallback int) int {
	a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_started").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL)).
		Msg("fps probe started")
	detected, basis, err := a.probeFPS(ctx, inputURL)
	probeEvt := a.Logger.Info().
		Str("session_id", spec.SessionID).
		Str("startup_phase", "fps_probe_finished").
		Str("source_key", sourceKey).
		Str("input_url", sanitizeURLForLog(inputURL))
	if err != nil {
		probeEvt = probeEvt.Err(err)
	} else {
		probeEvt = probeEvt.Int("detected_fps", detected).Str("fps_basis", basis)
	}
	probeEvt.Msg("fps probe finished")
	if err == nil && a.isValidFPS(detected) {
		if sourceKey != "" {
			a.setLastKnownFPS(sourceKey, detected)
		}
		a.Logger.Debug().
			Str("sessionId", spec.SessionID).
			Int("fps", detected).
			Str("fps_basis", basis).
			Str("url", sanitizeURLForLog(inputURL)).
			Msg("detected input fps")
		return detected
	}
	if err == nil {
		err = fmt.Errorf("detected fps out of range: %d", detected)
	}
	if cachedFPS, ok := a.cachedFPS(sourceKey); ok {
		a.Logger.Warn().
			Str("sessionId", spec.SessionID).
			Err(err).
			Int("cached_fps", cachedFPS).
			Str("fps_basis", "last_known_source").
			Str("source_key", sourceKey).
			Int("fps_min", a.FPSMin).
			Int("fps_max", a.FPSMax).
			Str("url", sanitizeURLForLog(inputURL)).
			Msg("fps detection failed, using last known source fps")
		return cachedFPS
	}
	a.Logger.Warn().
		Str("sessionId", spec.SessionID).
		Err(err).
		Int("fallback_fps", fallback).
		Int("fps_min", a.FPSMin).
		Int("fps_max", a.FPSMax).
		Str("url", sanitizeURLForLog(inputURL)).
		Msg("fps detection failed, using fallback")
	return fallback
}

func (a *LocalAdapter) cachedFPS(sourceKey string) (int, bool) {
	if sourceKey == "" {
		return 0, false
	}
	cachedFPS, ok := a.getLastKnownFPS(sourceKey)
	if !ok || !a.isValidFPS(cachedFPS) {
		return 0, false
	}
	return cachedFPS, true
}

func (a *LocalAdapter) isValidFPS(fps int) bool {
	return fps >= a.FPSMin && fps <= a.FPSMax
}

func (a *LocalAdapter) adjustLiveFPSForRuntimeServiceOverride(spec ports.StreamSpec, inputURL string, fps int) int {
	if !shouldForce25FPSForLiveProfile(spec, inputURL) {
		return fps
	}

	targetFPS := 25
	if fps == targetFPS {
		return fps
	}

	logEvt := a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Int("cached_or_detected_fps", fps).
		Int("override_fps", targetFPS).
		Str("runtime_mode", string(effectiveLiveRuntimeMode(spec.Profile)))
	if serviceRef := safariRuntimeServiceRef(spec, inputURL); serviceRef != "" {
		logEvt = logEvt.Str("service_ref", serviceRef)
	}
	logEvt.Msg("forcing 25fps for live runtime profile")
	return targetFPS
}

func shouldForce25FPSForLiveProfile(spec ports.StreamSpec, inputURL string) bool {
	if shouldForceSafariHQ25ForServiceRef(spec, inputURL) {
		return true
	}
	if shouldForceSafariHQ50ForServiceRef(spec, inputURL) {
		return false
	}
	if shouldForceSafariHQForServiceRef(spec, inputURL) {
		return shouldForce25FPSForSafariHQ(spec.Profile)
	}
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return false
	}
	return normalizeRequestedCodec(spec.Profile.VideoCodec) == "hevc"
}

func effectiveLiveRuntimeMode(profile ports.ProfileSpec) ports.RuntimeMode {
	if profile.EffectiveRuntimeMode != "" && profile.EffectiveRuntimeMode != ports.RuntimeModeUnknown {
		return profile.EffectiveRuntimeMode
	}
	if profile.PolicyModeHint != "" && profile.PolicyModeHint != ports.RuntimeModeUnknown {
		return profile.PolicyModeHint
	}
	return profiles.RuntimeModeHintFromProfile(profile)
}

func targetLiveOutputFPS(spec ports.StreamSpec) int {
	if spec.Mode != ports.ModeLive {
		return 0
	}
	if !spec.Profile.TranscodeVideo {
		return 0
	}
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return 0
	}
	return 25
}

func (a *LocalAdapter) buildLiveVideoOutputArgs(args []string, spec ports.StreamSpec, inputURL string, codec codecPlan, gop, segmentDurationSec int) []string {
	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, codec.resolvedCodec) {
		return a.buildCopyVideoArgs(args, spec, inputURL)
	}
	if codec.useHW {
		switch codec.hwBackend {
		case profiles.GPUBackendVAAPI:
			if codec.fullVAAPI {
				return a.buildVaapiVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
			}
			return a.buildVaapiEncodeOnlyVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		case profiles.GPUBackendNVENC:
			return a.buildNVENCVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		}
	}
	return a.buildCPUVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
}

func appendLiveAudioArgs(args []string, spec ports.StreamSpec) []string {
	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	return append(args,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-ac", "2",
		"-ar", "48000",
		"-sn",
		"-f", "hls",
	)
}

func appendLiveVideoContainerTags(args []string, spec ports.StreamSpec, outputCodec string) []string {
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return args
	}
	if !strings.EqualFold(strings.TrimSpace(outputCodec), "hevc") {
		return args
	}
	return append(args, "-tag:v", "hvc1")
}

func (a *LocalAdapter) appendLiveHLSArgs(args []string, spec ports.StreamSpec, layout liveSegmentLayout) []string {
	segmentType := "mpegts"
	sessionDir := ports.SessionHLSDir(a.HLSRoot, spec.SessionID)
	segmentFilename := filepath.Join(sessionDir, "seg_%06d.ts")
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		segmentType = "fmp4"
		segmentFilename = filepath.Join(sessionDir, "seg_%06d.m4s")
	}
	args = append(args,
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
		"-hls_segment_type", segmentType,
		"-hls_segment_filename", segmentFilename,
	)
	if segmentType == "fmp4" {
		args = append(args, "-hls_fmp4_init_filename", "init.mp4")
	}
	if layout.initSegmentDurationSec > 0 {
		args = append(args, "-hls_init_time", strconv.Itoa(layout.initSegmentDurationSec))
	}
	return args
}

func (a *LocalAdapter) prepareLiveOutputPath(sessionID string) string {
	outputPath := filepath.Join(ports.SessionHLSDir(a.HLSRoot, sessionID), "index.m3u8")
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	if markerPath := ports.SessionFirstFrameMarkerPath(a.HLSRoot, sessionID); markerPath != "" {
		_ = os.Remove(markerPath)
	}
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "output_dir_ready").
		Str("output_path", outputPath).
		Msg("output directory ready")
	return outputPath
}

func (a *LocalAdapter) runSafariRuntimeProbeWithRetry(ctx context.Context, sessionID, inputURL string, probeTimeout time.Duration) (*vod.StreamInfo, error) {
	const maxAttempts = 2

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		info, err := a.runSafariRuntimeProbeOnce(attemptCtx, inputURL)
		cancel()
		if err == nil {
			if attempt > 1 {
				a.Logger.Info().
					Str("sessionId", sessionID).
					Str("input_url", sanitizeURLForLog(inputURL)).
					Int("attempt", attempt).
					Msg("safari runtime probe recovered after transient startup error")
			}
			return info, nil
		}

		lastErr = err
		if attempt == maxAttempts || !shouldRetrySafariRuntimeProbe(err, inputURL) {
			break
		}

		a.Logger.Info().
			Err(err).
			Str("sessionId", sessionID).
			Str("input_url", sanitizeURLForLog(inputURL)).
			Int("attempt", attempt).
			Msg("safari runtime probe transient startup error; retrying")

		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, lastErr
}

func (a *LocalAdapter) runSafariRuntimeProbeOnce(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
	if a.streamProbeFn != nil {
		return a.streamProbeFn(ctx, inputURL)
	}
	return infraffmpeg.ProbeWithBin(ctx, a.FFprobeBin, inputURL)
}

func shouldRetrySafariRuntimeProbe(err error, inputURL string) bool {
	if err == nil || !isStreamRelayURL(inputURL) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	msg := strings.ToLower(err.Error())
	transientMarkers := []string{
		"stream ends prematurely",
		"input/output error",
		"non-existing pps",
		"decode_slice_header error",
		"no frame!",
		"mmco: unref short failure",
	}
	for _, marker := range transientMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func usesLegacyCPUDefaults(spec ports.StreamSpec, outputCodec string) bool {
	prof := spec.Profile
	return prof.Name == "" && prof.VideoCodec == "" && !prof.TranscodeVideo && outputCodec == "h264"
}

func (a *LocalAdapter) buildVaapiVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi")

	if prof.Deinterlace {
		args = append(args, "-vf", "deinterlace_vaapi")
	}

	encoder := "h264_vaapi"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_vaapi"
	case "av1":
		encoder = "av1_vaapi"
	}
	args = append(args, "-c:v", encoder)
	args = appendVaapiRateControlArgs(args, prof, outputCodec)
	args = appendConservativeHEVCVAAPIArgs(args, spec, outputCodec)

	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
	)

	if normalizeRequestedCodec(outputCodec) != "av1" {
		args = append(args, "-profile:v", "main")
	}
	return args
}

func (a *LocalAdapter) buildVaapiEncodeOnlyVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "vaapi_encode_only").
		Str("vaapi.device", a.VaapiDevice).
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: vaapi encode only")

	filter := a.vaapiEncodeOnlyFilter(spec, outputCodec)
	args = append(args, "-vf", filter)

	encoder := "h264_vaapi"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_vaapi"
	case "av1":
		encoder = "av1_vaapi"
	}
	args = append(args, "-c:v", encoder)
	args = appendVaapiRateControlArgs(args, prof, outputCodec)
	args = appendConservativeHEVCVAAPIArgs(args, spec, outputCodec)

	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
	)
	if normalizeRequestedCodec(outputCodec) != "av1" {
		args = append(args, "-profile:v", "main")
	}
	return args
}

func (a *LocalAdapter) vaapiEncodeOnlyFilter(spec ports.StreamSpec, outputCodec string) string {
	parts := make([]string, 0, 4)
	if spec.Profile.Deinterlace {
		parts = append(parts, a.deinterlaceFilterForProfile(spec))
	}
	if normalizeRequestedCodec(outputCodec) == "av1" {
		parts = append(parts, av1VAAPIGeometryPadFilter())
	}
	parts = append(parts, "format=nv12", "hwupload")
	return strings.Join(parts, ",")
}

func av1VAAPIGeometryPadFilter() string {
	// Current AMD VAAPI AV1 encoders can emit 1080p bitstreams that decode as
	// 1082-line frames. Padding the software-domain input to a 16-line height
	// keeps the decoded geometry stable for native HLS clients. Adjust SAR
	// before padding so display aspect ratio remains unchanged after 1088p.
	return "setsar=sar=sar*ceil(h/16)*16/h:max=1000,pad=iw:ceil(ih/16)*16:0:(oh-ih)/2:black"
}

func appendVaapiRateControlArgs(args []string, prof ports.ProfileSpec, outputCodec string) []string {
	isAV1 := normalizeRequestedCodec(outputCodec) == "av1"
	if prof.VideoQP > 0 {
		args = append(args,
			"-rc_mode", "CQP",
			"-qp", strconv.Itoa(prof.VideoQP),
		)
		if prof.VideoMaxRateK > 0 {
			args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		}
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		if isAV1 {
			args = append(args, "-async_depth", "1")
		}
		return args
	}

	if prof.VideoMaxRateK > 0 {
		// AMD VAAPI AV1 (Phoenix3 / VCN4) stalls the VCN ring when -b:v == -maxrate.
		// Use VBR with a 25% target headroom to keep the encoder ring stable.
		bV := prof.VideoMaxRateK
		if isAV1 {
			bV = (prof.VideoMaxRateK * 3) / 4
			if bV < 1 {
				bV = 1
			}
		}
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", bV),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		if isAV1 {
			args = append(args, "-async_depth", "1")
		}
		return args
	}

	if isAV1 {
		args = append(args, "-rc_mode", "CQP", "-global_quality", "28", "-async_depth", "1")
		return args
	}
	return append(args, "-global_quality", "23")
}

func (a *LocalAdapter) buildNVENCVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "nvenc").
		Str("video.codec", outputCodec).
		Int("video.qp", prof.VideoQP).
		Int("video.maxRateK", prof.VideoMaxRateK).
		Int("video.bufSizeK", prof.VideoBufSizeK).
		Bool("deinterlace", prof.Deinterlace).
		Msg("pipeline video: nvenc")

	if prof.Deinterlace {
		args = append(args, "-vf", a.deinterlaceFilterForProfile(spec))
	}

	encoder := "h264_nvenc"
	switch outputCodec {
	case "hevc":
		encoder = "hevc_nvenc"
	case "av1":
		encoder = "av1_nvenc"
	}
	args = append(args, "-c:v", encoder)
	args = appendNVENCRateControlArgs(args, prof)
	args = append(args,
		"-g", strconv.Itoa(gop),
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSec),
		"-flags", "+cgop",
	)
	if outputCodec != "av1" {
		args = append(args, "-profile:v", "main")
	}
	return args
}

func appendNVENCRateControlArgs(args []string, prof ports.ProfileSpec) []string {
	if prof.VideoQP > 0 {
		args = append(args,
			"-rc", "constqp",
			"-qp", strconv.Itoa(prof.VideoQP),
		)
		if prof.VideoMaxRateK > 0 {
			args = append(args, "-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK))
		}
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		return args
	}

	if prof.VideoMaxRateK > 0 {
		args = append(args,
			"-b:v", fmt.Sprintf("%dk", prof.VideoMaxRateK),
			"-maxrate", fmt.Sprintf("%dk", prof.VideoMaxRateK),
		)
		if prof.VideoBufSizeK > 0 {
			args = append(args, "-bufsize", fmt.Sprintf("%dk", prof.VideoBufSizeK))
		}
		return args
	}

	return append(args, "-cq", "23")
}

func appendConservativeHEVCVAAPIArgs(args []string, spec ports.StreamSpec, outputCodec string) []string {
	if !useConservativeHEVCVAAPILivePreset(spec, outputCodec) {
		return args
	}
	// iOS-native HEVC fMP4 attaches are sensitive to open-GOP recovery behavior.
	// Keep the VAAPI bitstream conservative so every forced segment boundary lands
	// on a full IDR, avoid B-frame reordering, and include AUD markers for
	// stricter Apple decoders.
	return append(args,
		"-bf", "0",
		"-aud", "1",
		"-idr_interval", "1",
		"-tier", "main",
	)
}

func useConservativeHEVCVAAPILivePreset(spec ports.StreamSpec, outputCodec string) bool {
	if spec.Mode != ports.ModeLive {
		return false
	}
	if normalizeRequestedCodec(outputCodec) != "hevc" {
		return false
	}
	if effectiveLiveRuntimeMode(spec.Profile) != ports.RuntimeModeHQ25 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4")
}

func (a *LocalAdapter) buildCopyVideoArgs(args []string, spec ports.StreamSpec, inputURL string) []string {
	hardenedBitstream := shouldHardenSafariCopyBitstream(spec, inputURL)

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("transcode.mode", "copy").
		Str("video.codec", "copy").
		Bool("bitstream_hardened", hardenedBitstream).
		Msg("pipeline video: copy")

	args = append(args, "-c:v", "copy")
	// DVB stream-relay sources (port 17999) deliver non-monotonic DTS.
	// -enc_time_base:v demux forces the muxer to derive timestamps from
	// the demuxer timebase (which igndts+genpts have already cleaned)
	// instead of the raw packet DTS, eliminating A/V desync in copy mode.
	if spec.Source.Type != ports.SourceFile {
		args = append(args, "-enc_time_base:v", "demux")
	}
	if hardenedBitstream {
		// Repeat H.264 extradata on keyframes so Safari can recover SPS/PPS
		// more reliably from dirty relay streams while staying in copy mode.
		args = append(args, "-bsf:v", "dump_extra=freq=keyframe")
	}
	return args
}

func (a *LocalAdapter) buildCPUVideoArgs(args []string, spec ports.StreamSpec, outputCodec string, gop, segmentSec int) []string {
	prof := spec.Profile
	legacy := usesLegacyCPUDefaults(spec, outputCodec)

	codec := "libx264"
	preset := "ultrafast"
	crf := 20
	deinterlace := true

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

	deinterlaceFilter := a.deinterlaceFilterForProfile(spec)
	if deinterlace {
		args = append(args, "-vf", deinterlaceFilter)
	}

	args = append(args, "-c:v", codec)
	args = append(args, "-preset", preset)
	tune := "zerolatency"
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") {
		tune = strings.TrimSpace(a.SafariDirtyX264Tune)
	}
	if tune != "" {
		args = append(args, "-tune", tune)
	}
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

func (a *LocalAdapter) deinterlaceFilterForProfile(spec ports.StreamSpec) string {
	deinterlaceFilter := "yadif"
	if spec.Mode == ports.ModeLive && spec.Format == ports.FormatHLS && effectiveLiveRuntimeMode(spec.Profile) == ports.RuntimeModeHQ25 {
		deinterlaceFilter = "bwdif=mode=send_frame:parity=auto:deint=all"
	} else if strings.EqualFold(strings.TrimSpace(spec.Profile.Name), "safari_dirty") && strings.TrimSpace(a.SafariDirtyFilter) != "" {
		deinterlaceFilter = strings.TrimSpace(a.SafariDirtyFilter)
	} else if spec.Mode == ports.ModeLive && spec.Format == ports.FormatHLS {
		// Generic live HLS transcodes should preserve sports motion on interlaced
		// broadcast sources instead of collapsing them to 25p.
		deinterlaceFilter = "bwdif=mode=send_field:parity=auto:deint=all"
	}
	return deinterlaceFilter
}

func (a *LocalAdapter) supportedHWCodecs() []string {
	return a.supportedHWCodecsLocal()
}

func (a *LocalAdapter) autoHWCodecs() []string {
	return a.autoHWCodecsLocal()
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

func codecToNVENCEncoder(codec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264":
		return "h264_nvenc", true
	case "hevc":
		return "hevc_nvenc", true
	case "av1":
		return "av1_nvenc", true
	default:
		return "", false
	}
}

func backendForHWAccel(hwaccel string) profiles.GPUBackend {
	switch strings.ToLower(strings.TrimSpace(hwaccel)) {
	case "vaapi", "vaapi_encode_only":
		return profiles.GPUBackendVAAPI
	case "nvenc":
		return profiles.GPUBackendNVENC
	default:
		return profiles.GPUBackendNone
	}
}

func normalizeRequestedCodec(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch c {
	case "", "h264", "avc", "avc1", "libx264", "h264_vaapi", "h264_nvenc":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi", "hevc_nvenc":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "av1_nvenc", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return c
	}
}

func fpsCacheKey(source ports.StreamSource, inputURL string) string {
	if source.Type == ports.SourceTuner {
		if ref := normalizeServiceRef(source.ID); ref != "" {
			return "service_ref:" + ref
		}
	}
	if ref := extractServiceRefFromURL(inputURL); ref != "" {
		return "service_ref:" + ref
	}
	return ""
}

func normalizeServiceRef(raw string) string {
	ref := strings.TrimSpace(raw)
	ref = strings.Trim(ref, "/")
	if ref == "" {
		return ""
	}
	if !isLikelyServiceRef(ref) {
		return ""
	}
	ref = strings.TrimRight(ref, ":")
	if isHexColonServiceRef(ref) {
		return strings.ToUpper(ref)
	}
	return ref
}

func extractServiceRefFromURL(inputURL string) string {
	u, err := url.Parse(strings.TrimSpace(inputURL))
	if err != nil {
		return ""
	}
	if ref := normalizeServiceRef(u.Query().Get("ref")); ref != "" {
		return ref
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	return normalizeServiceRef(path)
}

func isLikelyServiceRef(value string) bool {
	return strings.Count(value, ":") >= 5
}

func isHexColonServiceRef(ref string) bool {
	if ref == "" || !strings.Contains(ref, ":") {
		return false
	}
	for _, ch := range ref {
		switch {
		case ch == ':':
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func safariRuntimeServiceRef(spec ports.StreamSpec, inputURL string) string {
	if ref := normalizeServiceRef(spec.Source.ID); ref != "" {
		return ref
	}
	return extractServiceRefFromURL(inputURL)
}

func shouldForceSafariCopyForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	if spec.Profile.DisableSafariForceCopy {
		return false
	}
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}

	return serviceRefEnvContains("XG2G_SAFARI_FORCE_COPY_SERVICE_REFS", targetRef)
}

func shouldForceSafariHQForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ_SERVICE_REFS", targetRef)
}

func shouldForceAnySafariHQForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	return shouldForceSafariHQForServiceRef(spec, inputURL) ||
		shouldForceSafariHQ25ForServiceRef(spec, inputURL) ||
		shouldForceSafariHQ50ForServiceRef(spec, inputURL)
}

func shouldForceSafariHQ25ForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ25_SERVICE_REFS", targetRef)
}

func shouldForceSafariHQ50ForServiceRef(spec ports.StreamSpec, inputURL string) bool {
	targetRef := safariRuntimeServiceRef(spec, inputURL)
	if targetRef == "" {
		return false
	}
	return serviceRefEnvContains("XG2G_SAFARI_HQ50_SERVICE_REFS", targetRef)
}

func safariHQRuntimeMode(profile ports.ProfileSpec) ports.RuntimeMode {
	if shouldForce25FPSForSafariHQ(profile) {
		return ports.RuntimeModeHQ25
	}
	return ports.RuntimeModeHQ50
}

func shouldUseProgressiveSafariHQ(profile ports.ProfileSpec) bool {
	hint := profile.PolicyModeHint
	if hint == "" || hint == ports.RuntimeModeUnknown {
		hint = profiles.RuntimeModeHintFromProfile(profile)
	}
	return hint == ports.RuntimeModeCopy || hint == ports.RuntimeModeCopyHardened
}

func shouldForce25FPSForSafariHQ(profile ports.ProfileSpec) bool {
	if profile.ForceSafariHQ25 {
		return true
	}
	if profile.EffectiveRuntimeMode == ports.RuntimeModeHQ50 || profile.PolicyModeHint == ports.RuntimeModeHQ50 {
		return false
	}
	return !shouldUseProgressiveSafariHQ(profile)
}

func shouldHardenSafariCopyBitstream(spec ports.StreamSpec, inputURL string) bool {
	return shouldForceSafariCopyForServiceRef(spec, inputURL)
}

func serviceRefEnvContains(envKey, targetRef string) bool {
	raw := strings.TrimSpace(config.ParseString(envKey, ""))
	if raw == "" || targetRef == "" {
		return false
	}
	for _, candidate := range strings.Split(raw, ",") {
		if normalizeServiceRef(candidate) == targetRef {
			return true
		}
	}
	return false
}
