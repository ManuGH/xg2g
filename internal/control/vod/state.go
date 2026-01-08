package vod

// State represents the lifecycle state of a VOD build job.
type State int

const (
	StateIdle State = iota
	StateBuilding
	StateFinalizing
	StateSucceeded
	StateFailed
	StateCanceled
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateBuilding:
		return "Building"
	case StateFinalizing:
		return "Finalizing"
	case StateSucceeded:
		return "Succeeded"
	case StateFailed:
		return "Failed"
	case StateCanceled:
		return "Canceled"
	default:
		return "Unknown"
	}
}

// IsTerminal returns true if the state is terminal (no further transitions).
func (s State) IsTerminal() bool {
	return s == StateSucceeded || s == StateFailed || s == StateCanceled
}

// FailureReason categorizes why a build failed.
type FailureReason string

const (
	ReasonStall     FailureReason = "STALL"
	ReasonCrash     FailureReason = "CRASH"
	ReasonStartFail FailureReason = "START_FAIL"
	ReasonInternal  FailureReason = "INTERNAL"
	ReasonCanceled  FailureReason = "CANCELED"
)

// TransitionEvent describes a state transition trigger.
type TransitionEvent int

const (
	EventStart TransitionEvent = iota
	EventBuildComplete
	EventPublishComplete
	EventFail
	EventCancel
)

// CanTransition validates if a state transition is legal.
func CanTransition(from State, event TransitionEvent) bool {
	switch from {
	case StateIdle:
		return event == EventStart
	case StateBuilding:
		return event == EventBuildComplete || event == EventFail || event == EventCancel
	case StateFinalizing:
		return event == EventPublishComplete || event == EventFail
	default:
		// Terminal states cannot transition
		return false
	}
}

// Transition returns the new state given a valid transition event.
// Returns the original state if transition is invalid.
func Transition(from State, event TransitionEvent) State {
	if !CanTransition(from, event) {
		return from
	}

	switch event {
	case EventStart:
		return StateBuilding
	case EventBuildComplete:
		return StateFinalizing
	case EventPublishComplete:
		return StateSucceeded
	case EventFail:
		return StateFailed
	case EventCancel:
		return StateCanceled
	default:
		return from
	}
}
