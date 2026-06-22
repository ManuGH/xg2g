package intents

import (
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/http/v3/clientpolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"strings"
)

func (s *Service) lookupStartCapability(serviceRef string) *scan.Capability {
	if scanner := s.deps.ChannelScanner(); scanner != nil {
		if capability, found := scanner.GetCapability(serviceRef); found {
			return &capability
		}
	}
	return nil
}

func startSourceProfileFromCapability(capability *scan.Capability) *playbackprofile.SourceProfile {
	if capability == nil {
		return nil
	}
	normalized := capability.Normalized()
	if !normalized.Usable() {
		return nil
	}

	source := playbackprofile.SourceProfile{
		Container:        normalized.Container,
		VideoCodec:       normalized.VideoCodec,
		AudioCodec:       normalized.AudioCodec,
		BitrateKbps:      normalized.StableBitrateKbps(),
		Width:            normalized.Width,
		Height:           normalized.Height,
		FPS:              normalized.FPS,
		Interlaced:       normalized.Interlaced,
		AudioChannels:    normalized.AudioChannels,
		AudioBitrateKbps: normalized.AudioBitrateKbps,
	}
	if source.Container == "" &&
		source.VideoCodec == "" &&
		source.AudioCodec == "" &&
		source.BitrateKbps == 0 &&
		source.Width == 0 &&
		source.Height == 0 &&
		source.FPS == 0 &&
		!source.Interlaced &&
		source.AudioChannels == 0 &&
		source.AudioBitrateKbps == 0 {
		return nil
	}
	return &source
}

func detectStartHardwareState() startHardwareState {
	defaultBackend := hardware.PreferredGPUBackend()
	return startHardwareState{
		hasGPU:      defaultBackend != profiles.GPUBackendNone,
		defaultGPU:  defaultBackend,
		av1Backend:  hardware.PreferredGPUBackendForCodec("av1"),
		hevcBackend: hardware.PreferredGPUBackendForCodec("hevc"),
		h264Backend: hardware.PreferredGPUBackendForCodec("h264"),
	}
}

func (s *Service) resolveStartHWAccelMode(intent Intent, hw startHardwareState) (profiles.HWAccelMode, *Error) {
	hwaccelMode := profiles.HWAccelAuto
	if hwaccel := normalize.Token(intent.Params["hwaccel"]); hwaccel != "" {
		switch hwaccel {
		case "force":
			hwaccelMode = profiles.HWAccelForce
		case "off":
			hwaccelMode = profiles.HWAccelOff
		case "auto":
			hwaccelMode = profiles.HWAccelAuto
		default:
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "invalid_hwaccel")
			return "", &Error{
				Kind:    ErrorInvalidInput,
				Message: fmt.Sprintf("invalid hwaccel value: %q (must be auto, force, or off)", hwaccel),
			}
		}
	}

	if hwaccelMode == profiles.HWAccelForce && !hw.hasGPU {
		reason := "no hardware encoder device exposed"
		switch {
		case hardware.HasVAAPI() && hardware.HasNVENC():
			reason = "VAAPI/NVENC preflight encode tests failed"
		case hardware.HasVAAPI():
			reason = "VAAPI preflight encode test failed"
		case hardware.HasNVENC():
			reason = "NVENC preflight encode test failed"
		}
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "hwaccel_unavailable")
		return "", &Error{
			Kind:    ErrorInvalidInput,
			Message: fmt.Sprintf("hwaccel=force requested but GPU not available (%s)", reason),
		}
	}

	return hwaccelMode, nil
}

