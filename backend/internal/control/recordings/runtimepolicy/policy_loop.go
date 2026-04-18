package runtimepolicy

import "time"

const sessionLoopMinimumInterval = 2 * time.Second
const (
	sessionLoopProbeScheduleTimeout   = 8 * time.Second
	sessionLoopProbeObservationWindow = 12 * time.Second
)

func TickSessionLoop(prev SessionLoopState, input SessionLoopInput, now time.Time) (SessionLoopState, SessionLoopDecision, bool) {
	prev = NormalizeSessionLoopState(prev)
	decision := SessionLoopDecision{
		Action:            PolicyHold,
		CurrentStep:       prev.CurrentStep,
		TargetStep:        prev.TargetStep,
		ProbeStep:         prev.ProbeStep,
		ProbeState:        prev.ProbeState,
		Blockers:          nil,
		PolicyConstraints: append([]string(nil), prev.PolicyConstraints...),
		Reasons:           append([]string(nil), prev.Reasons...),
	}

	if !prev.LastTickAt.IsZero() && now.Sub(prev.LastTickAt) < sessionLoopMinimumInterval {
		return prev, decision, false
	}

	state := prev
	observed := NormalizePlaybackLadderStep(string(input.ObservedStep))
	target := NormalizePlaybackLadderStep(string(input.TargetStep))
	confidence := input.Confidence

	if observed != PlaybackStepUnknown && (state.CurrentStep == PlaybackStepUnknown || playbackLadderIndex(observed) < playbackLadderIndex(state.CurrentStep)) {
		state.CurrentStep = observed
	}
	if state.CurrentStep == PlaybackStepUnknown {
		state.CurrentStep = observed
	}
	if target != PlaybackStepUnknown {
		state.TargetStep = target
	}
	if state.TargetStep == PlaybackStepUnknown {
		state.TargetStep = state.CurrentStep
	}

	state.ConfidenceScore = confidence.Score
	state.ConfidenceState = confidence.State
	state.CooldownUntil = maxTime(state.CooldownUntil, confidence.CooldownUntil)
	state.PolicyConstraints = append([]string(nil), confidence.PolicyConstraints...)
	state.Reasons = append([]string(nil), confidence.Reasons...)
	state.LastTickAt = now

	decision.TargetStep = state.TargetStep
	decision.ProbeState = state.ProbeState
	decision.PolicyConstraints = append([]string(nil), state.PolicyConstraints...)
	decision.Reasons = append([]string(nil), state.Reasons...)

	if state.ProbeStep != PlaybackStepUnknown {
		if state.ProbeState == ProbeLifecycleNone {
			state.ProbeState = ProbeLifecycleScheduled
		}
		if state.ProbeStartedAt.IsZero() {
			state.ProbeStartedAt = now
		}
		if state.ProbeState == ProbeLifecycleScheduled && observed == state.ProbeStep {
			state.ProbeState = ProbeLifecycleObserving
			if state.ProbeObservedAt.IsZero() {
				state.ProbeObservedAt = now
			}
		}

		switch {
		case shouldAbortSessionProbe(state, confidence, now):
			state.ProbeState = ProbeLifecycleAborted
			state.ProbeStep = PlaybackStepUnknown
			state.ProbeStartedAt = time.Time{}
			state.ProbeObservedAt = time.Time{}
			state.CooldownUntil = maxTime(state.CooldownUntil, now.Add(cooldownProbeUp))
			state.LastAction = PolicyAbortProbe
			decision.Action = PolicyAbortProbe
		case shouldConfirmSessionProbe(state, confidence, now):
			state.CurrentStep = state.ProbeStep
			state.ProbeState = ProbeLifecycleConfirmed
			state.ProbeStep = PlaybackStepUnknown
			state.ProbeStartedAt = time.Time{}
			state.ProbeObservedAt = time.Time{}
			state.LastAction = PolicyConfirmProbe
			decision.Action = PolicyConfirmProbe
		default:
			state.LastAction = PolicyHold
			decision.ProbeStep = state.ProbeStep
			decision.ProbeState = state.ProbeState
			decision.CurrentStep = state.CurrentStep
			decision.Blockers = sessionLoopProbeBlockers(state)
			return state, decision, !sessionLoopStateEqual(prev, state)
		}

		decision.CurrentStep = state.CurrentStep
		decision.ProbeStep = state.ProbeStep
		decision.ProbeState = state.ProbeState
		decision.Blockers = nil
		decision.PolicyConstraints = append([]string(nil), state.PolicyConstraints...)
		return state, decision, !sessionLoopStateEqual(prev, state)
	}

	if state.CooldownUntil.After(now) {
		state.LastAction = PolicyCooldown
		decision.Action = PolicyCooldown
		decision.CurrentStep = state.CurrentStep
		decision.Blockers = []string{BlockerCooldownActive}
		return state, decision, !sessionLoopStateEqual(prev, state)
	}

	if confidence.State == ConfidenceLow {
		if next, ok := PlaybackLadderNextDown(state.CurrentStep); ok {
			state.CurrentStep = next
			state.CooldownUntil = maxTime(state.CooldownUntil, now.Add(cooldownDegrade))
			state.LastAction = PolicyStepDown
			decision.Action = PolicyStepDown
			decision.CurrentStep = state.CurrentStep
			decision.Blockers = nil
			return state, decision, !sessionLoopStateEqual(prev, state)
		}
		decision.Blockers = []string{BlockerAlreadyAtLowestStep}
	}

	if probeBlockers := sessionLoopProbeUpBlockers(state, confidence, now); len(probeBlockers) == 0 {
		if next, ok := PlaybackLadderNextUpTowards(state.CurrentStep, state.TargetStep); ok {
			state.ProbeStep = next
			state.ProbeState = ProbeLifecycleScheduled
			state.ProbeStartedAt = now
			state.ProbeObservedAt = time.Time{}
			state.CooldownUntil = maxTime(state.CooldownUntil, now.Add(cooldownProbeUp))
			state.LastAction = PolicyProbeUp
			decision.Action = PolicyProbeUp
			decision.CurrentStep = state.CurrentStep
			decision.ProbeStep = state.ProbeStep
			decision.ProbeState = state.ProbeState
			decision.Blockers = nil
			return state, decision, !sessionLoopStateEqual(prev, state)
		}
		decision.Blockers = []string{BlockerAlreadyAtTarget}
	} else if len(decision.Blockers) == 0 {
		decision.Blockers = probeBlockers
	}

	state.LastAction = PolicyHold
	decision.CurrentStep = state.CurrentStep
	if len(decision.Blockers) == 0 {
		decision.Blockers = sessionLoopHoldBlockers(state, confidence, now)
	}
	return state, decision, !sessionLoopStateEqual(prev, state)
}

