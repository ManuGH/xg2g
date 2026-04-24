package intents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/http/v3/autocodec"
	"github.com/ManuGH/xg2g/internal/control/http/v3/clientpolicy"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

const admissionLeaseTTL = 30 * time.Second
const startReplayRecoveryAttempts = 3

// Service handles intent processing independent of HTTP transport.
type Service struct {
	deps Deps
}

type startHardwareState struct {
	hasGPU      bool
	defaultGPU  profiles.GPUBackend
	av1Backend  profiles.GPUBackend
	hevcBackend profiles.GPUBackend
	h264Backend profiles.GPUBackend
}

type startProfileResolution struct {
	requestedPlaybackMode string
	publicRequestProfile  string
	effectiveProfileID    string
	profileSpec           model.ProfileSpec
	operatorSnapshot      profiles.OperatorOverrideSnapshot
	hostPressureBand      playbackprofile.HostPressureBand
	hostOverrideApplied   bool
	bucket                string
	idempotencyKey        string
	resolvedIntent        string
	degradedFrom          string
	autoCodecTrace        autocodec.SelectionTrace
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) ProcessIntent(ctx context.Context, intent Intent) (*Result, *Error) {
	switch intent.Type {
	case model.IntentTypeStreamStart:
		return s.processStart(ctx, intent)
	case model.IntentTypeStreamStop:
		return s.processStop(ctx, intent)
	default:
		return nil, &Error{Kind: ErrorInvalidInput, Message: "unsupported intent type"}
	}
}

func (s *Service) processStart(ctx context.Context, intent Intent) (*Result, *Error) {
	intent.ClientCaps = normalizedClientCaps(intent.ClientCaps)
	store := s.deps.SessionStore()
	bus := s.deps.EventBus()
	// Watchpoint: start intents may use scan capability as a profile hint source
	// (for example interlaced/progressive handling), but they are not a second
	// SSOT for live container/codec readiness. Any future readiness/media-truth
	// branching must go through the live truth resolver used by /live/stream-info.
	capability := s.lookupStartCapability(intent.ServiceRef)
	hardwareState := detectStartHardwareState()
	hwaccelMode, err := s.resolveStartHWAccelMode(intent, hardwareState)
	if err != nil {
		return nil, err
	}
	reqProfileID, requestedPlaybackMode, err := s.resolveRequestedStartProfile(ctx, intent, hwaccelMode, capability)
	if err != nil {
		return nil, err
	}
	resolution, err := s.resolveStartProfile(ctx, intent, capability, hardwareState, hwaccelMode, reqProfileID, requestedPlaybackMode)
	if err != nil {
		return nil, err
	}
	if err := s.checkStartAdmission(ctx, intent, resolution.profileSpec); err != nil {
		return nil, err
	}

	hwaccelEffective, hwaccelReason, encoderBackend := deriveStartHWAccelSummary(resolution.profileSpec, hwaccelMode, hardwareState.hasGPU)
	s.logStartProfileResolution(intent, resolution, hardwareState, hwaccelMode, hwaccelEffective, hwaccelReason, encoderBackend)

	if !s.deps.HasTunerSlots() {
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "no_slots")
		return nil, &Error{Kind: ErrorNoTunerSlots, Message: "no tuner slots configured", RetryAfter: "10"}
	}

	phaseLabel := "phase2"
	session := s.buildStartSession(intent, resolution)
	if replay, err := s.persistStartSession(ctx, intent, store, session, resolution.idempotencyKey, phaseLabel); err != nil {
		return nil, err
	} else if replay != nil {
		return replay, nil
	}
	if err := s.publishStartSession(ctx, intent, bus, resolution.effectiveProfileID, phaseLabel); err != nil {
		return nil, err
	}

	intent.Logger.Info().Msg("intent accepted")
	s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "accepted")

	return &Result{
		SessionID:     intent.SessionID,
		Status:        "accepted",
		CorrelationID: intent.CorrelationID,
	}, nil
}