func (s *Service) resolveRequestedStartProfile(ctx context.Context, intent Intent, hwaccelMode profiles.HWAccelMode, capability *scan.Capability) (string, string, *Error) {
	requestedPlaybackMode := normalize.Token(intent.Params["playback_mode"])
	if requestedPlaybackMode != "" {
		_, keyLabel, resultLabel, tokenErr := resolvePlaybackDecisionToken(intent.PlaybackDecisionToken, intent.Params)
		if tokenErr != nil {
			s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
			return "", "", &Error{Kind: ErrorInvalidInput, Message: tokenErr.Error()}
		}
		s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
	}
	if profileID := normalize.Token(intent.Params["profile"]); profileID != "" {
		return profileID, "", nil
	}

	profileID, err := resolveRequestedStartProfilePolicy(startProfilePolicyInput{
		RequestedPlaybackMode: requestedPlaybackMode,
		ClientFamily:          clientFamilyForIntent(intent),
		RequestedCodecs:       requestedCodecsForIntent(intent, requestedPlaybackMode),
		ClientCaps:            intent.ClientCaps,
		Capability:            capability,
		HWAccelMode:           hwaccelMode,
		HostRuntime:           s.deps.HostRuntime(ctx),
	})
	if err != nil {
		return "", "", &Error{Kind: ErrorInvalidInput, Message: err.Error()}
	}
	profileID = s.clampRequestedStartProfileFromPlaybackPolicy(ctx, intent, requestedPlaybackMode, profileID)
	return profileID, requestedPlaybackMode, nil
}

func (s *Service) resolveStartProfile(ctx context.Context, intent Intent, capability *scan.Capability, hw startHardwareState, hwaccelMode profiles.HWAccelMode, reqProfileID, requestedPlaybackMode string) (startProfileResolution, *Error) {
	resolution := startProfileResolution{
		requestedPlaybackMode: requestedPlaybackMode,
		publicRequestProfile:  profiles.PublicProfileName(reqProfileID),
		sourceProfile:         startSourceProfileFromCapability(capability),
	}
	operatorCfg := s.deps.PlaybackOperator()
	resolution.effectiveProfileID, resolution.operatorSnapshot = profiles.ResolveRequestedProfileWithSourceOperatorOverride(reqProfileID, string(intent.Mode), intent.ServiceRef, operatorCfg)
	resolution.effectiveProfileID = profiles.NormalizeRequestedProfileID(resolution.effectiveProfileID)
	resolution.hostPressureBand = playbackprofile.NormalizeHostPressureBand(string(s.deps.HostPressure(ctx).EffectiveBand))
	resolution.bucket = "0"
	if intent.StartMs != nil && *intent.StartMs > 0 {
		resolution.bucket = fmt.Sprintf("%d", *intent.StartMs/1000)
	}

	clientFamily := clientFamilyForIntent(intent)
	profileUserAgent := clientpolicy.ResolveProfileUserAgent(requestedPlaybackMode, clientFamily, intent.UserAgent)
	resolveProfileSpec := func(profileID string) model.ProfileSpec {
		return s.resolveProfileSpec(profileID, profileUserAgent, capability, hw, hwaccelMode)
	}
	resolution.profileSpec = resolveProfileSpec(resolution.effectiveProfileID)
	if cappedSpec, changed := profiles.ApplyMaxQualityRung(resolution.profileSpec, resolution.operatorSnapshot.MaxQualityRung); changed {
		resolution.profileSpec = cappedSpec
		resolution.operatorSnapshot.OverrideApplied = true
	}
	operatorActive := resolution.operatorSnapshot.ForcedIntent != playbackprofile.IntentUnknown || resolution.operatorSnapshot.MaxQualityRung != playbackprofile.RungUnknown
	if !operatorActive {
		if downgradedProfileID, changed := profiles.ApplyHostPressureOverride(resolution.effectiveProfileID, resolution.hostPressureBand); changed {
			resolution.effectiveProfileID = downgradedProfileID
			resolution.profileSpec = resolveProfileSpec(resolution.effectiveProfileID)
			resolution.hostOverrideApplied = true
		}
	}
	startSourceVideoCodec := ""
	if capability != nil {
		startSourceVideoCodec = capability.VideoCodec
	}
	resolution.profileSpec = clientpolicy.ApplyStartPackagingPolicy(clientFamily, resolution.effectiveProfileID, resolution.profileSpec, startSourceVideoCodec, preferredEngineForIntent(intent))
	resolution.effectiveProfileID, resolution.profileSpec = autocodec.ApplyClientCompatibilityPolicy(
		clientFamily,
		resolution.effectiveProfileID,
		resolution.profileSpec,
		resolveProfileSpec,
	)
	requestedCodecs := requestedCodecsForIntent(intent, requestedPlaybackMode)
	if shouldTraceAutoCodecDecision(intent, requestedCodecs) {
		resolution.autoCodecTrace = autocodec.DescribeSelection(requestedCodecs, resolution.effectiveProfileID, s.deps.HostRuntime(ctx))
	}
	resolution.idempotencyKey = ComputeIdemKey(model.IntentTypeStreamStart, intent.ServiceRef, resolution.effectiveProfileID, resolution.bucket)
	if requiredCodec, ok := requiredVerifiedHardwareCodecForProfile(resolution.effectiveProfileID); ok {
		if hwaccelMode == profiles.HWAccelOff {
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "hw_profile_conflict")
			return startProfileResolution{}, &Error{
				Kind:    ErrorInvalidInput,
				Message: fmt.Sprintf("profile %q requires verified hardware acceleration; hwaccel=off is incompatible", resolution.effectiveProfileID),
			}
		}
		if !hardware.IsHardwareEncoderReady(requiredCodec) {
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "hw_profile_unavailable")
			return startProfileResolution{}, &Error{
				Kind:    ErrorInvalidInput,
				Message: fmt.Sprintf("profile %q requires verified %s hardware encoding on this host", resolution.effectiveProfileID, requiredCodec),
			}
		}
	}
	resolution.resolvedIntent = profiles.PublicProfileName(resolution.profileSpec.Name)
	if resolution.publicRequestProfile != "" && resolution.resolvedIntent != "" && resolution.publicRequestProfile != resolution.resolvedIntent {
		resolution.degradedFrom = resolution.publicRequestProfile
	}
	return resolution, nil
}