func sessionLoopStateEqual(a, b SessionLoopState) bool {
	if a.CurrentStep != b.CurrentStep ||
		a.TargetStep != b.TargetStep ||
		a.ProbeStep != b.ProbeStep ||
		a.ProbeState != b.ProbeState ||
		!a.ProbeStartedAt.Equal(b.ProbeStartedAt) ||
		!a.ProbeObservedAt.Equal(b.ProbeObservedAt) ||
		a.ConfidenceScore != b.ConfidenceScore ||
		a.ConfidenceState != b.ConfidenceState ||
		!a.CooldownUntil.Equal(b.CooldownUntil) ||
		!a.LastTickAt.Equal(b.LastTickAt) ||
		a.LastAction != b.LastAction {
		return false
	}
	if !sameStrings(a.PolicyConstraints, b.PolicyConstraints) {
		return false
	}
	return sameStrings(a.Reasons, b.Reasons)
}

func shouldAbortSessionProbe(state SessionLoopState, confidence ConfidenceSnapshot, now time.Time) bool {
	if hasString(confidence.Reasons, ReasonProbeWindowRegressed) || hasString(confidence.Reasons, ReasonProbeRecentlyRegressed) {
		return true
	}
	if confidence.State == ConfidenceLow {
		return true
	}
	if hasString(confidence.Reasons, ReasonDecodeRiskHigh) || hasString(confidence.Reasons, ReasonDecodeWarningRecent) || hasString(confidence.Reasons, ReasonStallRecent) {
		return true
	}
	if state.ProbeState == ProbeLifecycleScheduled && !state.ProbeStartedAt.IsZero() && now.Sub(state.ProbeStartedAt) >= sessionLoopProbeScheduleTimeout {
		return true
	}
	if state.ProbeState == ProbeLifecycleObserving && !state.ProbeObservedAt.IsZero() &&
		now.Sub(state.ProbeObservedAt) >= sessionLoopProbeObservationWindow &&
		(hasString(confidence.Reasons, ReasonBufferingRecent) || hasString(confidence.Reasons, ReasonNetworkRecentlyUnstable)) {
		return true
	}
	return false
}

