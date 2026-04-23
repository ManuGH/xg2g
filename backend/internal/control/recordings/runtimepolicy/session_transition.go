package runtimepolicy

func PlanSessionTransition(prev, next SessionLoopState, decision SessionLoopDecision) SessionTransition {
	prev = NormalizeSessionLoopState(prev)
	next = NormalizeSessionLoopState(next)

	switch decision.Action {
	case PolicyStepDown:
		fromStep := NormalizePlaybackLadderStep(string(prev.CurrentStep))
		toStep := NormalizePlaybackLadderStep(string(next.CurrentStep))
		if fromStep == PlaybackStepUnknown || toStep == PlaybackStepUnknown || fromStep == toStep {
			return SessionTransition{}
		}
		return SessionTransition{
			Kind:     SessionTransitionScheduleStepDown,
			Action:   decision.Action,
			FromStep: fromStep,
			ToStep:   toStep,
			Reasons:  append([]string(nil), decision.Reasons...),
		}
	case PolicyProbeUp:
		fromStep := NormalizePlaybackLadderStep(string(prev.CurrentStep))
		toStep := NormalizePlaybackLadderStep(string(next.ProbeStep))
		if fromStep == PlaybackStepUnknown || toStep == PlaybackStepUnknown || fromStep == toStep {
			return SessionTransition{}
		}
		return SessionTransition{
			Kind:     SessionTransitionScheduleProbeUp,
			Action:   decision.Action,
			FromStep: fromStep,
			ToStep:   toStep,
			Reasons:  append([]string(nil), decision.Reasons...),
		}
	case PolicyConfirmProbe:
		if prev.ProbeStep == PlaybackStepUnknown {
			return SessionTransition{}
		}
		return SessionTransition{
			Kind:     SessionTransitionCommitProbe,
			Action:   decision.Action,
			FromStep: prev.CurrentStep,
			ToStep:   next.CurrentStep,
			Reasons:  append([]string(nil), decision.Reasons...),
		}
	case PolicyAbortProbe:
		if prev.ProbeStep == PlaybackStepUnknown {
			return SessionTransition{}
		}
		return SessionTransition{
			Kind:     SessionTransitionRevertProbe,
			Action:   decision.Action,
			FromStep: prev.ProbeStep,
			ToStep:   next.CurrentStep,
			Reasons:  append([]string(nil), decision.Reasons...),
		}
	default:
		return SessionTransition{}
	}
}
