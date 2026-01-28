package openwebif

import (
	"errors"
	"fmt"
)

var (
	// Sentinel errors for errors.Is checks at the boundary.
	ErrNotFound            = errors.New("upstream: resource not found")
	ErrForbidden           = errors.New("upstream: access forbidden")
	ErrUpstreamUnavailable = errors.New("upstream: host unreachable or transport failure")
	ErrUpstreamError       = errors.New("upstream: internal error (5xx)")
	ErrUpstreamBadResponse = errors.New("upstream: invalid response format or malformed data")
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

func (e *OWIError) Unwrap() error {
	return e.Sentinel
}
