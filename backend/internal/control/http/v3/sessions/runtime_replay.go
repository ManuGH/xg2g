// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// BuildSessionRuntimePolicyReplay constructs a RuntimePolicyReplay from a session's timeline.
func BuildSessionRuntimePolicyReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
	if session == nil {
		return nil
	}
	timeline := LoadSessionRuntimeTimeline(session)
	if len(timeline) == 0 {
		return nil
	}
	first := timeline[0]
	initial := runtimepolicy.NormalizeSessionLoopState(runtimepolicy.SessionLoopState{
		CurrentStep:       first.ObservedStep,
		TargetStep:        first.TargetStep,
		ConfidenceScore:   first.ConfidenceScore,
		ConfidenceState:   first.ConfidenceState,
		CooldownUntil:     first.CooldownUntil,
		PolicyConstraints: append([]string(nil), first.PolicyConstraints...),
		Reasons:           append([]string(nil), first.Reasons...),
	})
	replay := runtimepolicy.ReplayFromTimeline(runtimepolicy.ReplayMetadata{
		SessionID:     strings.TrimSpace(session.SessionID),
		ServiceRef:    strings.TrimSpace(session.ServiceRef),
		ClientPath:    SessionContextValue(session, model.CtxKeyClientPath),
		SourceType:    SessionContextValue(session, model.CtxKeySourceType),
		InitialTarget: first.TargetStep,
	}, initial, timeline)
	return &replay
}

// SessionContextValue retrieves a trimmed string from session.ContextData safely.
func SessionContextValue(session *model.SessionRecord, key string) string {
	if session == nil || session.ContextData == nil {
		return ""
	}
	return strings.TrimSpace(session.ContextData[key])
}
