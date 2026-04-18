package runtimepolicy

import (
	"strconv"
	"time"
)

type ReplayMetadata struct {
	SessionID     string             `json:"sessionId,omitempty"`
	ServiceRef    string             `json:"serviceRef,omitempty"`
	ClientPath    string             `json:"clientPath,omitempty"`
	SourceType    string             `json:"sourceType,omitempty"`
	InitialTarget PlaybackLadderStep `json:"initialTarget,omitempty"`
}

type ReplayTickInput struct {
	TickAt       time.Time          `json:"tickAt"`
	ObservedStep PlaybackLadderStep `json:"observedStep,omitempty"`
	TargetStep   PlaybackLadderStep `json:"targetStep,omitempty"`
	Confidence   ConfidenceSnapshot `json:"confidence"`
}

type ReplayTickExpectation struct {
	Action             PolicyAction          `json:"action,omitempty"`
	PlannedTransition  SessionTransitionKind `json:"plannedTransition,omitempty"`
	ExecutedTransition SessionTransitionKind `json:"executedTransition,omitempty"`
	ActiveStep         PlaybackLadderStep    `json:"activeStep,omitempty"`
	ProbeStep          PlaybackLadderStep    `json:"probeStep,omitempty"`
	ProbeState         ProbeLifecycleState   `json:"probeState,omitempty"`
	RuntimePhase       string                `json:"runtimePhase,omitempty"`
	Blockers           []string              `json:"blockers,omitempty"`
	Reasons            []string              `json:"reasons,omitempty"`
}

type ReplayTick struct {
	Input    ReplayTickInput       `json:"input"`
	Expected ReplayTickExpectation `json:"expected"`
}

type RuntimePolicyReplay struct {
	Metadata     ReplayMetadata   `json:"metadata"`
	InitialState SessionLoopState `json:"initialState"`
	Ticks        []ReplayTick     `json:"ticks"`
	FinalState   SessionLoopState `json:"finalState"`
}

type ReplayResultTick struct {
	Input      ReplayTickInput     `json:"input"`
	State      SessionLoopState    `json:"state"`
	Decision   SessionLoopDecision `json:"decision"`
	Transition SessionTransition   `json:"transition"`
	Changed    bool                `json:"changed"`
}

type ReplayResult struct {
	Ticks      []ReplayResultTick `json:"ticks"`
	FinalState SessionLoopState   `json:"finalState"`
}