func (s *Service) resolveProfileSpec(profileID, userAgent string, capability *scan.Capability, hw startHardwareState, hwaccelMode profiles.HWAccelMode) model.ProfileSpec {
	resolveBackend := hw.defaultGPU
	switch profileID {
	case profiles.ProfileAV1HW:
		resolveBackend = hw.av1Backend
	case profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		resolveBackend = hw.hevcBackend
	case profiles.ProfileH264FMP4:
		resolveBackend = hw.h264Backend
	}
	return profiles.Resolve(profileID, userAgent, int(s.deps.DVRWindow().Seconds()), capability, resolveBackend, hwaccelMode)
}

func deriveStartHWAccelSummary(profileSpec model.ProfileSpec, hwaccelMode profiles.HWAccelMode, hasGPU bool) (effective, reason, backend string) {
	if profileSpec.TranscodeVideo {
		if profiles.IsGPUBackedProfile(profileSpec.HWAccel) {
			effective = "gpu"
			backend = string(hardware.BackendForHWAccel(profileSpec.HWAccel))
			if backend == "" {
				backend = "gpu"
			}
			if hwaccelMode == profiles.HWAccelForce {
				reason = "forced"
			} else {
				reason = "auto_has_gpu"
			}
			return effective, reason, backend
		}
		effective = "cpu"
		backend = profileSpec.VideoCodec
		if hwaccelMode == profiles.HWAccelOff {
			reason = "user_disabled"
		} else if !hasGPU {
			reason = "no_gpu_available"
		} else {
			reason = "profile_cpu_only"
		}
		return effective, reason, backend
	}
	return "off", "passthrough", "none"
}

