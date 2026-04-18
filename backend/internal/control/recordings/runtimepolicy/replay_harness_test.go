package runtimepolicy

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestPlaybackLadderStepFromTargetProfile(t *testing.T) {
	if got := PlaybackLadderStepFromTargetProfile(&playbackprofile.TargetPlaybackProfile{
		Video: playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeCopy},
		Audio: playbackprofile.AudioTarget{Mode: playbackprofile.MediaModeCopy},
	}, playbackprofile.RungDirectCopy); got != PlaybackStepDirectCopy {
		t.Fatalf("expected direct copy step, got %q", got)
	}

	if got := PlaybackLadderStepFromTargetProfile(&playbackprofile.TargetPlaybackProfile{
		Video: playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeCopy},
		Audio: playbackprofile.AudioTarget{Mode: playbackprofile.MediaModeTranscode, Codec: "aac"},
	}, playbackprofile.RungCompatibleAudioAAC256Stereo); got != PlaybackStepVideoCopyAudioAAC {
		t.Fatalf("expected audio-only step, got %q", got)
	}

	if got := PlaybackLadderStepFromTargetProfile(&playbackprofile.TargetPlaybackProfile{
		Video: playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeTranscode, Width: 1280, Codec: "h264", CRF: 23, Preset: "fast"},
		Audio: playbackprofile.AudioTarget{Mode: playbackprofile.MediaModeTranscode, Codec: "aac"},
	}, playbackprofile.RungCompatibleVideoH264CRF23); got != PlaybackStepH264720p {
		t.Fatalf("expected 720p step, got %q", got)
	}

	if got := PlaybackLadderStepFromTargetProfile(&playbackprofile.TargetPlaybackProfile{
		Video: playbackprofile.VideoTarget{Mode: playbackprofile.MediaModeTranscode, Width: 1280, Codec: "h264", CRF: 28, Preset: "veryfast"},
		Audio: playbackprofile.AudioTarget{Mode: playbackprofile.MediaModeTranscode, Codec: "aac"},
	}, playbackprofile.RungRepairVideoH264CRF28); got != PlaybackStepRepairLow {
		t.Fatalf("expected repair step, got %q", got)
	}
}

func TestTickSessionLoop_StepsDownUnderLowConfidence(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	state, decision, changed := TickSessionLoop(SessionLoopState{
		CurrentStep: PlaybackStepH2641080p,
		TargetStep:  PlaybackStepH2641080p,
	}, SessionLoopInput{
		ObservedStep: PlaybackStepH2641080p,
		TargetStep:   PlaybackStepH2641080p,
		Confidence: ConfidenceSnapshot{
			Score: -42,
			State: ConfidenceLow,
		},
	}, now)

	if !changed {
		t.Fatalf("expected session loop state to change")
	}
	if decision.Action != PolicyStepDown {
		t.Fatalf("expected step_down action, got %q", decision.Action)
	}
	if state.CurrentStep != PlaybackStepH264720p {
		t.Fatalf("expected current step to degrade to 720p, got %q", state.CurrentStep)
	}
}

func TestTickSessionLoop_ProbesOneStepTowardTarget(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 1, 0, 0, time.UTC)
	state, decision, changed := TickSessionLoop(SessionLoopState{
		CurrentStep: PlaybackStepH264720p,
		TargetStep:  PlaybackStepDirectCopy,
	}, SessionLoopInput{
		ObservedStep: PlaybackStepH264720p,
		TargetStep:   PlaybackStepDirectCopy,
		Confidence: ConfidenceSnapshot{
			Score:       68,
			State:       ConfidenceHigh,
			StateSince:  now.Add(-15 * time.Second),
			WindowCount: 4,
		},
	}, now)

	if !changed {
		t.Fatalf("expected session loop state to change")
	}
	if decision.Action != PolicyProbeUp {
		t.Fatalf("expected probe_up action, got %q", decision.Action)
	}
	if state.ProbeStep != PlaybackStepH2641080p {
		t.Fatalf("expected first probe step to move one rung up, got %q", state.ProbeStep)
	}
	if state.ProbeState != ProbeLifecycleScheduled {
		t.Fatalf("expected scheduled probe lifecycle, got %q", state.ProbeState)
	}
	if state.ProbeStartedAt != now {
		t.Fatalf("expected probe start timestamp, got %v", state.ProbeStartedAt)
	}
}