func (s *Service) lookupStartCapability(serviceRef string) *scan.Capability {
	if scanner := s.deps.ChannelScanner(); scanner != nil {
		if capability, found := scanner.GetCapability(serviceRef); found {
			return &capability
		}
	}
	return nil
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
	resolution.profileSpec = clientpolicy.ApplyStartPackagingPolicy(clientFamily, resolution.effectiveProfileID, resolution.profileSpec)
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

func (s *Service) checkStartAdmission(ctx context.Context, intent Intent, profileSpec model.ProfileSpec) *Error {
	controller := s.deps.AdmissionController()
	if controller == nil {
		return &Error{Kind: ErrorAdmissionUnavailable}
	}
	decision := controller.Check(ctx, admission.Request{WantsTranscode: profileSpec.TranscodeVideo}, s.deps.AdmissionRuntimeState(ctx))
	if !decision.Allow {
		if decision.Problem != nil {
			s.deps.RecordReject(decision.Problem.Code)
		}

		retryAfter := ""
		if decision.RetryAfterSeconds != nil {
			retryAfter = fmt.Sprintf("%d", *decision.RetryAfterSeconds)
		} else if decision.Problem != nil && (decision.Problem.Code == admission.CodeNoTuners || decision.Problem.Code == admission.CodeSessionsFull) {
			retryAfter = "5"
		}

		problemCode := "admission_rejected"
		if decision.Problem != nil {
			problemCode = decision.Problem.Code
		}
		intent.Logger.Info().
			Str("serviceRef", intent.ServiceRef).
			Str("code", problemCode).
			Msg("admission rejected")

		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "admission", problemCode)
		return &Error{Kind: ErrorAdmissionRejected, RetryAfter: retryAfter, AdmissionProblem: decision.Problem}
	}
	s.deps.RecordAdmit()
	return nil
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

func shouldTraceAutoCodecDecision(intent Intent, requestedCodecs string) bool {
	return strings.TrimSpace(intent.Params["profile"]) == "" && strings.TrimSpace(requestedCodecs) != ""
}

func buildStartRequestParams(intent Intent, resolution startProfileResolution) map[string]string {
	requestParams := map[string]string{
		"profile": resolution.effectiveProfileID,
		"bucket":  resolution.bucket,
	}
	if resolution.requestedPlaybackMode != "" {
		requestParams[model.CtxKeyClientPath] = resolution.requestedPlaybackMode
	}
	if clientFamily := clientFamilyForIntent(intent); clientFamily != "" {
		requestParams[model.CtxKeyClientFamily] = clientFamily
	}
	if preferredEngine := preferredEngineForIntent(intent); preferredEngine != "" {
		requestParams[model.CtxKeyPreferredEngine] = preferredEngine
	}
	if deviceType := deviceTypeForIntent(intent); deviceType != "" {
		requestParams[model.CtxKeyDeviceType] = deviceType
	}
	if requestedCodecs := requestedCodecsForIntent(intent, resolution.requestedPlaybackMode); requestedCodecs != "" {
		requestParams["codecs"] = requestedCodecs
	}
	if capHash := clientCapHashForIntent(intent); capHash != "" {
		requestParams["capHash"] = capHash
	}
	if intent.CorrelationID != "" {
		requestParams["correlationId"] = intent.CorrelationID
	}
	if intent.DecisionTrace != "" {
		requestParams[model.CtxKeyDecisionRequest] = intent.DecisionTrace
	}
	if principalID := normalize.Token(intent.PrincipalID); principalID != "" {
		requestParams[model.CtxKeyPrincipalID] = principalID
	}
	if intent.Mode != "" {
		requestParams[model.CtxKeyMode] = intent.Mode
	}
	return requestParams
}

func buildStartOperatorTrace(snapshot profiles.OperatorOverrideSnapshot) *model.PlaybackOperatorTrace {
	if !snapshot.OverrideApplied && snapshot.ForcedIntent == playbackprofile.IntentUnknown && snapshot.MaxQualityRung == playbackprofile.RungUnknown && !snapshot.DisableClientFallback {
		return nil
	}
	return &model.PlaybackOperatorTrace{
		ForcedIntent:           playbackprofile.PublicIntentName(snapshot.ForcedIntent),
		MaxQualityRung:         string(snapshot.MaxQualityRung),
		ClientFallbackDisabled: snapshot.DisableClientFallback,
		RuleName:               snapshot.RuleName,
		RuleScope:              snapshot.RuleScope,
		OverrideApplied:        snapshot.OverrideApplied,
	}
}

func (s *Service) buildStartSession(intent Intent, resolution startProfileResolution) *model.SessionRecord {
	targetProfile := model.TraceTargetProfileFromProfile(resolution.profileSpec)
	targetVideoQualityRung := model.TraceVideoQualityRungFromProfile(resolution.profileSpec)
	targetStep := runtimepolicy.PlaybackLadderStepFromTargetProfile(targetProfile, playbackprofile.NormalizeQualityRung(targetVideoQualityRung))
	startupProfile, _ := capLiveStartupProfile(intent, resolution.profileSpec, targetStep)
	videoQualityRung := model.TraceVideoQualityRungFromProfile(startupProfile)
	now := time.Now()
	session := lifecycle.NewSessionRecord(now)
	session.SessionID = intent.SessionID
	session.ServiceRef = intent.ServiceRef
	session.Profile = startupProfile
	session.CorrelationID = intent.CorrelationID
	session.LeaseExpiresAtUnix = now.Add(s.deps.SessionLeaseTTL()).Unix()
	session.HeartbeatInterval = int(s.deps.SessionHeartbeatInterval().Seconds())
	session.ContextData = buildStartRequestParams(intent, resolution)
	session.PlaybackTrace = &model.PlaybackTrace{
		RequestProfile:      resolution.publicRequestProfile,
		RequestedIntent:     resolution.publicRequestProfile,
		ResolvedIntent:      resolution.resolvedIntent,
		QualityRung:         videoQualityRung,
		VideoQualityRung:    videoQualityRung,
		DegradedFrom:        resolution.degradedFrom,
		ClientPath:          resolution.requestedPlaybackMode,
		Operator:            buildStartOperatorTrace(resolution.operatorSnapshot),
		Client:              buildStartClientSnapshot(intent, now),
		HostPressureBand:    string(resolution.hostPressureBand),
		HostOverrideApplied: resolution.hostOverrideApplied,
		AutoCodecPolicy:     resolution.autoCodecTrace.Policy,
		AutoCodecRequested:  resolution.autoCodecTrace.RequestedCodecs,
		AutoCodecSelected:   resolution.autoCodecTrace.SelectedCodec,
		AutoCodecHostClass:  resolution.autoCodecTrace.PerformanceClass,
		AutoCodecBenchClass: resolution.autoCodecTrace.CodecBenchmarkClass,
	}
	if session.ContextData == nil {
		session.ContextData = make(map[string]string, 2)
	}
	if targetStep != runtimepolicy.PlaybackStepUnknown {
		session.ContextData[model.CtxKeyRuntimeTargetStep] = string(targetStep)
	}
	if session.ContextData == nil {
		session.ContextData = make(map[string]string, 2)
	}
	if targetStep := runtimepolicy.PlaybackLadderStepFromTargetProfile(session.PlaybackTrace.TargetProfile, playbackprofile.NormalizeQualityRung(videoQualityRung)); targetStep != runtimepolicy.PlaybackStepUnknown {
		session.ContextData[model.CtxKeyRuntimeTargetStep] = string(targetStep)
	}
	return session
}

func (s *Service) persistStartSession(ctx context.Context, intent Intent, store SessionStore, session *model.SessionRecord, idempotencyKey, phaseLabel string) (*Result, *Error) {
	if replay, err := resolveReusableLiveStart(ctx, store, intent, session); err != nil {
		intent.Logger.Error().Err(err).Msg("failed to inspect active live sessions")
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
		return nil, &Error{Kind: ErrorStoreUnavailable, Message: "failed to inspect active live sessions", Cause: err}
	} else if replay != nil {
		s.deps.RecordReplay(string(model.IntentTypeStreamStart))
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "replay")
		intent.Logger.Info().Str("existing_sid", replay.SessionID).Msg("reused matching active live session")
		return replay, nil
	}

	persisted := false
	for attempt := 0; attempt < startReplayRecoveryAttempts; attempt++ {
		existingID, exists, err := store.PutSessionWithIdempotency(ctx, session, idempotencyKey, admissionLeaseTTL)
		if err != nil {
			intent.Logger.Error().Err(err).Msg("failed to persist intent")
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
			return nil, &Error{Kind: ErrorStoreUnavailable, Message: "failed to persist intent", Cause: err}
		}
		if !exists {
			persisted = true
			break
		}

		replay, retry, replayErr := resolveStartReplay(ctx, store, idempotencyKey, existingID, intent.CorrelationID)
		if replayErr != nil {
			intent.Logger.Error().Err(replayErr).Str("existing_sid", existingID).Msg("failed to reconcile stale idempotent replay")
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
			return nil, &Error{Kind: ErrorStoreUnavailable, Message: "failed to reconcile idempotent replay", Cause: replayErr}
		}
		if replay != nil {
			s.deps.RecordReplay(string(model.IntentTypeStreamStart))
			s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "replay")
			intent.Logger.Info().Str("existing_sid", existingID).Msg("idempotent replay detected")
			return &Result{
				SessionID:     existingID,
				Status:        "idempotent_replay",
				CorrelationID: replay.correlationID,
			}, nil
		}
		if retry {
			intent.Logger.Warn().Str("existing_sid", existingID).Int("attempt", attempt+1).Msg("discarded stale idempotent replay for terminal session")
		}
	}
	if persisted {
		return nil, nil
	}

	err := fmt.Errorf("stale idempotency mapping persisted after %d attempts", startReplayRecoveryAttempts)
	intent.Logger.Error().Err(err).Str("idem_key", idempotencyKey).Msg("failed to refresh stale idempotency mapping")
	s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
	return nil, &Error{Kind: ErrorStoreUnavailable, Message: "failed to refresh stale intent mapping", Cause: err}
}

