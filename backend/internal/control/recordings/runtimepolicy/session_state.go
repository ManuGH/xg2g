package runtimepolicy

import (
	"strings"
	"time"
)

func NormalizeSessionLoopState(in SessionLoopState) SessionLoopState {
	in.CurrentStep = NormalizePlaybackLadderStep(string(in.CurrentStep))
	in.TargetStep = NormalizePlaybackLadderStep(string(in.TargetStep))
	in.ProbeStep = NormalizePlaybackLadderStep(string(in.ProbeStep))
	in.ProbeState = normalizeProbeLifecycleState(in.ProbeState)
	if in.ProbeStep == PlaybackStepUnknown {
		in.ProbeStartedAt = time.Time{}
		in.ProbeObservedAt = time.Time{}
		if in.ProbeState == ProbeLifecycleScheduled || in.ProbeState == ProbeLifecycleObserving {
			in.ProbeState = ProbeLifecycleNone
		}
	}
	in.LastAction = normalizePolicyAction(in.LastAction)
	in.PolicyConstraints = sortedKeys(sliceToSet(in.PolicyConstraints))
	in.Reasons = sortedKeys(sliceToSet(in.Reasons))
	return in
}

func normalizePolicyAction(action PolicyAction) PolicyAction {
	switch action {
	case PolicyHold,
		PolicyDegrade,
		PolicyStepDown,
		PolicyProbeUp,
		PolicyConfirmProbe,
		PolicyAbortProbe,
		PolicyLockCurrent,
		PolicyCooldown:
		return action
	default:
		return ""
	}
}

func normalizeProbeLifecycleState(state ProbeLifecycleState) ProbeLifecycleState {
	switch state {
	case ProbeLifecycleNone,
		ProbeLifecycleScheduled,
		ProbeLifecycleObserving,
		ProbeLifecycleConfirmed,
		ProbeLifecycleAborted:
		return state
	default:
		return ProbeLifecycleNone
	}
}

func sliceToSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}