func TestTickSessionLoop_ObservedProbeTransitionsToObserving(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 2, 0, 0, time.UTC)
	state, decision, changed := TickSessionLoop(SessionLoopState{
		CurrentStep:    PlaybackStepH264720p,
		TargetStep:     PlaybackStepDirectCopy,
		ProbeStep:      PlaybackStepH2641080p,
		ProbeState:     ProbeLifecycleScheduled,
		ProbeStartedAt: now.Add(-3 * time.Second),
		LastTickAt:     now.Add(-5 * time.Second),
	}, SessionLoopInput{
		ObservedStep: PlaybackStepH2641080p,
		TargetStep:   PlaybackStepDirectCopy,
		Confidence: ConfidenceSnapshot{
			Score:       68,
			State:       ConfidenceHigh,
			StateSince:  now.Add(-20 * time.Second),
			WindowCount: 5,
		},
	}, now)

	if !changed {
		t.Fatalf("expected session loop state to change")
	}
	if decision.Action != PolicyHold {
		t.Fatalf("expected hold action while observing, got %q", decision.Action)
	}
	if state.ProbeState != ProbeLifecycleObserving {
		t.Fatalf("expected observing probe lifecycle, got %q", state.ProbeState)
	}
	if state.ProbeObservedAt != now {
		t.Fatalf("expected probe observed timestamp, got %v", state.ProbeObservedAt)
	}
}

func TestTickSessionLoop_ConfirmsObservedProbe(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 2, 15, 0, time.UTC)
	state, decision, changed := TickSessionLoop(SessionLoopState{
		CurrentStep:     PlaybackStepH264720p,
		TargetStep:      PlaybackStepDirectCopy,
		ProbeStep:       PlaybackStepH2641080p,
		ProbeState:      ProbeLifecycleObserving,
		ProbeStartedAt:  now.Add(-15 * time.Second),
		ProbeObservedAt: now.Add(-12 * time.Second),
		LastTickAt:      now.Add(-5 * time.Second),
	}, SessionLoopInput{
		ObservedStep: PlaybackStepH2641080p,
		TargetStep:   PlaybackStepDirectCopy,
		Confidence: ConfidenceSnapshot{
			Score:       72,
			State:       ConfidenceHigh,
			StateSince:  now.Add(-20 * time.Second),
			WindowCount: 5,
			Reasons:     []string{ReasonProbeWindowConfirmed},
		},
	}, now)

	if !changed {
		t.Fatalf("expected session loop state to change")
	}
	if decision.Action != PolicyConfirmProbe {
		t.Fatalf("expected confirm_probe action, got %q", decision.Action)
	}
	if state.CurrentStep != PlaybackStepH2641080p {
		t.Fatalf("expected current step to advance to confirmed probe, got %q", state.CurrentStep)
	}
	if state.ProbeStep != PlaybackStepUnknown {
		t.Fatalf("expected probe step to clear after confirmation, got %q", state.ProbeStep)
	}
	if state.ProbeState != ProbeLifecycleConfirmed {
		t.Fatalf("expected confirmed probe lifecycle, got %q", state.ProbeState)
	}
}

