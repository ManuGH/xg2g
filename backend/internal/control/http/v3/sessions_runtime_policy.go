package v3

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capreg"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

const sessionRuntimePolicyFreshness = 60 * time.Second

func (s *Server) tickSessionRuntimePolicy(ctx context.Context, store SessionStateStore, session *model.SessionRecord, now time.Time) {
	if s == nil || store == nil || session == nil || session.SessionID == "" {
		return
	}

	next, decision, input, changed := s.evaluateSessionRuntimePolicy(ctx, session, now.UTC())
	if !changed {
		return
	}

	var (
		updated    *model.SessionRecord
		restart    bool
		transition runtimepolicy.SessionTransition
		execution  sessionRuntimeTransitionResult
	)
	record, err := store.UpdateSession(ctx, session.SessionID, func(rec *model.SessionRecord) error {
		prev := loadSessionRuntimePolicyState(rec)
		if prev.CurrentStep == runtimepolicy.PlaybackStepUnknown {
			prev.CurrentStep = observedRuntimeStep(rec)
		}
		if prev.TargetStep == runtimepolicy.PlaybackStepUnknown {
			prev.TargetStep = targetRuntimeStep(rec)
		}
		transition = runtimepolicy.PlanSessionTransition(prev, next, decision)
		storeSessionRuntimePolicyState(rec, next)
		if !transition.IsZero() {
			applied, applyErr := applySessionRuntimePolicyTransition(rec, transition, now.UTC())
			if applyErr != nil {
				return applyErr
			}
			execution = applied
			restart = applied.Restart
		}
		timeline := runtimepolicy.AppendTickTrace(loadSessionRuntimeTimeline(rec), buildSessionRuntimeTickTrace(input, next, decision, transition, execution, now.UTC()))
		storeSessionRuntimeTimeline(rec, timeline)
		storeSessionRuntimeReplay(rec, buildSessionRuntimePolicyReplay(rec))
		return nil
	})
	if err != nil {
		log.L().Warn().Err(err).Str("sessionId", session.SessionID).Msg("session runtime policy update failed")
		return
	}
	updated = record
	if restart && updated != nil {
		s.publishSessionRuntimeTransition(ctx, store, updated, transition)
	}
}

