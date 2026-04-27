package ffmpeg

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type runtimeHardeningPlanID string

const (
	runtimeHardeningPlanUnknown                   runtimeHardeningPlanID = ""
	runtimeHardeningPlanSafariRuntimeRemuxProbe   runtimeHardeningPlanID = "safari_runtime_remux_probe"
	runtimeHardeningPlanSafariRuntimeRemuxAllow   runtimeHardeningPlanID = "safari_runtime_remux_allowlist"
	runtimeHardeningPlanSafariRuntimeHQAllow      runtimeHardeningPlanID = "safari_runtime_hq_allowlist"
	runtimeHardeningPlanRuntimeHQ50Override       runtimeHardeningPlanID = "runtime_hq50_service_ref_override"
	runtimeHardeningPlanAdaptiveTranscodeQuality  runtimeHardeningPlanID = "adaptive_transcode_quality"
	runtimeHardeningPlanProgressiveAV1Deinterlace runtimeHardeningPlanID = "progressive_av1_deinterlace_clear"
	runtimeHardeningPlanSafariCopyBitstreamHarden runtimeHardeningPlanID = "safari_copy_bitstream_harden"
)

type adaptiveTranscodeQualityBudget struct {
	codec    string
	maxRateK int
	bufSizeK int
}

type runtimeHardeningDecision struct {
	id            runtimeHardeningPlanID
	source        ports.RuntimeModeSource
	effectiveMode ports.RuntimeMode
	profile       ports.ProfileSpec
	applied       bool
}

func (a *LocalAdapter) FinalizePlan(ctx context.Context, spec ports.StreamSpec, inputURL string) ports.StreamSpec {
	spec = seedRuntimeMode(spec)
	spec = a.applyRuntimeHardeningPlan(ctx, spec, inputURL)
	if spec.Profile.EffectiveRuntimeMode == "" || spec.Profile.EffectiveRuntimeMode == ports.RuntimeModeUnknown {
		spec.Profile.EffectiveRuntimeMode = profiles.RuntimeModeHintFromProfile(spec.Profile)
	}
	return spec
}

func seedRuntimeMode(spec ports.StreamSpec) ports.StreamSpec {
	spec.Profile.EffectiveRuntimeMode = profiles.RuntimeModeHintFromProfile(spec.Profile)
	if spec.Profile.EffectiveModeSource == "" || spec.Profile.EffectiveModeSource == ports.RuntimeModeSourceUnknown {
		spec.Profile.EffectiveModeSource = ports.RuntimeModeSourceResolve
	}
	return spec
}

func (a *LocalAdapter) applyRuntimeHardeningPlan(ctx context.Context, spec ports.StreamSpec, inputURL string) ports.StreamSpec {
	evaluators := []func(context.Context, ports.StreamSpec, string) runtimeHardeningDecision{
		a.evaluateSafariRuntimeRemuxHardening,
		a.evaluateSafariRuntimeHQHardening,
		a.evaluateAdaptiveTranscodeQualityHardening,
		a.evaluateProgressiveHardwareDeinterlaceHardening,
		a.evaluateSafariCopyBitstreamHardening,
	}

	for _, evaluate := range evaluators {
		decision := evaluate(ctx, spec, inputURL)
		if !decision.applied {
			continue
		}
		spec = a.applyRuntimeHardeningDecision(spec, decision)
	}

	return spec
}

func (a *LocalAdapter) applyRuntimeHardeningDecision(spec ports.StreamSpec, decision runtimeHardeningDecision) ports.StreamSpec {
	spec.Profile = decision.profile
	if decision.effectiveMode != "" && decision.effectiveMode != ports.RuntimeModeUnknown {
		spec.Profile.EffectiveRuntimeMode = decision.effectiveMode
	}
	if decision.source != "" && decision.source != ports.RuntimeModeSourceUnknown {
		spec.Profile.EffectiveModeSource = decision.source
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("runtime_hardening_plan", string(decision.id)).
		Str("effective_runtime_mode", string(spec.Profile.EffectiveRuntimeMode)).
		Str("effective_mode_source", string(spec.Profile.EffectiveModeSource)).
		Msg("applied runtime hardening plan")

	return spec
}

func noRuntimeHardeningDecision() runtimeHardeningDecision {
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanUnknown,
		source:        ports.RuntimeModeSourceUnknown,
		effectiveMode: ports.RuntimeModeUnknown,
	}
}