func (s *Service) publishStartSession(ctx context.Context, intent Intent, bus EventBus, effectiveProfileID, phaseLabel string) *Error {
	evt := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     intent.SessionID,
		ServiceRef:    intent.ServiceRef,
		ProfileID:     effectiveProfileID,
		CorrelationID: intent.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if intent.StartMs != nil {
		evt.StartMs = *intent.StartMs
	}

	if err := bus.Publish(ctx, string(model.EventStartSession), evt); err != nil {
		intent.Logger.Error().Err(err).Msg("failed to publish start event")
		s.deps.RecordPublish("session.start", "error")
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "publish_error")
		return &Error{Kind: ErrorPublishUnavailable, Message: "failed to publish event", Cause: err}
	}
	s.deps.RecordPublish("session.start", "ok")
	return nil
}

func (s *Service) processStop(ctx context.Context, intent Intent) (*Result, *Error) {
	bus := s.deps.EventBus()
	event := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     intent.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: intent.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(ctx, string(model.EventStopSession), event); err != nil {
		intent.Logger.Error().Err(err).Msg("failed to publish stop event")
		s.deps.RecordPublish("session.stop", "error")
		s.deps.RecordIntent(string(model.IntentTypeStreamStop), "any", "publish_error")
		return nil, &Error{Kind: ErrorPublishUnavailable, Message: "failed to dispatch intent", Cause: err}
	}
	s.deps.RecordPublish("session.stop", "ok")
	s.deps.RecordIntent(string(model.IntentTypeStreamStop), "any", "accepted")

	return &Result{
		SessionID:     intent.SessionID,
		Status:        "accepted",
		CorrelationID: intent.CorrelationID,
	}, nil
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	payload := fmt.Sprintf("v1:%s:%s:%s:%s", intentType, ref, profile, bucket)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}

