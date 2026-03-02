// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import "errors"

// ErrNoValidTS signals that preflight failed to detect a valid TS stream.
var ErrNoValidTS = errors.New("preflight no valid ts")

// PreflightError provides a typed failure reason for TS preflight checks.
type PreflightError struct {
	Reason string
}

func (e *PreflightError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrNoValidTS.Error()
	}
	return "preflight no valid ts: " + e.Reason
}

func (e *PreflightError) Unwrap() error {
	return ErrNoValidTS
}