func TestTickSessionLoop_AbortsObservedProbeOnRegression(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 2, 20, 0, time.UTC)
	state, decision, changed := TickSessionLoop(SessionLoopState{
		CurrentStep:     PlaybackStepH264720p,
		TargetStep:      PlaybackStepDirectCopy,
		ProbeStep:       PlaybackStepH2641080p,
		ProbeState:      ProbeLifecycleObserving,
		ProbeStartedAt:  now.Add(-10 * time.Second),
		ProbeObservedAt: now.Add(-6 * time.Second),
		LastTickAt:      now.Add(-5 * time.Second),
	}, SessionLoopInput{
		ObservedStep: PlaybackStepH2641080p,
		TargetStep:   PlaybackStepDirectCopy,
		Confidence: ConfidenceSnapshot{
			Score:       20,
			State:       ConfidenceRecovery,
			StateSince:  now.Add(-20 * time.Second),
			WindowCount: 5,
			Reasons:     []string{ReasonProbeWindowRegressed},
		},
	}, now)

	if !changed {
		t.Fatalf("expected session loop state to change")
	}
	if decision.Action != PolicyAbortProbe {
		t.Fatalf("expected abort_probe action, got %q", decision.Action)
	}
	if state.CurrentStep != PlaybackStepH264720p {
		t.Fatalf("expected current step to stay on stable rung, got %q", state.CurrentStep)
	}
	if state.ProbeStep != PlaybackStepUnknown {
		t.Fatalf("expected probe step to clear after abort, got %q", state.ProbeStep)
	}
	if state.ProbeState != ProbeLifecycleAborted {
		t.Fatalf("expected aborted probe lifecycle, got %q", state.ProbeState)
	}
}

func TestRunReplay_ReplaysStepDownTimeline(t *testing.T) {
	now := time.Date(2026, 4, 18, 13, 0, 0, 0, time.UTC)
	replay := ReplayFromTimeline(ReplayMetadata{
		SessionID:     "sid-stepdown",
		ServiceRef:    "1:0:1:1:2:3:4:0:0:0:",
		ClientPath:    "hlsjs",
		SourceType:    "tuner",
		InitialTarget: PlaybackStepH2641080p,
	}, SessionLoopState{
		CurrentStep: PlaybackStepH2641080p,
		TargetStep:  PlaybackStepH2641080p,
	}, []TickTrace{{
		TickAt:                now,
		ObservedStep:          PlaybackStepH2641080p,
		ConfidenceScore:       -44,
		ConfidenceState:       ConfidenceLow,
		ConfidenceWindowCount: 1,
		PolicyAction:          PolicyStepDown,
		PlannedTransition:     SessionTransitionScheduleStepDown,
		ExecutedTransition:    SessionTransitionScheduleStepDown,
		ActiveStep:            PlaybackStepH264720p,
		TargetStep:            PlaybackStepH2641080p,
		Reasons:               []string{ReasonBufferingRecent},
	}})

	result := RunReplay(replay)
	if len(result.Ticks) != 1 {
		t.Fatalf("expected 1 replay tick, got %d", len(result.Ticks))
	}
	if mismatches := CompareReplayExpectations(replay, result); len(mismatches) > 0 {
		t.Fatalf("expected replay to match timeline, got mismatches %#v", mismatches)
	}
	if result.Ticks[0].Decision.Action != PolicyStepDown {
		t.Fatalf("expected step_down action, got %q", result.Ticks[0].Decision.Action)
	}
	if result.Ticks[0].Transition.Kind != SessionTransitionScheduleStepDown {
		t.Fatalf("expected schedule_step_down transition, got %q", result.Ticks[0].Transition.Kind)
	}
	if result.FinalState.CurrentStep != PlaybackStepH264720p {
		t.Fatalf("expected final step 720p, got %q", result.FinalState.CurrentStep)
	}
}