func (s *Server) evaluateSessionRuntimePolicy(ctx context.Context, session *model.SessionRecord, now time.Time) (runtimepolicy.SessionLoopState, runtimepolicy.SessionLoopDecision, runtimepolicy.SessionLoopInput, bool) {
	if session == nil {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	s.mu.RLock()
	registry := s.capabilityRegistry
	s.mu.RUnlock()
	if registry == nil {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	decisionRequestID := ""
	if session.ContextData != nil {
		decisionRequestID = strings.TrimSpace(session.ContextData[model.CtxKeyDecisionRequest])
	}
	if decisionRequestID == "" {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	linked, ok, err := registry.LookupDecisionObservation(ctx, decisionRequestID)
	if err != nil {
		log.L().Warn().Err(err).Str("sessionId", session.SessionID).Str("decisionRequestId", decisionRequestID).Msg("session runtime policy decision lookup failed")
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}
	if !ok || strings.TrimSpace(linked.SourceFingerprint) == "" || strings.TrimSpace(linked.DeviceFingerprint) == "" || strings.TrimSpace(linked.HostFingerprint) == "" {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	stateLookup, ok := registry.(capreg.PlaybackPolicyStateLookup)
	if !ok {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	policyState, found, err := stateLookup.LookupPlaybackPolicyState(ctx, capreg.PlaybackPolicyStateQuery{
		SubjectKind:       strings.TrimSpace(linked.SubjectKind),
		SourceFingerprint: strings.TrimSpace(linked.SourceFingerprint),
		DeviceFingerprint: strings.TrimSpace(linked.DeviceFingerprint),
		HostFingerprint:   strings.TrimSpace(linked.HostFingerprint),
	})
	if err != nil {
		log.L().Warn().Err(err).Str("sessionId", session.SessionID).Msg("session runtime policy state lookup failed")
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}
	if !found || policyState.UpdatedAt.Before(now.Add(-sessionRuntimePolicyFreshness)) {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	prev := loadSessionRuntimePolicyState(session)
	input := runtimepolicy.SessionLoopInput{
		ObservedStep:       observedRuntimeStep(session),
		TargetStep:         targetRuntimeStep(session),
		Confidence:         policyState.Confidence,
		StartupWarmupUntil: sessionPlaybackStartupWarmupUntil(session, s.cfg.HLS.Root, sessionRuntimeStartupWarmup),
	}
	next, decision, changed := runtimepolicy.TickSessionLoop(prev, input, now)
	if !changed {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}
	return next, decision, input, true
}

func loadSessionRuntimePolicyState(session *model.SessionRecord) runtimepolicy.SessionLoopState {
	if session == nil || session.ContextData == nil {
		return runtimepolicy.SessionLoopState{}
	}
	raw := strings.TrimSpace(session.ContextData[model.CtxKeyRuntimePolicyState])
	if raw == "" {
		return runtimepolicy.SessionLoopState{}
	}
	var state runtimepolicy.SessionLoopState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return runtimepolicy.SessionLoopState{}
	}
	return runtimepolicy.NormalizeSessionLoopState(state)
}

func storeSessionRuntimePolicyState(session *model.SessionRecord, state runtimepolicy.SessionLoopState) {
	if session == nil {
		return
	}
	state = runtimepolicy.NormalizeSessionLoopState(state)
	if session.ContextData == nil {
		session.ContextData = make(map[string]string, 6)
	}
	if payload, err := json.Marshal(state); err == nil {
		session.ContextData[model.CtxKeyRuntimePolicyState] = string(payload)
	}
	session.ContextData[model.CtxKeyRuntimePolicyAction] = string(state.LastAction)
	session.ContextData[model.CtxKeyRuntimeCurrentStep] = string(state.CurrentStep)
	session.ContextData[model.CtxKeyRuntimeProbeStep] = string(state.ProbeStep)
	session.ContextData[model.CtxKeyRuntimeProbeState] = string(state.ProbeState)
	if state.TargetStep != runtimepolicy.PlaybackStepUnknown {
		session.ContextData[model.CtxKeyRuntimeTargetStep] = string(state.TargetStep)
	}
}

func loadSessionRuntimeTimeline(session *model.SessionRecord) []runtimepolicy.TickTrace {
	if session == nil || session.ContextData == nil {
		return nil
	}
	raw := strings.TrimSpace(session.ContextData[model.CtxKeyRuntimePolicyTimeline])
	if raw == "" {
		return nil
	}
	var timeline []runtimepolicy.TickTrace
	if err := json.Unmarshal([]byte(raw), &timeline); err != nil {
		return nil
	}
	return timeline
}

func storeSessionRuntimeTimeline(session *model.SessionRecord, timeline []runtimepolicy.TickTrace) {
	if session == nil {
		return
	}
	if session.ContextData == nil {
		session.ContextData = make(map[string]string, 7)
	}
	if len(timeline) == 0 {
		delete(session.ContextData, model.CtxKeyRuntimePolicyTimeline)
		return
	}
	if payload, err := json.Marshal(timeline); err == nil {
		session.ContextData[model.CtxKeyRuntimePolicyTimeline] = string(payload)
	}
}

func loadSessionRuntimeReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
	if session == nil || session.ContextData == nil {
		return nil
	}
	raw := strings.TrimSpace(session.ContextData[model.CtxKeyRuntimePolicyReplay])
	if raw == "" {
		return nil
	}
	var replay runtimepolicy.RuntimePolicyReplay
	if err := json.Unmarshal([]byte(raw), &replay); err != nil {
		return nil
	}
	return &replay
}

func storeSessionRuntimeReplay(session *model.SessionRecord, replay *runtimepolicy.RuntimePolicyReplay) {
	if session == nil {
		return
	}
	if session.ContextData == nil {
		session.ContextData = make(map[string]string, 8)
	}
	if replay == nil {
		delete(session.ContextData, model.CtxKeyRuntimePolicyReplay)
		return
	}
	if payload, err := json.Marshal(replay); err == nil {
		session.ContextData[model.CtxKeyRuntimePolicyReplay] = string(payload)
	}
}

func buildSessionRuntimeTickTrace(input runtimepolicy.SessionLoopInput, state runtimepolicy.SessionLoopState, decision runtimepolicy.SessionLoopDecision, transition runtimepolicy.SessionTransition, execution sessionRuntimeTransitionResult, now time.Time) runtimepolicy.TickTrace {
	blockers := append([]string(nil), decision.Blockers...)
	blockers = append(blockers, execution.Blockers...)
	executedTransition := runtimepolicy.SessionTransitionNoOp
	if execution.Executed {
		executedTransition = transition.Kind
	}
	return runtimepolicy.TickTrace{
		TickAt:                now,
		ObservedStep:          input.ObservedStep,
		ConfidenceScore:       state.ConfidenceScore,
		ConfidenceState:       state.ConfidenceState,
		ConfidenceStateSince:  input.Confidence.StateSince,
		ConfidenceWindowCount: input.Confidence.WindowCount,
		PolicyAction:          decision.Action,
		PolicyConstraints:     append([]string(nil), input.Confidence.PolicyConstraints...),
		PlannedTransition:     transition.Kind,
		ExecutedTransition:    executedTransition,
		ActiveStep:            state.CurrentStep,
		TargetStep:            input.TargetStep,
		ProbeStep:             state.ProbeStep,
		ProbeState:            state.ProbeState,
		CooldownUntil:         state.CooldownUntil,
		RuntimePhase:          sessionRuntimePolicyPhaseName(state, now),
		Blockers:              blockers,
		Reasons:               append([]string(nil), state.Reasons...),
	}
}

func observedRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
	if session == nil {
		return runtimepolicy.PlaybackStepUnknown
	}
	trace := session.PlaybackTrace
	if trace == nil {
		trace = &model.PlaybackTrace{}
	}
	target := trace.TargetProfile
	if target == nil {
		target = model.TraceTargetProfileFromProfile(session.Profile)
	}
	return runtimepolicy.PlaybackLadderStepFromTargetProfile(target, sessionRuntimeQualityRung(trace))
}

func targetRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
	if session == nil || session.ContextData == nil {
		return observedRuntimeStep(session)
	}
	if target := runtimepolicy.NormalizePlaybackLadderStep(session.ContextData[model.CtxKeyRuntimeTargetStep]); target != runtimepolicy.PlaybackStepUnknown {
		return target
	}
	return observedRuntimeStep(session)
}

func sessionRuntimeQualityRung(trace *model.PlaybackTrace) playbackprofile.QualityRung {
	if trace == nil {
		return playbackprofile.RungUnknown
	}
	for _, candidate := range []string{
		trace.VideoQualityRung,
		trace.AudioQualityRung,
		trace.QualityRung,
	} {
		if rung := playbackprofile.NormalizeQualityRung(candidate); rung != playbackprofile.RungUnknown {
			return rung
		}
	}
	return playbackprofile.RungUnknown
}
