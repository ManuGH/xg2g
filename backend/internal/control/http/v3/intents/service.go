package intents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/admission"
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

	// Smart Profile Lookup
	var cap *scan.Capability
	if scanner := s.deps.ChannelScanner(); scanner != nil {
		if c, found := scanner.GetCapability(intent.ServiceRef); found {
			cap = &c
		}
	}

	hasGPU := hardware.IsVAAPIReady()
	av1OK := hardware.IsVAAPIEncoderReady("av1_vaapi")
	hevcOK := hardware.IsVAAPIEncoderReady("hevc_vaapi")
	h264OK := hardware.IsVAAPIEncoderReady("h264_vaapi")

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
			return nil, &Error{
				Kind:    ErrorInvalidInput,
				Message: fmt.Sprintf("invalid hwaccel value: %q (must be auto, force, or off)", hwaccel),
			}
		}
	}

	if hwaccelMode == profiles.HWAccelForce && !hasGPU {
		reason := "no /dev/dri/renderD128"
		if hardware.HasVAAPI() {
			reason = "VAAPI preflight encode test failed"
		}
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "hwaccel_unavailable")
		return nil, &Error{
			Kind:    ErrorInvalidInput,
			Message: fmt.Sprintf("hwaccel=force requested but GPU not available (%s)", reason),
		}
	}

	reqProfileID := "universal"
	requestedPlaybackMode := normalize.Token(intent.Params["playback_mode"])
	if requestedPlaybackMode != "" {
		_, keyLabel, resultLabel, tokenErr := resolvePlaybackDecisionToken(intent.Params)
		if tokenErr != nil {
			s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
			return nil, &Error{Kind: ErrorInvalidInput, Message: tokenErr.Error()}
		}
		s.deps.IncLivePlaybackKey(keyLabel, resultLabel)
		mappedProfile, mapErr := mapPlaybackModeToProfile(requestedPlaybackMode)
		if mapErr != nil {
			return nil, &Error{Kind: ErrorInvalidInput, Message: mapErr.Error()}
		}
		reqProfileID = mappedProfile
	} else if p := normalize.Token(intent.Params["profile"]); p != "" {
		reqProfileID = p
	} else if picked := pickProfileForCodecs(intent.Params["codecs"], av1OK, hevcOK, h264OK, hwaccelMode); picked != "" {
		reqProfileID = picked
	}
	publicRequestProfile := profiles.PublicProfileName(reqProfileID)
	reqProfileID = profiles.NormalizeRequestedProfileID(reqProfileID)

	bucket := "0"
	if intent.StartMs != nil && *intent.StartMs > 0 {
		bucket = fmt.Sprintf("%d", *intent.StartMs/1000)
	}
	idempotencyKey := ComputeIdemKey(model.IntentTypeStreamStart, intent.ServiceRef, reqProfileID, bucket)

	resolveHasGPU := hasGPU
	switch reqProfileID {
	case profiles.ProfileAV1HW:
		resolveHasGPU = av1OK
	case profiles.ProfileSafariHEVCHW:
		resolveHasGPU = hevcOK
	case profiles.ProfileH264FMP4:
		resolveHasGPU = h264OK
	}
	profileUserAgent := intent.UserAgent
	if requestedPlaybackMode != "" {
		profileUserAgent = ""
	}

	profileSpec := profiles.Resolve(reqProfileID, profileUserAgent, int(s.deps.DVRWindow().Seconds()), cap, resolveHasGPU, hwaccelMode)
	resolvedIntent := profiles.PublicProfileName(profileSpec.Name)
	degradedFrom := ""
	if publicRequestProfile != "" && resolvedIntent != "" && publicRequestProfile != resolvedIntent {
		degradedFrom = publicRequestProfile
	}

	controller := s.deps.AdmissionController()
	if controller == nil {
		return nil, &Error{Kind: ErrorAdmissionUnavailable}
	}

	runtimeState := s.deps.AdmissionRuntimeState(ctx)
	wantsTranscode := profileSpec.TranscodeVideo
	decision := controller.Check(ctx, admission.Request{WantsTranscode: wantsTranscode}, runtimeState)
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
		return nil, &Error{Kind: ErrorAdmissionRejected, RetryAfter: retryAfter, AdmissionProblem: decision.Problem}
	}

	s.deps.RecordAdmit()

	var hwaccelEffective, hwaccelReason, encoderBackend string
	if profileSpec.TranscodeVideo {
		if profiles.IsGPUBackedProfile(profileSpec.HWAccel) {
			hwaccelEffective = "gpu"
			encoderBackend = "vaapi"
			if hwaccelMode == profiles.HWAccelForce {
				hwaccelReason = "forced"
			} else {
				hwaccelReason = "auto_has_gpu"
			}
		} else {
			hwaccelEffective = "cpu"
			encoderBackend = profileSpec.VideoCodec
			if hwaccelMode == profiles.HWAccelOff {
				hwaccelReason = "user_disabled"
			} else if !hasGPU {
				hwaccelReason = "no_gpu_available"
			} else {
				hwaccelReason = "profile_cpu_only"
			}
		}
	} else {
		hwaccelEffective = "off"
		hwaccelReason = "passthrough"
		encoderBackend = "none"
	}

	intent.Logger.Info().
		Str("ua", intent.UserAgent).
		Str("profile", profileSpec.Name).
		Str("profile_public", publicRequestProfile).
		Int("dvr_window_sec", profileSpec.DVRWindowSec).
		Str("idem_key", idempotencyKey).
		Bool("gpu_available", hasGPU).
		Str("hwaccel_requested", string(hwaccelMode)).
		Str("hwaccel_effective", hwaccelEffective).
		Str("hwaccel_reason", hwaccelReason).
		Str("encoder_backend", encoderBackend).
		Str("video_codec", profileSpec.VideoCodec).
		Str("container", profileSpec.Container).
		Bool("llhls", profileSpec.LLHLS).
		Msg("intent profile resolved")

	if !s.deps.HasTunerSlots() {
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), "phase0", "no_slots")
		return nil, &Error{Kind: ErrorNoTunerSlots, Message: "no tuner slots configured", RetryAfter: "10"}
	}

	phaseLabel := "phase2"
	requestParams := map[string]string{
		"profile": reqProfileID,
		"bucket":  bucket,
	}
	if requestedPlaybackMode != "" {
		requestParams[model.CtxKeyClientPath] = requestedPlaybackMode
	}
	if raw := intent.Params["codecs"]; raw != "" {
		requestParams["codecs"] = raw
	}
	if intent.CorrelationID != "" {
		requestParams["correlationId"] = intent.CorrelationID
	}
	if intent.Mode != "" {
		requestParams[model.CtxKeyMode] = intent.Mode
	}

	session := lifecycle.NewSessionRecord(time.Now())
	session.SessionID = intent.SessionID
	session.ServiceRef = intent.ServiceRef
	session.Profile = profileSpec
	session.CorrelationID = intent.CorrelationID
	session.LeaseExpiresAtUnix = time.Now().Add(s.deps.SessionLeaseTTL()).Unix()
	session.HeartbeatInterval = int(s.deps.SessionHeartbeatInterval().Seconds())
	session.ContextData = requestParams
	session.PlaybackTrace = &model.PlaybackTrace{
		RequestProfile:  publicRequestProfile,
		RequestedIntent: publicRequestProfile,
		ResolvedIntent:  resolvedIntent,
		DegradedFrom:    degradedFrom,
		ClientPath:      requestedPlaybackMode,
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
	if !persisted {
		err := fmt.Errorf("stale idempotency mapping persisted after %d attempts", startReplayRecoveryAttempts)
		intent.Logger.Error().Err(err).Str("idem_key", idempotencyKey).Msg("failed to refresh stale idempotency mapping")
		s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
		return nil, &Error{Kind: ErrorStoreUnavailable, Message: "failed to refresh stale intent mapping", Cause: err}
	}

	evt := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     intent.SessionID,
		ServiceRef:    intent.ServiceRef,
		ProfileID:     reqProfileID,
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
		return nil, &Error{Kind: ErrorPublishUnavailable, Message: "failed to publish event", Cause: err}
	}
	s.deps.RecordPublish("session.start", "ok")

	intent.Logger.Info().Msg("intent accepted")
	s.deps.RecordIntent(string(model.IntentTypeStreamStart), phaseLabel, "accepted")

	return &Result{
		SessionID:     intent.SessionID,
		Status:        "accepted",
		CorrelationID: intent.CorrelationID,
	}, nil
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