func (a *LocalAdapter) evaluateSafariRuntimeRemuxHardening(ctx context.Context, spec ports.StreamSpec, inputURL string) runtimeHardeningDecision {
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Name), profiles.ProfileSafari) {
		return noRuntimeHardeningDecision()
	}
	if !spec.Profile.TranscodeVideo {
		return noRuntimeHardeningDecision()
	}
	if strings.TrimSpace(inputURL) == "" {
		return noRuntimeHardeningDecision()
	}
	if shouldForceAnySafariHQForServiceRef(spec, inputURL) {
		return noRuntimeHardeningDecision()
	}
	if shouldForceSafariCopyForServiceRef(spec, inputURL) {
		a.Logger.Warn().
			Str("sessionId", spec.SessionID).
			Str("service_ref", safariRuntimeServiceRef(spec, inputURL)).
			Msg("forcing safari remux path via service-ref allowlist")
		return runtimeHardeningDecision{
			id:            runtimeHardeningPlanSafariRuntimeRemuxAllow,
			source:        ports.RuntimeModeSourceEnvOverride,
			effectiveMode: ports.RuntimeModeCopy,
			profile:       buildSafariRuntimeRemuxProfile(spec.Profile),
			applied:       true,
		}
	}

	probeTimeout := a.SafariRuntimeProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 6 * time.Second
	}
	info, err := a.runSafariRuntimeProbeWithRetry(ctx, spec.SessionID, inputURL, probeTimeout)
	if err != nil {
		a.Logger.Info().
			Err(err).
			Str("sessionId", spec.SessionID).
			Str("input_url", sanitizeURLForLog(inputURL)).
			Dur("probe_timeout", probeTimeout).
			Msg("safari runtime probe failed; keeping transcode path")
		return noRuntimeHardeningDecision()
	}
	if !strings.EqualFold(strings.TrimSpace(info.Video.CodecName), "h264") || info.Video.Interlaced {
		return noRuntimeHardeningDecision()
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("video_codec", info.Video.CodecName).
		Bool("interlaced", info.Video.Interlaced).
		Str("container", info.Container).
		Msg("safari runtime probe selected remux path")
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanSafariRuntimeRemuxProbe,
		source:        ports.RuntimeModeSourceRuntimeHardening,
		effectiveMode: ports.RuntimeModeCopy,
		profile:       buildSafariRuntimeRemuxProfile(spec.Profile),
		applied:       true,
	}
}

func (a *LocalAdapter) evaluateSafariRuntimeHQHardening(_ context.Context, spec ports.StreamSpec, inputURL string) runtimeHardeningDecision {
	if !shouldForceAnySafariHQForServiceRef(spec, inputURL) {
		return noRuntimeHardeningDecision()
	}

	forceHQ25 := shouldForceSafariHQ25ForServiceRef(spec, inputURL)
	forceHQ50 := shouldForceSafariHQ50ForServiceRef(spec, inputURL)
	forceSafariHQProfile := shouldForceSafariHQForServiceRef(spec, inputURL)
	if forceHQ50 && !forceHQ25 && !forceSafariHQProfile && spec.Profile.TranscodeVideo {
		a.Logger.Warn().
			Str("sessionId", spec.SessionID).
			Str("service_ref", safariRuntimeServiceRef(spec, inputURL)).
			Msg("forcing HQ50 runtime mode via service-ref allowlist")
		profile := spec.Profile
		profile.PolicyModeHint = ports.RuntimeModeHQ50
		profile.ForceSafariHQ25 = false
		profile = applySafariHQ50BitrateBudget(profile)
		return runtimeHardeningDecision{
			id:            runtimeHardeningPlanRuntimeHQ50Override,
			source:        ports.RuntimeModeSourceEnvOverride,
			effectiveMode: ports.RuntimeModeHQ50,
			profile:       profile,
			applied:       true,
		}
	}
	a.Logger.Warn().
		Str("sessionId", spec.SessionID).
		Str("service_ref", safariRuntimeServiceRef(spec, inputURL)).
		Msg("forcing safari HQ transcode path via service-ref allowlist")

	profile := buildSafariRuntimeHQProfile(spec.Profile, forceHQ25, forceHQ50)
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanSafariRuntimeHQAllow,
		source:        ports.RuntimeModeSourceEnvOverride,
		effectiveMode: safariHQRuntimeMode(profile),
		profile:       profile,
		applied:       true,
	}
}