func shouldConfirmSessionProbe(state SessionLoopState, confidence ConfidenceSnapshot, now time.Time) bool {
	if hasString(confidence.Reasons, ReasonProbeWindowConfirmed) || hasString(confidence.Reasons, ReasonProbeRecentlyConfirmed) {
		return true
	}
	if state.ProbeState != ProbeLifecycleObserving || state.ProbeObservedAt.IsZero() {
		return false
	}
	if now.Sub(state.ProbeObservedAt) < sessionLoopProbeObservationWindow {
		return false
	}
	if confidence.State == ConfidenceLow {
		return false
	}
	if hasString(confidence.Reasons, ReasonBufferingRecent) || hasString(confidence.Reasons, ReasonNetworkRecentlyUnstable) || hasString(confidence.Reasons, ReasonDecodeWarningRecent) || hasString(confidence.Reasons, ReasonStallRecent) || hasString(confidence.Reasons, ReasonDecodeRiskHigh) {
		return false
	}
	return true
}

func sessionLoopProbeBlockers(state SessionLoopState) []string {
	switch state.ProbeState {
	case ProbeLifecycleScheduled:
		return []string{BlockerProbeScheduled}
	case ProbeLifecycleObserving:
		return []string{BlockerProbeObserving}
	default:
		return nil
	}
}

func sessionLoopProbeUpBlockers(state SessionLoopState, confidence ConfidenceSnapshot, now time.Time) []string {
	switch {
	case confidence.State != ConfidenceHigh:
		return []string{BlockerInsufficientConfidence}
	case state.CurrentStep == PlaybackStepUnknown || state.TargetStep == PlaybackStepUnknown:
		return []string{BlockerInsufficientConfidence}
	case playbackLadderIndex(state.CurrentStep) >= playbackLadderIndex(state.TargetStep):
		return []string{BlockerAlreadyAtTarget}
	case state.CooldownUntil.After(now):
		return []string{BlockerCooldownActive}
	case hasString(confidence.PolicyConstraints, ConstraintNoProbeUp), hasString(confidence.PolicyConstraints, ConstraintCooldownActive):
		return []string{BlockerNoProbeUp}
	case confidence.Score < 50 || confidence.WindowCount < 3:
		return []string{BlockerInsufficientConfidence}
	case confidence.StateSince.IsZero() || now.Sub(confidence.StateSince) < holdHighBeforeProbeUp:
		return []string{BlockerInsufficientConfidence}
	default:
		return nil
	}
}

func sessionLoopHoldBlockers(state SessionLoopState, confidence ConfidenceSnapshot, now time.Time) []string {
	if blockers := sessionLoopProbeBlockers(state); len(blockers) > 0 {
		return blockers
	}
	if state.CooldownUntil.After(now) {
		return []string{BlockerCooldownActive}
	}
	if confidence.State == ConfidenceLow {
		if _, ok := PlaybackLadderNextDown(state.CurrentStep); !ok {
			return []string{BlockerAlreadyAtLowestStep}
		}
	}
	return sessionLoopProbeUpBlockers(state, confidence, now)
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