func resolvePlaybackDecisionToken(requestToken string, params map[string]string) (token, keyLabel, resultLabel string, err error) {
	canonicalToken := strings.TrimSpace(requestToken)
	paramToken := strings.TrimSpace(params["playback_decision_token"])
	paramID := strings.TrimSpace(params["playback_decision_id"])

	switch {
	case canonicalToken == "":
		switch {
		case paramToken == "" && paramID == "":
			return "", "none", "rejected_missing", fmt.Errorf("playbackDecisionToken is required when playback_mode is provided")
		default:
			return "", "params_only", "rejected_missing", fmt.Errorf("playbackDecisionToken is required when playback_mode is provided")
		}
	case paramToken != "" && paramToken != canonicalToken:
		return "", "request+playback_decision_token", "mismatch", fmt.Errorf("params.playback_decision_token must match playbackDecisionToken")
	case paramID != "" && paramID != canonicalToken:
		return "", "request+playback_decision_id", "mismatch", fmt.Errorf("params.playback_decision_id must match playbackDecisionToken")
	case paramToken != "" && paramID != "":
		return canonicalToken, "all", "equal", nil
	case paramToken != "":
		return canonicalToken, "request+playback_decision_token", "equal", nil
	case paramID != "":
		return canonicalToken, "request+playback_decision_id", "equal", nil
	default:
		return canonicalToken, "request", "accepted", nil
	}
}

