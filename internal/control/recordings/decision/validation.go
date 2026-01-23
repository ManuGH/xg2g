package decision

import (
	"fmt"
	"strings"
)

var allowedAPIVersions = map[string]struct{}{
	"v3":   {},
	"v3.0": {},
	"v3.1": {},
}

// validateInput performs V-1, V-2, V-3 fail-closed validation.
// Returns RFC7807 problem or nil if valid.
func validateInput(input DecisionInput) *Problem {
	// V-0: API Version required
	apiVersion := robustNorm(input.APIVersion)
	if isEmpty(apiVersion) {
		return &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "API Version Missing",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: "Fail-Closed: 'api' (compact) or 'APIVersion' (legacy) is required",
		}
	}

	// V-1: Capabilities presence (for API v3.x family)
	if _, ok := allowedAPIVersions[apiVersion]; !ok {
		return &Problem{
			Type:   "recordings/capabilities-invalid",
			Title:  "API Version Invalid",
			Status: 400,
			Code:   string(ProblemCapabilitiesInvalid),
			Detail: fmt.Sprintf("Fail-Closed: unsupported api version %q", apiVersion),
		}
	}

	// V-1: Capabilities presence (for API v3.x family)
	if strings.HasPrefix(apiVersion, "v3") {
		if input.Capabilities.Version == 0 {
			// Capabilities version missing
			return &Problem{
				Type:   "recordings/capabilities-missing",
				Title:  "Capabilities Missing",
				Status: 412,
				Code:   string(ProblemCapabilitiesMissing),
				Detail: "Client must provide capabilities (capabilities_version required for v3)",
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
