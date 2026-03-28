package openwebif

import (
	"errors"
	"net/http"
	"testing"
)

func TestTimerOperationError_MapsKnownMessages(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		message      string
		wantSentinel error
		wantStatus   int
	}{
		{
			name:         "logical_rejection_uses_status_ok",
			status:       http.StatusOK,
			message:      "Konflikt mit anderem Timer",
			wantSentinel: ErrUpstreamRejected,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "not_found_http_status",
			status:       http.StatusNotFound,
			message:      "timer not found",
			wantSentinel: ErrNotFound,
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "conflict_http_status",
			status:       http.StatusConflict,
			message:      "Timer conflict detected",
			wantSentinel: ErrConflict,
			wantStatus:   http.StatusConflict,
		},
		{
			name:         "bad_request_remains_generic_rejection",
			status:       http.StatusBadRequest,
			message:      "unexpected response",
			wantSentinel: ErrUpstreamRejected,
			wantStatus:   http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := timerOperationError("timers.add", tt.status, tt.message)
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("expected sentinel %v, got %v", tt.wantSentinel, err)
			}
			var owiErr *OWIError
			if !errors.As(err, &owiErr) {
				t.Fatalf("expected OWIError, got %T", err)
			}
			if owiErr.Status != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, owiErr.Status)
			}
		})
	}
}

func TestTimerErrorClassifiers_UseTypedErrors(t *testing.T) {
	if !IsTimerConflict(ErrTimerConflict) {
		t.Fatal("ErrTimerConflict must classify as timer conflict")
	}
	if !IsTimerNotFound(ErrTimerNotFound) {
		t.Fatal("ErrTimerNotFound must classify as timer not found")
	}

	// Backward compatibility with older sentinels.
	if !IsTimerConflict(ErrConflict) {
		t.Fatal("ErrConflict must classify as timer conflict")
	}
	if !IsTimerNotFound(ErrNotFound) {
		t.Fatal("ErrNotFound must classify as timer not found")
	}

	if IsTimerConflict(errors.New("conflict in plain text")) {
		t.Fatal("plain text error must not classify as timer conflict")
	}
	if IsTimerNotFound(errors.New("404 in plain text")) {
		t.Fatal("plain text error must not classify as timer not found")
	}
	if IsTimerConflict(&OWIError{Status: http.StatusOK, Body: "Konflikt mit anderem Timer"}) {
		t.Fatal("localized body text must not classify as timer conflict without typed status")
	}
	if IsTimerNotFound(&OWIError{Status: http.StatusOK, Body: "timer not found"}) {
		t.Fatal("localized body text must not classify as timer not found without typed status")
	}
}
