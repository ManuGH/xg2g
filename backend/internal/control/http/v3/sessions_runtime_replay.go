package v3

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/control/recordings/runtimepolicy"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func buildSessionRuntimePolicyReplay(session *model.SessionRecord) *runtimepolicy.RuntimePolicyReplay {
	if session == nil {
		return nil
	}
	timeline := loadSessionRuntimeTimeline(session)
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
		ClientPath:    sessionContextValue(session, model.CtxKeyClientPath),
		SourceType:    sessionContextValue(session, model.CtxKeySourceType),
		InitialTarget: first.TargetStep,
	}, initial, timeline)
	return &replay
}

func sessionContextValue(session *model.SessionRecord, key string) string {
	if session == nil || session.ContextData == nil {
		return ""
	}
	return strings.TrimSpace(session.ContextData[key])
}
