package openwebif

import (
	"errors"
	"net/http"
)

var (
	ErrTimerConflict = errors.New("timer conflict")
	ErrTimerNotFound = errors.New("timer not found")
)

func timerOperationError(operation string, status int, message string) error {
	if status == 0 {
		status = http.StatusOK
	}

	return &OWIError{
		Sentinel:  timerOperationSentinel(status),
		Operation: operation,
		Status:    status,
		Body:      message,
	}
}

func timerOperationSentinel(status int) error {
	switch status {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrForbidden
	default:
		if status >= 500 {
			return ErrUpstreamError
		}
		return ErrUpstreamRejected
	}
}

func IsTimerConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrTimerConflict) || errors.Is(err, ErrConflict) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		return owiErr.Status == http.StatusConflict
	}
	return false
}

func IsTimerNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrTimerNotFound) || errors.Is(err, ErrNotFound) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		return owiErr.Status == http.StatusNotFound
	}
	return false
}
