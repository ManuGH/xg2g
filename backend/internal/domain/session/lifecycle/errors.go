// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lifecycle

import (
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

var (
	ErrAdmissionRejected  = errors.New("admission rejected")
	ErrSessionNotFound    = errors.New("session not found")
	ErrBadRequest         = errors.New("bad request")
	ErrPipelineFailure    = errors.New("pipeline failure")
	ErrSessionCanceled    = errors.New("session canceled")
	ErrInvariantViolation = errors.New("invariant violation")
	ErrUnknown            = errors.New("unknown session error")
)

func ReasonErrorClass(reason model.ReasonCode) error {
	switch reason {
	case model.RLeaseBusy, model.RLeaseExpired:
		return ErrAdmissionRejected
	case model.RNotFound:
		return ErrSessionNotFound
	case model.RBadRequest:
		return ErrBadRequest
	case model.RClientStop, model.RCancelled, model.RIdleTimeout:
		return ErrSessionCanceled
	case model.RInvariantViolation:
		return ErrInvariantViolation
	case model.RInternalInvariantBreach:
		return ErrInvariantViolation
	case model.RUnknown:
		return ErrUnknown
	case model.RNone:
		return nil
	default:
		return ErrPipelineFailure
	}
}