type ReplayMismatch struct {
	Tick     int    `json:"tick"`
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

func ReplayFromTimeline(metadata ReplayMetadata, initial SessionLoopState, timeline []TickTrace) RuntimePolicyReplay {
	replay := RuntimePolicyReplay{
		Metadata:     metadata,
		InitialState: NormalizeSessionLoopState(initial),
		FinalState:   NormalizeSessionLoopState(initial),
	}
	if metadata.InitialTarget == PlaybackStepUnknown {
		replay.Metadata.InitialTarget = replay.InitialState.TargetStep
	}
	for _, tick := range timeline {
		replayTick := ReplayTick{
			Input: ReplayTickInput{
				TickAt:       tick.TickAt,
				ObservedStep: NormalizePlaybackLadderStep(string(tick.ObservedStep)),
				TargetStep:   NormalizePlaybackLadderStep(string(tick.TargetStep)),
				Confidence: ConfidenceSnapshot{
					Score:             tick.ConfidenceScore,
					State:             tick.ConfidenceState,
					StateSince:        tick.ConfidenceStateSince,
					WindowCount:       tick.ConfidenceWindowCount,
					CooldownUntil:     tick.CooldownUntil,
					PolicyConstraints: append([]string(nil), tick.PolicyConstraints...),
					Reasons:           append([]string(nil), tick.Reasons...),
				},
			},
			Expected: ReplayTickExpectation{
				Action:             tick.PolicyAction,
				PlannedTransition:  tick.PlannedTransition,
				ExecutedTransition: tick.ExecutedTransition,
				ActiveStep:         tick.ActiveStep,
				ProbeStep:          tick.ProbeStep,
				ProbeState:         tick.ProbeState,
				RuntimePhase:       tick.RuntimePhase,
				Blockers:           append([]string(nil), tick.Blockers...),
				Reasons:            append([]string(nil), tick.Reasons...),
			},
		}
		replay.Ticks = append(replay.Ticks, replayTick)
	}
	if len(replay.Ticks) > 0 {
		last := replay.Ticks[len(replay.Ticks)-1].Expected
		replay.FinalState = NormalizeSessionLoopState(SessionLoopState{
			CurrentStep:       last.ActiveStep,
			TargetStep:        replay.Ticks[len(replay.Ticks)-1].Input.TargetStep,
			ProbeStep:         last.ProbeStep,
			ProbeState:        last.ProbeState,
			ConfidenceScore:   replay.Ticks[len(replay.Ticks)-1].Input.Confidence.Score,
			ConfidenceState:   replay.Ticks[len(replay.Ticks)-1].Input.Confidence.State,
			CooldownUntil:     replay.Ticks[len(replay.Ticks)-1].Input.Confidence.CooldownUntil,
			PolicyConstraints: append([]string(nil), replay.Ticks[len(replay.Ticks)-1].Input.Confidence.PolicyConstraints...),
			Reasons:           append([]string(nil), last.Reasons...),
		})
	}
	return replay
}

func RunReplay(replay RuntimePolicyReplay) ReplayResult {
	state := NormalizeSessionLoopState(replay.InitialState)
	result := ReplayResult{
		FinalState: state,
	}
	for _, tick := range replay.Ticks {
		next, decision, changed := TickSessionLoop(state, SessionLoopInput{
			ObservedStep: tick.Input.ObservedStep,
			TargetStep:   tick.Input.TargetStep,
			Confidence:   tick.Input.Confidence,
		}, tick.Input.TickAt)
		transition := PlanSessionTransition(state, next, decision)
		result.Ticks = append(result.Ticks, ReplayResultTick{
			Input:      tick.Input,
			State:      next,
			Decision:   decision,
			Transition: transition,
			Changed:    changed,
		})
		state = next
	}
	result.FinalState = state
	return result
}

func CompareReplayExpectations(replay RuntimePolicyReplay, result ReplayResult) []ReplayMismatch {
	var mismatches []ReplayMismatch
	limit := len(replay.Ticks)
	if len(result.Ticks) < limit {
		limit = len(result.Ticks)
	}
	for i := 0; i < limit; i++ {
		expected := replay.Ticks[i].Expected
		actual := result.Ticks[i]
		if expected.Action != actual.Decision.Action {
			mismatches = append(mismatches, ReplayMismatch{
				Tick:     i,
				Field:    "action",
				Expected: string(expected.Action),
				Actual:   string(actual.Decision.Action),
			})
		}
		if expected.PlannedTransition != actual.Transition.Kind {
			mismatches = append(mismatches, ReplayMismatch{
				Tick:     i,
				Field:    "planned_transition",
				Expected: string(expected.PlannedTransition),
				Actual:   string(actual.Transition.Kind),
			})
		}
		if expected.ActiveStep != actual.State.CurrentStep {
			mismatches = append(mismatches, ReplayMismatch{
				Tick:     i,
				Field:    "active_step",
				Expected: string(expected.ActiveStep),
				Actual:   string(actual.State.CurrentStep),
			})
		}
		if expected.ProbeStep != actual.State.ProbeStep {
			mismatches = append(mismatches, ReplayMismatch{
				Tick:     i,
				Field:    "probe_step",
				Expected: string(expected.ProbeStep),
				Actual:   string(actual.State.ProbeStep),
			})
		}
		if expected.ProbeState != actual.State.ProbeState {
			mismatches = append(mismatches, ReplayMismatch{
				Tick:     i,
				Field:    "probe_state",
				Expected: string(expected.ProbeState),
				Actual:   string(actual.State.ProbeState),
			})
		}
	}
	if len(replay.Ticks) != len(result.Ticks) {
		mismatches = append(mismatches, ReplayMismatch{
			Tick:     limit,
			Field:    "tick_count",
			Expected: intString(len(replay.Ticks)),
			Actual:   intString(len(result.Ticks)),
		})
	}
	return mismatches
}

func intString(v int) string {
	return strconv.Itoa(v)
}
