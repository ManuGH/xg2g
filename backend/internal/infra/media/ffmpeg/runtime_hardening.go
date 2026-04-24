package ffmpeg

import (
	"context"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type runtimeHardeningPlanID string

const (
	runtimeHardeningPlanUnknown                   runtimeHardeningPlanID = ""
	runtimeHardeningPlanSafariRuntimeRemuxProbe   runtimeHardeningPlanID = "safari_runtime_remux_probe"
	runtimeHardeningPlanSafariRuntimeRemuxAllow   runtimeHardeningPlanID = "safari_runtime_remux_allowlist"
	runtimeHardeningPlanSafariRuntimeHQAllow      runtimeHardeningPlanID = "safari_runtime_hq_allowlist"
	runtimeHardeningPlanProgressiveAV1Deinterlace runtimeHardeningPlanID = "progressive_av1_deinterlace_clear"
	runtimeHardeningPlanSafariCopyBitstreamHarden runtimeHardeningPlanID = "safari_copy_bitstream_harden"
)

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
	if !shouldForceSafariHQForServiceRef(spec, inputURL) {
		return noRuntimeHardeningDecision()
	}

	forceHQ25 := shouldForceSafariHQ25ForServiceRef(spec, inputURL)
	a.Logger.Warn().
		Str("sessionId", spec.SessionID).
		Str("service_ref", safariRuntimeServiceRef(spec, inputURL)).
		Msg("forcing safari HQ transcode path via service-ref allowlist")

	profile := buildSafariRuntimeHQProfile(spec.Profile, forceHQ25)
	return runtimeHardeningDecision{
		id:            runtimeHardeningPlanSafariRuntimeHQAllow,
		source:        ports.RuntimeModeSourceEnvOverride,
		effectiveMode: safariHQRuntimeMode(profile),
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

func buildSafariRuntimeHQProfile(current ports.ProfileSpec, forceHQ25 bool) ports.ProfileSpec {
	progressiveHQ := shouldUseProgressiveSafariHQ(current)

	next := current
	if forceHQ25 {
		next.ForceSafariHQ25 = true
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

func buildProgressiveHardwareDeinterlaceProfile(current ports.ProfileSpec) ports.ProfileSpec {
	next := current
	next.Deinterlace = false
	return next
}
