package openwebif

import (
	"errors"
	"net/http"
	"strings"
)

// OpenWebIF timer endpoints return localized free-text messages for logical failures
// (result=false) without a stable machine-readable error code.
var timerConflictTokens = []string{"conflict", "overlap", "konflikt", "ueberschneidung", "überschneidung"}
var timerNotFoundTokens = []string{"not found", "nicht gefunden", "404"}

var (
	ErrTimerConflict = errors.New("timer conflict")
	ErrTimerNotFound = errors.New("timer not found")
)

func timerOperationError(operation, message string) error {
	normalized := strings.TrimSpace(message)
	sentinel := ErrUpstreamBadResponse
	status := http.StatusBadRequest

	switch {
	case timerMessageHasAnyToken(normalized, timerConflictTokens):
		sentinel = ErrTimerConflict
		status = http.StatusConflict
	case timerMessageHasAnyToken(normalized, timerNotFoundTokens):
		sentinel = ErrTimerNotFound
		status = http.StatusNotFound
	}

	return &OWIError{
		Sentinel:  sentinel,
		Operation: operation,
		Status:    status,
		Body:      normalized,
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

func timerMessageHasAnyToken(message string, tokens []string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	for _, token := range tokens {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}
