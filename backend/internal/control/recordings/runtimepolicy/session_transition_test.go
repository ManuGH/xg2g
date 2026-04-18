package runtimepolicy

import "testing"

func TestPlanSessionTransition_StepDownSchedulesRuntimeTransition(t *testing.T) {
	transition := PlanSessionTransition(
		SessionLoopState{CurrentStep: PlaybackStepH2641080p},
		SessionLoopState{CurrentStep: PlaybackStepH264720p},
		SessionLoopDecision{
			Action:      PolicyStepDown,
			CurrentStep: PlaybackStepH264720p,
			Reasons:     []string{ReasonBufferingRecent},
		},
	)

	if transition.Kind != SessionTransitionScheduleStepDown {
		t.Fatalf("expected schedule_step_down, got %q", transition.Kind)
	}
	if transition.FromStep != PlaybackStepH2641080p || transition.ToStep != PlaybackStepH264720p {
		t.Fatalf("unexpected transition steps: %#v", transition)
	}
	if len(transition.Reasons) != 1 || transition.Reasons[0] != ReasonBufferingRecent {
		t.Fatalf("expected transition reasons to carry through, got %#v", transition.Reasons)
	}
}

func TestPlanSessionTransition_NoOpWhenStepDidNotChange(t *testing.T) {
	transition := PlanSessionTransition(
		SessionLoopState{CurrentStep: PlaybackStepH264720p},
		SessionLoopState{CurrentStep: PlaybackStepH264720p},
		SessionLoopDecision{Action: PolicyStepDown},
	)

	if !transition.IsZero() {
		t.Fatalf("expected no-op transition, got %#v", transition)
	}
}

func TestPlanSessionTransition_ProbeUpSchedulesRuntimeTransition(t *testing.T) {
	transition := PlanSessionTransition(
		SessionLoopState{CurrentStep: PlaybackStepH264720p},
		SessionLoopState{CurrentStep: PlaybackStepH264720p, ProbeStep: PlaybackStepH2641080p, ProbeState: ProbeLifecycleScheduled},
		SessionLoopDecision{
			Action:     PolicyProbeUp,
			ProbeStep:  PlaybackStepH2641080p,
			ProbeState: ProbeLifecycleScheduled,
		},
	)

	if transition.Kind != SessionTransitionScheduleProbeUp {
		t.Fatalf("expected schedule_probe_up, got %q", transition.Kind)
	}
	if transition.FromStep != PlaybackStepH264720p || transition.ToStep != PlaybackStepH2641080p {
		t.Fatalf("unexpected transition steps: %#v", transition)
	}
}

func TestPlanSessionTransition_AbortProbeRevertsToStableStep(t *testing.T) {
	transition := PlanSessionTransition(
		SessionLoopState{CurrentStep: PlaybackStepH264720p, ProbeStep: PlaybackStepH2641080p, ProbeState: ProbeLifecycleObserving},
		SessionLoopState{CurrentStep: PlaybackStepH264720p, ProbeState: ProbeLifecycleAborted},
		SessionLoopDecision{Action: PolicyAbortProbe},
	)

	if transition.Kind != SessionTransitionRevertProbe {
		t.Fatalf("expected revert_probe, got %q", transition.Kind)
	}
	if transition.FromStep != PlaybackStepH2641080p || transition.ToStep != PlaybackStepH264720p {
		t.Fatalf("unexpected transition steps: %#v", transition)
	}
}
