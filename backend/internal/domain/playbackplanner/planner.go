package playbackplanner

import "errors"

// ErrRuleNotImplemented is returned when the planner has no valid rules for the evidence.
var ErrRuleNotImplemented = errors.New("no planner rules implemented for this scenario")

// PlanningResult encapsulates the outcome of the planning process.
type PlanningResult struct {
	Plan  PlaybackPlan
	Trace PlanTrace
}

// Plan takes PlaybackEvidence and produces a PlanningResult.
// It is a pure, side-effect-free function. Deny is a valid plan, not an error.
// Errors are only returned if the evidence itself is invalid/malformed or if no rules apply.
func Plan(e PlaybackEvidence) (PlanningResult, error) {
	// TODO: Implement the pure planner logic
	return PlanningResult{}, ErrRuleNotImplemented
}
