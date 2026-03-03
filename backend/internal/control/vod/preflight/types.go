package preflight

import "context"

// PreflightOutcome is a bounded taxonomy for preflight results.
type PreflightOutcome string

const (
	PreflightOK           PreflightOutcome = "ok"
	PreflightUnreachable  PreflightOutcome = "unreachable"
	PreflightTimeout      PreflightOutcome = "timeout"
	PreflightUnauthorized PreflightOutcome = "unauthorized"
	PreflightForbidden    PreflightOutcome = "forbidden"
	PreflightNotFound     PreflightOutcome = "not_found"
	PreflightBadGateway   PreflightOutcome = "bad_gateway"
	PreflightInternal     PreflightOutcome = "internal"
)

type SourceRef struct {
	URL string
}

// PreflightResult conveys the semantic outcome (Outcome) and any upstream HTTP status.
type PreflightResult struct {
	Outcome    PreflightOutcome
	HTTPStatus int
}

type PreflightProvider interface {
	Check(ctx context.Context, src SourceRef) (PreflightResult, error)
}

// AllOutcomes returns the complete set of supported outcomes.
func AllOutcomes() []PreflightOutcome {
	return []PreflightOutcome{
		PreflightOK,
		PreflightUnreachable,
		PreflightTimeout,
		PreflightUnauthorized,
		PreflightForbidden,
		PreflightNotFound,
		PreflightBadGateway,
		PreflightInternal,
	}
}
