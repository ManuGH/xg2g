package decision

import "sort"

func makeRefDecision(mode Mode, reasons []ReasonCode, rules []string) *Decision {
	sort.Sort(ReasonCodeSlice(reasons))
	return &Decision{
		Mode:    mode,
		Reasons: reasons,
		Trace: Trace{
			RuleHits: rules,
		},
	}
}