func mapPlaybackModeToProfile(mode string) (string, error) {
	switch mode {
	case "native_hls":
		// native_hls should use the normal Safari profile first:
		// progressive inputs stay remux/copy, interlaced or unknown inputs transcode.
		// More aggressive recovery (safari_dirty / repair) is handled after runtime errors.
		return profiles.ProfileSafari, nil
	case "hlsjs", "direct_mp4":
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

func pickProfileForCodecs(raw string, av1OK, hevcOK, h264OK bool, hwaccelMode profiles.HWAccelMode) string {
	codecs := parseCodecList(raw)
	if len(codecs) == 0 {
		return ""
	}

	hwAllowed := hwaccelMode != profiles.HWAccelOff

	for _, c := range codecs {
		switch c {
		case "av1":
			if hwAllowed && av1OK {
				return profiles.ProfileAV1HW
			}
		case "hevc":
			if hwAllowed && hevcOK {
				return profiles.ProfileSafariHEVCHW
			}
			return profiles.ProfileSafariHEVC
		case "h264":
			_ = h264OK
			return profiles.ProfileH264FMP4
		}
	}

	return ""
}

func parseCodecList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n' || r == '\r'
	})

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t == "" {
			continue
		}
		switch t {
		case "av01":
			t = "av1"
		case "h265":
			t = "hevc"
		case "h.265":
			t = "hevc"
		case "h264":
			t = "h264"
		case "avc":
			t = "h264"
		case "avc1":
			t = "h264"
		}
		if t != "av1" && t != "hevc" && t != "h264" {
			continue
		}
		out = append(out, t)
	}
	return out
}