func TestRunReplay_ReplaysProbeConfirmTimeline(t *testing.T) {
	now := time.Date(2026, 4, 18, 13, 10, 0, 0, time.UTC)
	replay := ReplayFromTimeline(ReplayMetadata{
		SessionID:     "sid-probe",
		ServiceRef:    "1:0:1:5:6:7:8:0:0:0:",
		ClientPath:    "native",
		SourceType:    "tuner",
		InitialTarget: PlaybackStepDirectCopy,
	}, SessionLoopState{
		CurrentStep: PlaybackStepH264720p,
		TargetStep:  PlaybackStepDirectCopy,
	}, []TickTrace{
		{
			TickAt:                now,
			ObservedStep:          PlaybackStepH264720p,
			ConfidenceScore:       72,
			ConfidenceState:       ConfidenceHigh,
			ConfidenceStateSince:  now.Add(-20 * time.Second),
			ConfidenceWindowCount: 4,
			PolicyAction:          PolicyProbeUp,
			PlannedTransition:     SessionTransitionScheduleProbeUp,
			ExecutedTransition:    SessionTransitionScheduleProbeUp,
			ActiveStep:            PlaybackStepH264720p,
			TargetStep:            PlaybackStepDirectCopy,
			ProbeStep:             PlaybackStepH2641080p,
			ProbeState:            ProbeLifecycleScheduled,
			Reasons:               []string{ReasonHeadroomGood, ReasonCleanPlaybackWindow},
		},
		{
			TickAt:                now.Add(5 * time.Second),
			ObservedStep:          PlaybackStepH2641080p,
			ConfidenceScore:       72,
			ConfidenceState:       ConfidenceHigh,
			ConfidenceStateSince:  now.Add(-25 * time.Second),
			ConfidenceWindowCount: 5,
			PolicyAction:          PolicyHold,
			ActiveStep:            PlaybackStepH264720p,
			TargetStep:            PlaybackStepDirectCopy,
			ProbeStep:             PlaybackStepH2641080p,
			ProbeState:            ProbeLifecycleObserving,
			Blockers:              []string{BlockerProbeObserving},
			Reasons:               []string{ReasonHeadroomGood},
		},
		{
			TickAt:                now.Add(17 * time.Second),
			ObservedStep:          PlaybackStepH2641080p,
			ConfidenceScore:       75,
			ConfidenceState:       ConfidenceHigh,
			ConfidenceStateSince:  now.Add(-37 * time.Second),
			ConfidenceWindowCount: 6,
			PolicyAction:          PolicyConfirmProbe,
			PlannedTransition:     SessionTransitionCommitProbe,
			ExecutedTransition:    SessionTransitionCommitProbe,
			ActiveStep:            PlaybackStepH2641080p,
			TargetStep:            PlaybackStepDirectCopy,
			ProbeState:            ProbeLifecycleConfirmed,
			Reasons:               []string{ReasonProbeWindowConfirmed},
		},
	})

	result := RunReplay(replay)
	if len(result.Ticks) != 3 {
		t.Fatalf("expected 3 replay ticks, got %d", len(result.Ticks))
	}
	if mismatches := CompareReplayExpectations(replay, result); len(mismatches) > 0 {
		t.Fatalf("expected replay to match timeline, got mismatches %#v", mismatches)
	}
	if result.Ticks[0].Decision.Action != PolicyProbeUp {
		t.Fatalf("expected first action probe_up, got %q", result.Ticks[0].Decision.Action)
	}
	if result.Ticks[1].Decision.Action != PolicyHold {
		t.Fatalf("expected second action hold, got %q", result.Ticks[1].Decision.Action)
	}
	if result.Ticks[2].Decision.Action != PolicyConfirmProbe {
		t.Fatalf("expected third action confirm_probe, got %q", result.Ticks[2].Decision.Action)
	}
	if result.Ticks[2].Transition.Kind != SessionTransitionCommitProbe {
		t.Fatalf("expected commit_probe transition, got %q", result.Ticks[2].Transition.Kind)
	}
	if result.FinalState.CurrentStep != PlaybackStepH2641080p {
		t.Fatalf("expected final step 1080p, got %q", result.FinalState.CurrentStep)
	}
}
