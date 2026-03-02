package openwebif

import (
	"errors"
	"net/http"
	"testing"
)

func TestTimerOperationError_MapsKnownMessages(t *testing.T) {
	tests := []struct {
		name         string
		message      string
		wantSentinel error
		wantStatus   int
	}{
		{
			name:         "conflict_english",
			message:      "Timer conflict detected",
			wantSentinel: ErrTimerConflict,
			wantStatus:   http.StatusConflict,
		},
		{
			name:         "conflict_german",
			message:      "Konflikt mit anderem Timer",
			wantSentinel: ErrTimerConflict,
			wantStatus:   http.StatusConflict,
		},
		{
			name:         "not_found_english",
			message:      "timer not found",
			wantSentinel: ErrTimerNotFound,
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "not_found_404",
			message:      "HTTP 404",
			wantSentinel: ErrTimerNotFound,
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "unknown_message",
			message:      "unexpected response",
			wantSentinel: ErrUpstreamBadResponse,
			wantStatus:   http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := timerOperationError("timers.add", tt.message)
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
}
