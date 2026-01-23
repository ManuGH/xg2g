package decision

import "strings"

// validateInput performs V-1, V-2, V-3 fail-closed validation.
// Returns RFC7807 problem or nil if valid.
func validateInput(input DecisionInput) *Problem {
	// V-1: Capabilities presence (for API v3.1+)
	if strings.HasPrefix(input.APIVersion, "v3.1") || strings.HasPrefix(input.APIVersion, "v3.") {
		if input.Capabilities.Version == 0 && len(input.Capabilities.Containers) == 0 {
			// Capabilities appear missing/empty
			return &Problem{
				Type:   "recordings/capabilities-missing",
				Title:  "Capabilities Missing",
				Status: 412,
				Code:   string(ProblemCapabilitiesMissing),
				Detail: "Client must provide capabilities (capabilities_version required)",
			}
		}
	}

	// V-2: Capabilities version
	if input.Capabilities.Version != 0 && input.Capabilities.Version != 1 {
		return &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "Capabilities Invalid",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: "capabilities_version not supported (current: 1)",
		}
	}

	// V-3: Media truth completeness
	if isEmpty(input.Source.Container) || isEmpty(input.Source.VideoCodec) || isEmpty(input.Source.AudioCodec) {
		return &Problem{
			Type:   "recordings/decision-ambiguous",
			Title:  "Decision Ambiguous",
			Status: 422,
			Code:   string(ProblemDecisionAmbiguous),
			Detail: "Media truth unavailable or unknown (cannot make deterministic decision)",
		}
	}

	// Check for contradictory/unknown sentinel values in source
	if isUnknownSentinel(input.Source.Container) || isUnknownSentinel(input.Source.VideoCodec) || isUnknownSentinel(input.Source.AudioCodec) {
		return &Problem{
			Type:   "recordings/decision-ambiguous",
			Title:  "Decision Ambiguous",
			Status: 422,
			Code:   string(ProblemDecisionAmbiguous),
			Detail: "Media truth unavailable or unknown (cannot make deterministic decision)",
		}
	}

	return nil
}

// isEmpty checks if a string is empty or whitespace-only.
func isEmpty(s string) bool {
	return strings.TrimSpace(s) == ""
}

// isUnknownSentinel checks if a value indicates unknown/contradictory truth.
func isUnknownSentinel(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return lower == "unknown" || lower == "none" || lower == "null"
}
