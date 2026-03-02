package recordings

import (
	"errors"
	"fmt"

	"github.com/ManuGH/xg2g/internal/control/playback"
)

type ErrorClass string

const (
	ClassInvalidArgument ErrorClass = "invalid_argument"
	ClassForbidden       ErrorClass = "forbidden"
	ClassNotFound        ErrorClass = "not_found"
	ClassPreparing       ErrorClass = "preparing"
	ClassUnsupported     ErrorClass = "unsupported"
	ClassUpstream        ErrorClass = "upstream"
	ClassInternal        ErrorClass = "internal"
)

type ErrNotFound struct {
	RecordingID string
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("recording not found: %s", e.RecordingID)
}

type ErrForbidden struct {
	RequiredScopes []string
}

func (e ErrForbidden) Error() string {
	return "forbidden"
}

type ErrInvalidArgument struct {
	Field  string
	Reason string
}

func (e ErrInvalidArgument) Error() string {
	return fmt.Sprintf("invalid argument %s: %s", e.Field, e.Reason)
}

type ErrUpstream struct {
	Op    string
	Cause error
}

func (e ErrUpstream) Error() string {
	return fmt.Sprintf("upstream error in %s: %v", e.Op, e.Cause)
}

func (e ErrUpstream) Unwrap() error {
	return e.Cause
}

type ErrPreparing struct {
	RecordingID string
}

func (e ErrPreparing) Error() string {
	return fmt.Sprintf("recording preparing: %s", e.RecordingID)
}

func Classify(err error) ErrorClass {
	if err == nil {
		return ""
	}
	// ClassUnsupported is reserved for explicit capability limits (not a general bucket).
	if errors.Is(err, ErrRemoteProbeUnsupported) || errors.Is(err, playback.ErrUnsupported) {
		return ClassUnsupported
	}
	if errors.Is(err, playback.ErrUpstream) {
		return ClassUpstream
	}
	if errors.Is(err, playback.ErrNotFound) {
		return ClassNotFound
	}
	if errors.Is(err, playback.ErrPreparing) {
		return ClassPreparing
	}
	if errors.Is(err, playback.ErrForbidden) {
		return ClassForbidden
	}

	switch err.(type) {
	case ErrNotFound, *ErrNotFound:
		return ClassNotFound
	case ErrForbidden, *ErrForbidden:
		return ClassForbidden
	case ErrInvalidArgument, *ErrInvalidArgument:
		return ClassInvalidArgument
	case ErrPreparing, *ErrPreparing:
		return ClassPreparing
	case ErrUpstream, *ErrUpstream:
		return ClassUpstream
	default:
		return ClassInternal
	}
}
