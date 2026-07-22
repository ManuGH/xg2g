// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/sessions"
	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func (s *Server) tickSessionRuntimePolicy(ctx context.Context, store SessionStateStore, session *model.SessionRecord, now time.Time) {
	if s == nil {
		return
	}
	s.sessionsProcessor().TickSessionRuntimePolicy(ctx, session, now)
}

func (s *Server) evaluateSessionRuntimePolicy(ctx context.Context, session *model.SessionRecord, now time.Time) (runtimepolicy.SessionLoopState, runtimepolicy.SessionLoopDecision, runtimepolicy.SessionLoopInput, bool) {
	if s == nil {
		return runtimepolicy.SessionLoopState{}, runtimepolicy.SessionLoopDecision{}, runtimepolicy.SessionLoopInput{}, false
	}
	return s.sessionsProcessor().EvaluateSessionRuntimePolicy(ctx, session, now)
}

func loadSessionRuntimePolicyState(session *model.SessionRecord) runtimepolicy.SessionLoopState {
	return sessions.LoadSessionRuntimePolicyState(session)
}

func storeSessionRuntimePolicyState(session *model.SessionRecord, state runtimepolicy.SessionLoopState) {
	sessions.StoreSessionRuntimePolicyState(session, state)
}

func loadSessionRuntimeTimeline(session *model.SessionRecord) []runtimepolicy.TickTrace {
	return sessions.LoadSessionRuntimeTimeline(session)
}

func storeSessionRuntimeTimeline(session *model.SessionRecord, timeline []runtimepolicy.TickTrace) {
	sessions.StoreSessionRuntimeTimeline(session, timeline)
}

func loadSessionRuntimeReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
	return sessions.LoadSessionRuntimeReplay(session)
}

func storeSessionRuntimeReplay(session *model.SessionRecord, replay *runtimepolicy.RuntimePolicyReplay) {
	sessions.StoreSessionRuntimeReplay(session, replay)
}

func buildSessionRuntimeTickTrace(input runtimepolicy.SessionLoopInput, state runtimepolicy.SessionLoopState, decision runtimepolicy.SessionLoopDecision, transition runtimepolicy.SessionTransition, execution sessions.SessionRuntimeTransitionResult, now time.Time) runtimepolicy.TickTrace {
	return sessions.BuildSessionRuntimeTickTrace(input, state, decision, transition, execution, now)
}

func observedRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
	return sessions.ObservedRuntimeStep(session)
}

func targetRuntimeStep(session *model.SessionRecord) runtimepolicy.PlaybackLadderStep {
	return sessions.TargetRuntimeStep(session)
}

func sessionRuntimeQualityRung(trace *model.PlaybackTrace) playbackprofile.QualityRung {
	return sessions.SessionRuntimeQualityRung(trace)
}