func requestedCodecsForIntent(intent Intent, requestedPlaybackMode string) string {
	if explicit := joinRequestedCodecs(autocodec.ParseCodecList(intent.Params["codecs"])); explicit != "" {
		return clampRequestedCodecsForClient(intent, requestedPlaybackMode, explicit)
	}
	if derived := clampRequestedCodecsForClient(intent, requestedPlaybackMode, requestedCodecsFromClientCaps(intent, requestedPlaybackMode)); derived != "" {
		return derived
	}
	return clampRequestedCodecsForClient(intent, requestedPlaybackMode, requestedCodecsFromClientMatrix(intent, requestedPlaybackMode))
}

func requestedCodecsFromClientCaps(intent Intent, requestedPlaybackMode string) string {
	clientCaps := intent.ClientCaps
	if clientCaps == nil {
		return ""
	}

	codecs := append([]string(nil), autocodec.ResolveAutoTranscodeCodecs(*clientCaps)...)
	if requestedPlaybackMode == "native_hls" || requestedPlaybackMode == "" {
		source := normalize.Token(clientCaps.ClientCapsSource)
		if source == capabilities.ClientCapsSourceRuntime || source == capabilities.ClientCapsSourceRuntimePlusFam {
			if clientCapsHasCodec(clientCaps.VideoCodecs, "av1") {
				codecs = append(codecs, "av1")
			}
		}
		if source == capabilities.ClientCapsSourceRuntime ||
			source == capabilities.ClientCapsSourceRuntimePlusFam ||
			source == capabilities.ClientCapsSourceFamilyFallback {
			if clientCapsHasCodec(clientCaps.VideoCodecs, "hevc") {
				codecs = append(codecs, "hevc")
			}
		}
	}
	if clientCapsHasCodec(clientCaps.VideoCodecs, "h264") || len(codecs) == 0 {
		codecs = append(codecs, "h264")
	}
	return joinRequestedCodecs(mergeRequestedCodecLists(codecs, matrixFallbackVideoCodecs(intent, requestedPlaybackMode)))
}

func clampRequestedCodecsForClient(intent Intent, requestedPlaybackMode, requestedCodecs string) string {
	allowedCodecs := allowedRequestedCodecsForClient(intent, requestedPlaybackMode)
	clientFamily := clientFamilyForIntent(intent)
	if clientFamily == playbackprofile.ClientIOSSafariNative &&
		startPlaybackPath(intent, requestedPlaybackMode) == "hlsjs" &&
		!iosSafariManagedAV1Allowed(intent.ClientCaps) {
		allowedCodecs = []string{"h264"}
	}
	return mergeRequestedCodecsWithAllowed(requestedCodecs, allowedCodecs)
}

func requestedCodecsFromClientMatrix(intent Intent, requestedPlaybackMode string) string {
	return joinRequestedCodecs(matrixFallbackVideoCodecs(intent, requestedPlaybackMode))
}

func allowedRequestedCodecsForClient(intent Intent, requestedPlaybackMode string) []string {
	canonicalCaps := normalizedClientCaps(intent.ClientCaps)
	if canonicalCaps != nil && len(canonicalCaps.VideoCodecs) > 0 {
		return mergeRequestedCodecLists(preferredRequestedCodecOrder(canonicalCaps.VideoCodecs), matrixFallbackVideoCodecs(intent, requestedPlaybackMode))
	}
	return matrixFallbackVideoCodecs(intent, requestedPlaybackMode)
}

