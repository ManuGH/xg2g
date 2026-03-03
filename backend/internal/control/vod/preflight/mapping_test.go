package preflight

import (
	"net/http"
	"strings"
	"testing"
)

func TestMapPreflightOutcome(t *testing.T) {
	expectations := map[PreflightOutcome]int{
		PreflightOK:           0,
		PreflightUnreachable:  http.StatusBadGateway,
		PreflightTimeout:      http.StatusGatewayTimeout,
		PreflightUnauthorized: http.StatusUnauthorized,
		PreflightForbidden:    http.StatusForbidden,
		PreflightNotFound:     http.StatusNotFound,
		PreflightBadGateway:   http.StatusBadGateway,
		PreflightInternal:     http.StatusInternalServerError,
	}

	if len(expectations) != len(AllOutcomes()) {
		t.Fatalf("expected %d outcomes, got %d", len(expectations), len(AllOutcomes()))
	}

	for _, outcome := range AllOutcomes() {
		expectedStatus, ok := expectations[outcome]
		if !ok {
			t.Fatalf("missing expectation for outcome %q", outcome)
		}

		status, problemType, title := MapPreflightOutcome(outcome)
		if status != expectedStatus {
			t.Fatalf("outcome %q: expected status %d, got %d", outcome, expectedStatus, status)
		}

		if outcome == PreflightOK {
			if problemType != "" || title != "" {
				t.Fatalf("outcome %q: expected empty problem fields, got type=%q title=%q", outcome, problemType, title)
			}
			continue
		}

		if problemType == "" || !strings.HasSuffix(problemType, string(outcome)) {
			t.Fatalf("outcome %q: unexpected problem type %q", outcome, problemType)
		}
		if strings.TrimSpace(title) == "" {
			t.Fatalf("outcome %q: title must be non-empty", outcome)
		}
	}
}
