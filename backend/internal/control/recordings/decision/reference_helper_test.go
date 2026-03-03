package decision

func makeRefDecision(mode Mode, reasons []ReasonCode, rules []string) *Decision {
	sortReasonsByPriority(reasons)
	return &Decision{
		Mode:    mode,
		Reasons: reasons,
		Trace: Trace{
			RuleHits: rules,
		},
	}
}
