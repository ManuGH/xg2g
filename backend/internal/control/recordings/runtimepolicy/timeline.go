package runtimepolicy

const sessionTimelineMaxTicks = 64

func AppendTickTrace(existing []TickTrace, tick TickTrace) []TickTrace {
	if len(existing) >= sessionTimelineMaxTicks {
		trimmed := make([]TickTrace, 0, sessionTimelineMaxTicks)
		trimmed = append(trimmed, existing[len(existing)-sessionTimelineMaxTicks+1:]...)
		existing = trimmed
	} else {
		existing = append([]TickTrace(nil), existing...)
	}
	return append(existing, normalizeTickTrace(tick))
}

func normalizeTickTrace(tick TickTrace) TickTrace {
	tick.PolicyConstraints = compactStrings(tick.PolicyConstraints)
	tick.Blockers = compactStrings(tick.Blockers)
	tick.Reasons = compactStrings(tick.Reasons)
	return tick
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
