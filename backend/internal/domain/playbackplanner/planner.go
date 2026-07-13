package playbackplanner

// PlanningResult encapsulates the outcome of the planning process.
type PlanningResult struct {
	Plan  PlaybackPlan
	Trace PlanTrace
}

// Plan takes PlaybackEvidence and produces a PlanningResult.
// It is a pure, side-effect-free function. Deny is a valid plan, not an error.
// Errors are only returned if the evidence itself is invalid/malformed.
func Plan(e PlaybackEvidence) (PlanningResult, error) {
	// TODO: Implement the pure planner logic
	return PlanningResult{}, nil
}
