// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
)

var (
	ErrAdmissionRejected  = lifecycle.ErrAdmissionRejected
	ErrSessionNotFound    = lifecycle.ErrSessionNotFound
	ErrBadRequest         = lifecycle.ErrBadRequest
	ErrPipelineFailure    = lifecycle.ErrPipelineFailure
	ErrSessionCanceled    = lifecycle.ErrSessionCanceled
	ErrInvariantViolation = lifecycle.ErrInvariantViolation
	ErrUnknown            = lifecycle.ErrUnknown
)
