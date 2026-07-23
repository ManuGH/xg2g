// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

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

// TickSessionRuntimePolicy evaluates and applies runtime policy changes on a tick.
func (s *Service) TickSessionRuntimePolicy(ctx context.Context, session *model.SessionRecord, now time.Time) {
	if s == nil || s.deps == nil || session == nil || session.SessionID == "" {
		return
	}
	store := s.deps.SessionStore()
	if store == nil {
		return
	}

	next, decision, input, changed := s.EvaluateSessionRuntimePolicy(ctx, session, now.UTC())
	if !changed {
		return
	}

	var (
		updated    *model.SessionRecord
		restart    bool
		transition runtimepolicy.SessionTransition
		execution  SessionRuntimeTransitionResult
	)
	record, err := store.UpdateSession(ctx, session.SessionID, func(rec *model.SessionRecord) error {
		prev := LoadSessionRuntimePolicyState(rec)
		if prev.CurrentStep == runtimepolicy.PlaybackStepUnknown {
			prev.CurrentStep = ObservedRuntimeStep(rec)
		}
		if prev.TargetStep == runtimepolicy.PlaybackStepUnknown {
			prev.TargetStep = TargetRuntimeStep(rec)
		}
		transition = runtimepolicy.PlanSessionTransition(prev, next, decision)
		StoreSessionRuntimePolicyState(rec, next)
		if !transition.IsZero() {
			applied, applyErr := ApplySessionRuntimePolicyTransition(rec, transition, now.UTC(), s.deps.ProfileResolver())
			if applyErr != nil {
				return applyErr
			}
			execution = applied
			restart = applied.Restart
		}
		timeline := runtimepolicy.AppendTickTrace(LoadSessionRuntimeTimeline(rec), BuildSessionRuntimeTickTrace(input, next, decision, transition, execution, now.UTC()))
		StoreSessionRuntimeTimeline(rec, timeline)
		StoreSessionRuntimeReplay(rec, BuildSessionRuntimePolicyReplay(rec))
		return nil
	})
	if err != nil {
		log.L().Warn().Err(err).Str("sessionId", session.SessionID).Msg("session runtime policy update failed")
		return
	}
	updated = record
	if restart && updated != nil {
		s.PublishSessionRuntimeTransition(ctx, updated, transition)
	}
}

// EvaluateSessionRuntimePolicy calculates next state and decision without writing to store.
func (s *Service) EvaluateSessionRuntimePolicy(ctx context.Context, session *model.SessionRecord, now time.Time) (runtimepolicy.SessionLoopState, runtimepolicy.SessionLoopDecision, runtimepolicy.SessionLoopInput, bool) {
	if s == nil || s.deps == nil || session == nil {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}

	registry := s.deps.CapabilityRegistry()
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

	prev := LoadSessionRuntimePolicyState(session)
	input := runtimepolicy.SessionLoopInput{
		ObservedStep:       ObservedRuntimeStep(session),
		TargetStep:         TargetRuntimeStep(session),
		Confidence:         policyState.Confidence,
		StartupWarmupUntil: SessionPlaybackStartupWarmupUntil(session, s.deps.Config().HLS.Root, SessionRuntimeStartupWarmup),
	}
	next, decision, changed := runtimepolicy.TickSessionLoop(prev, input, now)
	if !changed {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}
	return next, decision, input, true
}

// LoadSessionRuntimePolicyState unmarshals and normalizes the runtime state from ContextData.
func LoadSessionRuntimePolicyState(session *model.SessionRecord) runtimepolicy.SessionLoopState {
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

// StoreSessionRuntimePolicyState serializes state into ContextData.
func StoreSessionRuntimePolicyState(session *model.SessionRecord, state runtimepolicy.SessionLoopState) {
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

// LoadSessionRuntimeTimeline unmarshals tick trace slice from ContextData.
func LoadSessionRuntimeTimeline(session *model.SessionRecord) []runtimepolicy.TickTrace {
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

// StoreSessionRuntimeTimeline serializes tick trace slice into ContextData.
func StoreSessionRuntimeTimeline(session *model.SessionRecord, timeline []runtimepolicy.TickTrace) {
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

// LoadSessionRuntimeReplay unmarshals replay object from ContextData.
func LoadSessionRuntimeReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
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

// StoreSessionRuntimeReplay serializes replay object into ContextData.
func StoreSessionRuntimeReplay(session *model.SessionRecord, replay *runtimepolicy.RuntimePolicyReplay) {
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

// BuildSessionRuntimeTickTrace creates a single TickTrace entry.
func BuildSessionRuntimeTickTrace(input runtimepolicy.SessionLoopInput, state runtimepolicy.SessionLoopState, decision runtimepolicy.SessionLoopDecision, transition runtimepolicy.SessionTransition, execution SessionRuntimeTransitionResult, now time.Time) runtimepolicy.TickTrace {
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
		RuntimePhase:          SessionRuntimePolicyPhaseName(state, now),
		Blockers:              blockers,
		Reasons:               append([]string(nil), state.Reasons...),
	}
}

// ObservedRuntimeStep computes the active ladder step observed for a session.
func ObservedRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
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
	return runtimepolicy.PlaybackLadderStepFromTargetProfile(target, SessionRuntimeQualityRung(trace))
}

// TargetRuntimeStep computes the target ladder step from ContextData or fallback.
func TargetRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
	if session == nil || session.ContextData == nil {
		return ObservedRuntimeStep(session)
	}
	if target := runtimepolicy.NormalizePlaybackLadderStep(session.ContextData[model.CtxKeyRuntimeTargetStep]); target != runtimepolicy.PlaybackStepUnknown {
		return target
	}
	return ObservedRuntimeStep(session)
}

// SessionRuntimeQualityRung normalizes candidate quality rung fields from PlaybackTrace.
func SessionRuntimeQualityRung(trace *model.PlaybackTrace) playbackprofile.QualityRung {
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

// SessionRuntimePolicyPhaseName calculates the string description of the runtime phase.
func SessionRuntimePolicyPhaseName(state runtimepolicy.SessionLoopState, now time.Time) string {
	if state.ProbeState == runtimepolicy.ProbeLifecycleScheduled || state.ProbeState == runtimepolicy.ProbeLifecycleObserving {
		return "probing"
	}
	switch strings.TrimSpace(string(state.LastAction)) {
	case "probe_up":
		return "probing"
	case "cooldown":
		return "cooldown"
	case "degrade", "step_down":
		return "degraded"
	case "lock_current":
		return "recovering"
	}
	if hasString(state.Reasons, runtimepolicy.ReasonProbeRecentlyRegressed, runtimepolicy.ReasonProbeWindowRegressed) || state.ProbeState == runtimepolicy.ProbeLifecycleAborted {
		return "probe_regressed"
	}
	if !state.CooldownUntil.IsZero() && state.CooldownUntil.After(now) {
		return "cooldown"
	}
	switch state.ConfidenceState {
	case runtimepolicy.ConfidenceRecovery:
		return "recovering"
	case runtimepolicy.ConfidenceLow:
		return "degraded"
	case runtimepolicy.ConfidenceStable, runtimepolicy.ConfidenceHigh:
		return "stable"
	default:
		return ""
	}
}

func hasString(values []string, targets ...string) bool {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, target := range targets {
			if value == strings.TrimSpace(target) {
				return true
			}
		}
	}
	return false
}
