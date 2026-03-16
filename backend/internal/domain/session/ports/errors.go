// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import (
	"errors"
	"strings"
)

// ErrNoValidTS signals that preflight failed to detect a valid TS stream.
var ErrNoValidTS = errors.New("preflight no valid ts")

// PreflightError provides a typed failure reason for TS preflight checks.
type PreflightError struct {
	Reason string
	Result PreflightResult
}

func (e *PreflightError) Error() string {
	if e == nil {
		return ErrNoValidTS.Error()
	}
	detail := strings.TrimSpace(e.Reason)
	if detail == "" {
		detail = e.StructuredResult().FailureDetail()
	}
	if detail == "" {
		return ErrNoValidTS.Error()
	}
	return ErrNoValidTS.Error() + ": " + detail
}

func (e *PreflightError) Unwrap() error {
	return ErrNoValidTS
}

// StructuredResult returns the bounded media-preflight result behind a legacy preflight error.
func (e *PreflightError) StructuredResult() PreflightResult {
	if e == nil {
		return NewPreflightResult("", 0, 0, 0, 0)
	}
	result := e.Result
	if detail := strings.TrimSpace(e.Reason); detail != "" && strings.TrimSpace(result.Detail) == "" {
		result.Detail = detail
	}
	result.OK = false
	return result.Normalized()
}

// NewPreflightError wraps a structured preflight result in the legacy ErrNoValidTS-compatible error.
func NewPreflightError(result PreflightResult) *PreflightError {
	normalized := result.Normalized()
	normalized.OK = false
	return &PreflightError{
		Reason: normalized.FailureDetail(),
		Result: normalized,
	}
}
