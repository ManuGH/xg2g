package openwebif

import (
	"errors"
	"net/http"
	"strings"
)

var timerConflictTokens = []string{"conflict", "overlap", "konflikt", "ueberschneidung", "überschneidung"}
var timerNotFoundTokens = []string{"not found", "nicht gefunden", "404"}

func timerOperationError(operation, message string) error {
	normalized := strings.TrimSpace(message)
	sentinel := ErrUpstreamBadResponse
	status := http.StatusBadRequest

	switch {
	case timerMessageHasAnyToken(normalized, timerConflictTokens):
		sentinel = ErrConflict
		status = http.StatusConflict
	case timerMessageHasAnyToken(normalized, timerNotFoundTokens):
		sentinel = ErrNotFound
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
	if errors.Is(err, ErrConflict) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		if owiErr.Status == http.StatusConflict {
			return true
		}
		return timerMessageHasAnyToken(owiErr.Body, timerConflictTokens)
	}
	return timerMessageHasAnyToken(err.Error(), timerConflictTokens)
}

func IsTimerNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	var owiErr *OWIError
	if errors.As(err, &owiErr) {
		if owiErr.Status == http.StatusNotFound {
			return true
		}
		return timerMessageHasAnyToken(owiErr.Body, timerNotFoundTokens)
	}
	return timerMessageHasAnyToken(err.Error(), timerNotFoundTokens)
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