func matrixFallbackVideoCodecs(intent Intent, requestedPlaybackMode string) []string {
	clientFamily := clientFamilyForIntent(intent)
	switch startPlaybackPath(intent, requestedPlaybackMode) {
	case "hlsjs":
		switch clientFamily {
		case playbackprofile.ClientSafariNative,
			playbackprofile.ClientIOSSafariNative,
			playbackprofile.ClientFirefoxHLSJS,
			playbackprofile.ClientChromiumHLSJS:
			return []string{"h264"}
		}
	case "android_native":
		return []string{"h264"}
	}

	if fixture, ok := playbackprofile.ClientFixture(clientFamily); ok {
		return preferredRequestedCodecOrder(fixture.VideoCodecs)
	}

	switch clientFamily {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
		return []string{"hevc", "h264"}
	case playbackprofile.ClientFirefoxHLSJS, playbackprofile.ClientChromiumHLSJS:
		return []string{"h264"}
	default:
		return nil
	}
}

func startPlaybackPath(intent Intent, requestedPlaybackMode string) string {
	switch normalize.Token(requestedPlaybackMode) {
	case "native_hls", "hlsjs", "transcode", "direct_mp4", "android_native":
		return normalize.Token(requestedPlaybackMode)
	}
	switch preferredEngineForIntent(intent) {
	case "hlsjs":
		return "hlsjs"
	case "native":
		return "native_hls"
	default:
		return ""
	}
}

func preferredRequestedCodecOrder(codecs []string) []string {
	out := make([]string, 0, 3)
	for _, codec := range []string{"av1", "hevc", "h264"} {
		if clientCapsHasCodec(codecs, codec) {
			out = append(out, codec)
		}
	}
	return out
}

func mergeRequestedCodecsWithAllowed(requestedCodecs string, allowedCodecs []string) string {
	requested := autocodec.ParseCodecList(requestedCodecs)
	allowed := preferredRequestedCodecOrder(allowedCodecs)

	if len(requested) == 0 {
		return joinRequestedCodecs(allowed)
	}
	if len(allowed) == 0 {
		return joinRequestedCodecs(requested)
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, codec := range allowed {
		allowedSet[codec] = struct{}{}
	}

	merged := make([]string, 0, len(allowed))
	for _, codec := range requested {
		if _, ok := allowedSet[codec]; !ok {
			continue
		}
		merged = append(merged, codec)
	}
	for _, codec := range allowed {
		if clientCapsHasCodec(merged, codec) {
			continue
		}
		merged = append(merged, codec)
	}
	return joinRequestedCodecs(merged)
}

func mergeRequestedCodecLists(primary, secondary []string) []string {
	combined := make([]string, 0, len(primary)+len(secondary))
	combined = append(combined, primary...)
	combined = append(combined, secondary...)
	return autocodec.ParseCodecList(joinRequestedCodecs(combined))
}

func iosSafariManagedAV1Allowed(clientCaps *capabilities.PlaybackCapabilities) bool {
	canonicalCaps := normalizedClientCaps(clientCaps)
	if canonicalCaps == nil {
		return false
	}

	source := normalize.Token(canonicalCaps.ClientCapsSource)
	if source != capabilities.ClientCapsSourceRuntime && source != capabilities.ClientCapsSourceRuntimePlusFam {
		return false
	}
	if normalize.Token(canonicalCaps.PreferredHLSEngine) != "hlsjs" {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.HLSEngines, "hlsjs") {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.Containers, "fmp4") {
		return false
	}
	if !clientCapsHasCodec(canonicalCaps.VideoCodecs, "av1") {
		return false
	}

	for _, signal := range canonicalCaps.VideoCodecSignals {
		if normalize.Token(signal.Codec) != "av1" {
			continue
		}
		if signal.Supported {
			return true
		}
		if signal.Smooth != nil && *signal.Smooth {
			return true
		}
	}
	return false
}

func joinRequestedCodecs(codecs []string) string {
	if len(codecs) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(codecs))
	ordered := make([]string, 0, len(codecs))
	for _, raw := range codecs {
		canonical := canonicalRequestedCodec(raw)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		ordered = append(ordered, canonical)
	}
	return strings.Join(ordered, ",")
}

func canonicalRequestedCodec(raw string) string {
	parsed := autocodec.ParseCodecList(raw)
	if len(parsed) == 0 {
		return ""
	}
	return parsed[0]
}

func clientCapsHasCodec(values []string, want string) bool {
	want = normalize.Token(want)
	for _, value := range values {
		if normalize.Token(value) == want {
			return true
		}
	}
	return false
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
