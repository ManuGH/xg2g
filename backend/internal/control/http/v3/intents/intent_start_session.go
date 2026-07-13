package intents

import (
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"strconv"
	"time"
)

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
	if v := intent.Params["dvr_window_sec"]; v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
			startupProfile.DVRWindowSec = sec
		}
	} else if intent.Params["dvr"] == "true" || intent.Params["dvr_window"] == "true" {
		if startupProfile.DVRWindowSec <= 0 {
			startupProfile.DVRWindowSec = 7200
		}
	} else if intent.Params["dvr"] == "false" || intent.Params["dvr_window"] == "false" {
		startupProfile.DVRWindowSec = 0
	}
	switch intent.Params["multi_audio"] {
	case "true":
		startupProfile.EnableMultiAudio = true
	case "false":
		startupProfile.EnableMultiAudio = false
	}

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
		Source:              resolution.sourceProfile,
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
	for attempt := range startReplayRecoveryAttempts {
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
