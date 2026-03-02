package preflight

import "net/http"

const preflightProblemPrefix = "preflight/"

// MapPreflightOutcome returns the HTTP status and RFC7807 problem fields for a given outcome.
// PreflightOK returns zero values to signal "no problem".
func MapPreflightOutcome(outcome PreflightOutcome) (status int, problemType, title string) {
	switch outcome {
	case PreflightOK:
		return 0, "", ""
	case PreflightUnreachable:
		return http.StatusBadGateway, preflightProblemPrefix + "unreachable", "Source unreachable"
	case PreflightTimeout:
		return http.StatusGatewayTimeout, preflightProblemPrefix + "timeout", "Source timeout"
	case PreflightUnauthorized:
		return http.StatusUnauthorized, preflightProblemPrefix + "unauthorized", "Unauthorized"
	case PreflightForbidden:
		return http.StatusForbidden, preflightProblemPrefix + "forbidden", "Forbidden"
	case PreflightNotFound:
		return http.StatusNotFound, preflightProblemPrefix + "not_found", "Not found"
	case PreflightBadGateway:
		return http.StatusBadGateway, preflightProblemPrefix + "bad_gateway", "Bad gateway"
	case PreflightInternal:
		return http.StatusInternalServerError, preflightProblemPrefix + "internal", "Internal error"
	default:
		return http.StatusInternalServerError, preflightProblemPrefix + "internal", "Internal error"
	}
}
