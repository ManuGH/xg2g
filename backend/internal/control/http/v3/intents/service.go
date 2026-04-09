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
	reqProfileID, requestedPlaybackMode, err := s.resolveRequestedStartProfile(intent, hwaccelMode)
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

func (s *Service) resolveRequestedStartProfile(intent Intent, hwaccelMode profiles.HWAccelMode) (string, string, *Error) {
	reqProfileID := "universal"
	requestedPlaybackMode := normalize.Token(intent.Params["playback_mode"])
	clientFamily := clientFamilyForIntent(intent)
	if requestedPlaybackMode != "" {
		_, keyLabel, resultLabel, tokenErr := resolvePlaybackDecisionToken(intent.Params)
		if tokenErr != nil {
			s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
			return "", "", &Error{Kind: ErrorInvalidInput, Message: tokenErr.Error()}
		}
		s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
		mappedProfile, mapErr := mapPlaybackModeToProfile(requestedPlaybackMode, clientFamily)
		if mapErr != nil {
			return "", "", &Error{Kind: ErrorInvalidInput, Message: mapErr.Error()}
		}
		reqProfileID = mappedProfile
		if requestedPlaybackMode == "transcode" {
			if picked := pickProfileForCodecs(intent.Params["codecs"], hwaccelMode); picked != "" {
				reqProfileID = picked
			}
		}
		return reqProfileID, requestedPlaybackMode, nil
	}
	if profileID := normalize.Token(intent.Params["profile"]); profileID != "" {
		return profileID, "", nil
	}
	if picked := pickProfileForCodecs(intent.Params["codecs"], hwaccelMode); picked != "" {
		return picked, "", nil
	}
	return reqProfileID, "", nil
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
	resolution.idempotencyKey = ComputeIdemKey(model.IntentTypeStreamStart, intent.ServiceRef, resolution.effectiveProfileID, resolution.bucket)

	profileUserAgent := ""
	clientFamily := clientFamilyForIntent(intent)
	switch requestedPlaybackMode {
	case "", "native_hls":
		// Explicit playback modes usually bypass UA sniffing, but native_hls
		// still needs the real Safari UA to distinguish browser Safari
		// (mpegts copy) from non-Safari/native clients.
		profileUserAgent = intent.UserAgent
	case "hlsjs":
		// Desktop Safari can prefer hls.js while still requiring Safari-specific
		// packaging choices for dirty live transcodes.
		if clientFamily == playbackprofile.ClientSafariNative || clientFamily == playbackprofile.ClientIOSSafariNative {
			profileUserAgent = intent.UserAgent
		}
	}
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
	intent.Logger.Info().
		Str("ua", intent.UserAgent).
		Str("profile", resolution.profileSpec.Name).
		Str("profile_public", resolution.publicRequestProfile).
		Str("profile_effective", resolution.effectiveProfileID).
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
		Str("encoder_backend", encoderBackend).
		Str("video_codec", resolution.profileSpec.VideoCodec).
		Str("container", resolution.profileSpec.Container).
		Bool("llhls", resolution.profileSpec.LLHLS).
		Msg("intent profile resolved")
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
	if raw := intent.Params["codecs"]; raw != "" {
		requestParams["codecs"] = raw
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
	videoQualityRung := model.TraceVideoQualityRungFromProfile(resolution.profileSpec)
	now := time.Now()
	session := lifecycle.NewSessionRecord(now)
	session.SessionID = intent.SessionID
	session.ServiceRef = intent.ServiceRef
	session.Profile = resolution.profileSpec
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
	}
	return session
}

func (s *Service) persistStartSession(ctx context.Context, intent Intent, store SessionStore, session *model.SessionRecord, idempotencyKey, phaseLabel string) (*Result, *Error) {
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

func mapPlaybackModeToProfile(mode, clientFamily string) (string, error) {
	switch mode {
	case "native_hls":
		// native_hls is the Safari/iOS native HLS path:
		// progressive inputs stay remux/copy, interlaced or unknown inputs transcode.
		// More aggressive recovery (safari_dirty / repair) is handled after runtime errors.
		return profiles.ProfileSafari, nil
	case "android_native":
		// Android ExoPlayer: video copy + AAC in mpegts.
		// Separate from native_hls to avoid fMP4 codec-parameter issues.
		return profiles.ProfileAndroid, nil
	case "hlsjs":
		// Desktop Safari can prefer hls.js while still reporting the Safari native
		// client family. Starting those sessions on the generic copy-first "high"
		// ladder causes dirty 1080i sports feeds to bounce into repair fallback.
		// Route them straight onto the robust Safari dirty transcode path.
		if clientFamily == playbackprofile.ClientSafariNative {
			return profiles.ProfileSafariDirty, nil
		}
		return profiles.ProfileHigh, nil
	case "direct_mp4":
		return profiles.ProfileHigh, nil
	case "transcode":
		return profiles.ProfileH264FMP4, nil
	case "deny":
		return "", fmt.Errorf("playback_mode=deny cannot start a live session")
	default:
		return "", fmt.Errorf("unsupported playback_mode: %q", mode)
	}
}

func resolvePlaybackDecisionToken(params map[string]string) (token, keyLabel, resultLabel string, err error) {
	playbackDecisionToken := strings.TrimSpace(params["playback_decision_token"])
	playbackDecisionID := strings.TrimSpace(params["playback_decision_id"])

	switch {
	case playbackDecisionToken == "" && playbackDecisionID == "":
		return "", "none", "rejected_missing", fmt.Errorf("playback_decision_id or playback_decision_token is required when playback_mode is provided")
	case playbackDecisionToken != "" && playbackDecisionID != "":
		if playbackDecisionToken != playbackDecisionID {
			return "", "both", "mismatch", fmt.Errorf("playback_decision_id and playback_decision_token mismatch")
		}
		return playbackDecisionToken, "both", "equal", nil
	case playbackDecisionToken != "":
		return playbackDecisionToken, "playback_decision_token", "accepted", nil
	default:
		return playbackDecisionID, "playback_decision_id", "accepted", nil
	}
}

func pickProfileForCodecs(raw string, hwaccelMode profiles.HWAccelMode) string {
	return autocodec.PickProfileForCodecs(raw, hwaccelMode)
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