func (a *LocalAdapter) evaluateAdaptiveTranscodeQualityHardening(_ context.Context, spec ports.StreamSpec, inputURL string) runtimeHardeningDecision {
	budget, ok := adaptiveTranscodeQualityBudgetFor(spec.Profile)
	if !ok || !shouldPromoteAdaptiveTranscodeQuality(spec, inputURL, budget) {
		return noRuntimeHardeningDecision()
	}

	profile := applyAdaptiveTranscodeQualityBudget(spec.Profile, budget)
	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("service_ref", safariRuntimeServiceRef(spec, inputURL)).
		Str("video_codec", budget.codec).
		Int("video_maxrate_k", profile.VideoMaxRateK).
		Int("video_bufsize_k", profile.VideoBufSizeK).
		Msg("adaptive transcode quality selected hq50 bitrate budget")
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanAdaptiveTranscodeQuality,
		source:        ports.RuntimeModeSourceRuntimeHardening,
		effectiveMode: ports.RuntimeModeHQ50,
		profile:       profile,
		applied:       true,
	}
}

func (a *LocalAdapter) evaluateProgressiveHardwareDeinterlaceHardening(ctx context.Context, spec ports.StreamSpec, inputURL string) runtimeHardeningDecision {
	if spec.Mode != ports.ModeLive {
		return noRuntimeHardeningDecision()
	}
	if !spec.Profile.TranscodeVideo || !spec.Profile.Deinterlace {
		return noRuntimeHardeningDecision()
	}
	if strings.TrimSpace(inputURL) == "" {
		return noRuntimeHardeningDecision()
	}
	if normalizeRequestedCodec(spec.Profile.VideoCodec) != "av1" {
		return noRuntimeHardeningDecision()
	}

	probeTimeout := a.SafariRuntimeProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 6 * time.Second
	}
	attemptCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	info, err := a.runSafariRuntimeProbeOnce(attemptCtx, inputURL)
	cancel()
	if err != nil {
		a.Logger.Info().
			Err(err).
			Str("sessionId", spec.SessionID).
			Str("input_url", sanitizeURLForLog(inputURL)).
			Dur("probe_timeout", probeTimeout).
			Msg("runtime progressive probe failed; keeping deinterlace enabled without retry")
		return noRuntimeHardeningDecision()
	}
	if info.Video.Interlaced {
		return noRuntimeHardeningDecision()
	}

	a.Logger.Info().
		Str("sessionId", spec.SessionID).
		Str("video_codec", info.Video.CodecName).
		Bool("interlaced", info.Video.Interlaced).
		Str("container", info.Container).
		Msg("runtime probe cleared unnecessary deinterlace for hardware av1 path")
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanProgressiveAV1Deinterlace,
		source:        ports.RuntimeModeSourceRuntimeHardening,
		effectiveMode: ports.RuntimeModeUnknown,
		profile:       buildProgressiveHardwareDeinterlaceProfile(spec.Profile),
		applied:       true,
	}
}

func (a *LocalAdapter) evaluateSafariCopyBitstreamHardening(_ context.Context, spec ports.StreamSpec, inputURL string) runtimeHardeningDecision {
	if spec.Profile.TranscodeVideo || !shouldHardenSafariCopyBitstream(spec, inputURL) {
		return noRuntimeHardeningDecision()
	}
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanSafariCopyBitstreamHarden,
		source:        ports.RuntimeModeSourceEnvOverride,
		effectiveMode: ports.RuntimeModeCopyHardened,
		profile:       spec.Profile,
		applied:       true,
	}
}

func buildSafariRuntimeRemuxProfile(current ports.ProfileSpec) ports.ProfileSpec {
	next := current
	next.TranscodeVideo = false
	next.Deinterlace = false
	next.Container = "mpegts"
	if next.AudioBitrateK <= 0 {
		next.AudioBitrateK = 192
	}
	return next
}

func buildSafariRuntimeHQProfile(current ports.ProfileSpec, forceHQ25 bool, forceHQ50 bool) ports.ProfileSpec {
	progressiveHQ := shouldUseProgressiveSafariHQ(current)

	next := current
	if forceHQ50 {
		next.ForceSafariHQ25 = false
		next.PolicyModeHint = ports.RuntimeModeHQ50
	}
	if forceHQ25 {
		next.ForceSafariHQ25 = true
		next.PolicyModeHint = ports.RuntimeModeHQ25
	}
	next.Name = profiles.ProfileSafariRuntimeHQ
	next.TranscodeVideo = true
	next.Deinterlace = !progressiveHQ
	next.Container = "mpegts"
	next.HWAccel = ""
	next.VideoCodec = "libx264"
	next.VideoCRF = 16
	next.VideoMaxRateK = 12000
	next.VideoBufSizeK = 24000
	next.AudioBitrateK = 256
	next.Preset = "fast"
	if progressiveHQ {
		next.Preset = "veryfast"
	}
	return next
}

