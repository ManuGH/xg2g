package openwebif

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestWrapError_Sentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		status   int
		sentinel error
	}{
		{
			name:     "HTTP 404",
			err:      nil,
			status:   http.StatusNotFound,
			sentinel: ErrNotFound,
		},
		{
			name:     "HTTP 403",
			err:      nil,
			status:   http.StatusForbidden,
			sentinel: ErrForbidden,
		},
		{
			name:     "HTTP 401",
			err:      nil,
			status:   http.StatusUnauthorized,
			sentinel: ErrForbidden,
		},
		{
			name:     "HTTP 500",
			err:      nil,
			status:   http.StatusInternalServerError,
			sentinel: ErrUpstreamError,
		},
		{
			name:     "Network Timeout",
			err:      &net.DNSError{IsTimeout: true},
			status:   0,
			sentinel: ErrTimeout,
		},
		{
			name:     "Context Timeout",
			err:      context.DeadlineExceeded,
			status:   0,
			sentinel: ErrTimeout,
		},
		{
			name:     "Malformed JSON (400)",
			err:      nil,
			status:   http.StatusBadRequest,
			sentinel: ErrUpstreamBadResponse,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := wrapError("test", tc.err, tc.status, nil)
			if !errors.Is(wrapped, tc.sentinel) {
				t.Errorf("expected sentinel %v, got %v", tc.sentinel, wrapped)
			}

			// Verify context remains (diagnostic fields)
			var owiErr *OWIError
			if !errors.As(wrapped, &owiErr) {
				t.Fatal("expected error to be *OWIError")
			}
			if owiErr.Operation != "test" {
				t.Errorf("expected operation 'test', got %s", owiErr.Operation)
			}
			if owiErr.Status != tc.status {
				t.Errorf("expected status %d, got %d", tc.status, owiErr.Status)
			}
		})
	}
}

func TestWrapError_Redaction(t *testing.T) {
	body := []byte(`{"error": "invalid token: token=1234-5678-abcd-efgh sid=secret_123 password=my_secret_pass"}`)
	err := wrapError("redact_test", nil, 403, body)

	msg := err.Error()
	if strings.Contains(msg, "1234-5678") {
		t.Error("expected token to be redacted")
	}
	if strings.Contains(msg, "secret_123") {
		t.Error("expected sid to be redacted")
	}
	if strings.Contains(msg, "my_secret_pass") {
		t.Error("expected password to be redacted")
	}
	if !strings.Contains(msg, "[REDACTED]") {
		t.Error("expected [REDACTED] placeholder")
	}
}
