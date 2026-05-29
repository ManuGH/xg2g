package openwebif

import (
	"errors"
	"fmt"
)

var (
	// Sentinel errors for errors.Is checks at the boundary.
	ErrNotFound            = errors.New("upstream: resource not found")
	ErrConflict            = errors.New("upstream: conflict")
	ErrForbidden           = errors.New("upstream: access forbidden")
	ErrUpstreamUnavailable = errors.New("upstream: host unreachable or transport failure")
	ErrUpstreamError       = errors.New("upstream: internal error (5xx)")
	ErrUpstreamBadResponse = errors.New("upstream: invalid response format or malformed data")
	ErrUpstreamRejected    = errors.New("upstream: request rejected")
	ErrTimeout             = errors.New("upstream: request timed out")
)

// OWIError is a rich error type that wraps the sentinel errors with context.
type OWIError struct {
	Sentinel  error
	Operation string
	Status    int
	Body      string
	Err       error // Nested lower-level error (e.g. net.Error)
}

func (e *OWIError) Error() string {
	msg := fmt.Sprintf("openwebif: %s: %v", e.Operation, e.Sentinel)
	if e.Status > 0 {
		msg = fmt.Sprintf("%s (HTTP %d)", msg, e.Status)
	}
	if e.Body != "" {
		msg = fmt.Sprintf("%s: %s", msg, e.Body)
	}
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Err)
	}
	return msg
}

// Unwrap exposes both the boundary sentinel and the nested lower-level error so
// errors.Is/As can reach either branch. Callers match the sentinel
// (errors.Is(err, ErrTimeout)); the circuit breaker and retry classifier reach
// the nested net.Error / context error to decide whether a failure is technical.
// Returning only the sentinel (a plain errors.New) hid the transport error and
// stopped the breaker from ever tripping on dial timeouts / connection refused.
func (e *OWIError) Unwrap() []error {
	errs := make([]error, 0, 2)
	if e.Sentinel != nil {
		errs = append(errs, e.Sentinel)
	}
	if e.Err != nil {
		errs = append(errs, e.Err)
	}
	return errs
}