func applySafariHQ50BitrateBudget(current ports.ProfileSpec) ports.ProfileSpec {
	next := current
	defaultMaxRateK := maxInt(next.VideoMaxRateK, 12000)
	next.VideoMaxRateK = envIntBounded("XG2G_SAFARI_HQ50_MAXRATE_K", defaultMaxRateK, 4000, 60000)
	defaultBufSizeK := maxInt(next.VideoBufSizeK, next.VideoMaxRateK*2)
	next.VideoBufSizeK = envIntBounded("XG2G_SAFARI_HQ50_BUFSIZE_K", defaultBufSizeK, 8000, 120000)
	return next
}

func applyAdaptiveTranscodeQualityBudget(current ports.ProfileSpec, budget adaptiveTranscodeQualityBudget) ports.ProfileSpec {
	next := current
	next.PolicyModeHint = ports.RuntimeModeHQ50
	next.ForceSafariHQ25 = false
	next.VideoMaxRateK = budget.maxRateK
	next.VideoBufSizeK = budget.bufSizeK
	return next
}

func shouldPromoteAdaptiveTranscodeQuality(spec ports.StreamSpec, inputURL string, budget adaptiveTranscodeQualityBudget) bool {
	if !config.ParseBool("XG2G_ADAPTIVE_QUALITY_ENABLED", true) {
		return false
	}
	if spec.Mode != ports.ModeLive || !spec.Profile.TranscodeVideo {
		return false
	}
	if spec.Quality != ports.QualityHigh {
		return false
	}
	if spec.Profile.EffectiveModeSource == ports.RuntimeModeSourceEnvOverride {
		return false
	}
	if spec.Profile.ForceSafariHQ25 || spec.Profile.EffectiveRuntimeMode == ports.RuntimeModeHQ50 {
		return false
	}
	if shouldForceSafariHQ25ForServiceRef(spec, inputURL) {
		return false
	}
	if !adaptiveTranscodeQualityContainerAllowed(budget.codec, spec.Profile.Container) {
		return false
	}
	if spec.Profile.VideoMaxRateK > 0 && spec.Profile.VideoMaxRateK >= budget.maxRateK {
		return false
	}
	return true
}

func adaptiveTranscodeQualityBudgetFor(profile ports.ProfileSpec) (adaptiveTranscodeQualityBudget, bool) {
	switch normalizeRequestedCodec(profile.VideoCodec) {
	case "av1":
		if !config.ParseBool("XG2G_ADAPTIVE_AV1_QUALITY_ENABLED", true) {
			return adaptiveTranscodeQualityBudget{}, false
		}
		return adaptiveCodecQualityBudget("av1", 14000, 28000), true
	case "hevc":
		if !config.ParseBool("XG2G_ADAPTIVE_HEVC_QUALITY_ENABLED", true) {
			return adaptiveTranscodeQualityBudget{}, false
		}
		return adaptiveCodecQualityBudget("hevc", 14000, 28000), true
	case "h264":
		if !config.ParseBool("XG2G_ADAPTIVE_H264_QUALITY_ENABLED", true) {
			return adaptiveTranscodeQualityBudget{}, false
		}
		return adaptiveCodecQualityBudget("h264", 16000, 32000), true
	default:
		return adaptiveTranscodeQualityBudget{}, false
	}
}

func adaptiveCodecQualityBudget(codec string, defaultMaxRateK int, defaultBufSizeK int) adaptiveTranscodeQualityBudget {
	codecEnv := strings.ToUpper(codec)
	maxRateK := envIntBounded("XG2G_ADAPTIVE_"+codecEnv+"_MAXRATE_K", defaultMaxRateK, 4000, 60000)
	defaultBufSizeK = maxInt(defaultBufSizeK, defaultMaxRateK*2)
	if maxRateK != defaultMaxRateK {
		defaultBufSizeK = maxRateK * 2
	}
	bufSizeK := envIntBounded("XG2G_ADAPTIVE_"+codecEnv+"_BUFSIZE_K", defaultBufSizeK, 8000, 120000)
	return adaptiveTranscodeQualityBudget{
		codec:    codec,
		maxRateK: maxRateK,
		bufSizeK: bufSizeK,
	}
}

func adaptiveTranscodeQualityContainerAllowed(codec string, container string) bool {
	container = strings.ToLower(strings.TrimSpace(container))
	switch codec {
	case "av1", "hevc":
		return container == "fmp4"
	case "h264":
		return container == "" || container == "mpegts" || container == "fmp4"
	default:
		return false
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildProgressiveHardwareDeinterlaceProfile(current ports.ProfileSpec) ports.ProfileSpec {
	next := current
	next.Deinterlace = false
	return next
}