func (s *Service) logStartProfileResolution(intent Intent, resolution startProfileResolution, hw startHardwareState, hwaccelMode profiles.HWAccelMode, hwaccelEffective, hwaccelReason, encoderBackend string) {
	clientFamily := clientFamilyForIntent(intent)
	preferredEngine := preferredEngineForIntent(intent)
	deviceType := deviceTypeForIntent(intent)
	capHash := clientCapHashForIntent(intent)
	canonicalCaps := normalizedClientCaps(intent.ClientCaps)

	event := intent.Logger.Info().
		Str("ua", intent.UserAgent).
		Str("profile", resolution.profileSpec.Name).
		Str("profile_public", resolution.publicRequestProfile).
		Str("profile_effective", resolution.effectiveProfileID).
		Str("requested_playback_mode", resolution.requestedPlaybackMode).
		Int("dvr_window_sec", resolution.profileSpec.DVRWindowSec).
		Str("idem_key", resolution.idempotencyKey).
		Bool("gpu_available", hw.hasGPU).
		Str("hwaccel_requested", string(hwaccelMode)).
		Str("hwaccel_effective", hwaccelEffective).
		Str("hwaccel_reason", hwaccelReason).
		Str("operator_force_intent", playbackprofile.PublicIntentName(resolution.operatorSnapshot.ForcedIntent)).
		Str("operator_max_quality_rung", string(resolution.operatorSnapshot.MaxQualityRung)).
		Bool("operator_disable_client_fallback", resolution.operatorSnapshot.DisableClientFallback).
		Bool("operator_override_applied", resolution.operatorSnapshot.OverrideApplied).
		Str("host_pressure_band", string(resolution.hostPressureBand)).
		Bool("host_override_applied", resolution.hostOverrideApplied).
		Str("auto_codec_policy", resolution.autoCodecTrace.Policy).
		Str("auto_codec_requested", resolution.autoCodecTrace.RequestedCodecs).
		Str("auto_codec_selected", resolution.autoCodecTrace.SelectedCodec).
		Str("auto_codec_host_class", resolution.autoCodecTrace.PerformanceClass).
		Str("auto_codec_benchmark_class", resolution.autoCodecTrace.CodecBenchmarkClass).
		Str("encoder_backend", encoderBackend).
		Str("video_codec", resolution.profileSpec.VideoCodec).
		Str("container", resolution.profileSpec.Container).
		Bool("llhls", resolution.profileSpec.LLHLS)
	if clientFamily != "" {
		event = event.Str("client_family", clientFamily)
	}
	if preferredEngine != "" {
		event = event.Str("preferred_hls_engine", preferredEngine)
	}
	if deviceType != "" {
		event = event.Str("device_type", deviceType)
	}
	if capHash != "" {
		event = event.Str("cap_hash", capHash)
	}
	if canonicalCaps != nil {
		if source := normalize.Token(canonicalCaps.ClientCapsSource); source != "" {
			event = event.Str("client_caps_source", source)
		}
		if len(canonicalCaps.Containers) > 0 {
			event = event.Str("client_containers", strings.Join(canonicalCaps.Containers, ","))
		}
		if len(canonicalCaps.VideoCodecs) > 0 {
			event = event.Str("client_video_codecs", strings.Join(canonicalCaps.VideoCodecs, ","))
		}
		if len(canonicalCaps.AudioCodecs) > 0 {
			event = event.Str("client_audio_codecs", strings.Join(canonicalCaps.AudioCodecs, ","))
		}
		if canonicalCaps.RuntimeProbeUsed {
			event = event.Bool("client_runtime_probe_used", true)
		}
		if canonicalCaps.RuntimeProbeVersion > 0 {
			event = event.Int("client_runtime_probe_version", canonicalCaps.RuntimeProbeVersion)
		}
	}
	event.Msg("intent profile resolved")
}

func requiredVerifiedHardwareCodecForProfile(profileID string) (string, bool) {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		return "hevc", true
	case profiles.ProfileAV1HW:
		return "av1", true
	default:
		return "", false
	}
}
