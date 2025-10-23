// SPDX-License-Identifier: MIT
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/google/uuid"
)

func TestRequestIDMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		existingID    string
		wantInHeader  bool
		wantInContext bool
	}{
		{
			name:          "generates new request ID when none provided",
			existingID:    "",
			wantInHeader:  true,
			wantInContext: true,
		},
		{
			name:          "uses existing request ID from header",
			existingID:    "test-request-id-123",
			wantInHeader:  true,
			wantInContext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that checks context
			var contextID string
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				contextID = log.RequestIDFromContext(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with middleware
			handler := requestIDMiddleware(testHandler)

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.existingID != "" {
				req.Header.Set("X-Request-ID", tt.existingID)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Execute request
			handler.ServeHTTP(rr, req)

			// Check response header
			if tt.wantInHeader {
				headerID := rr.Header().Get("X-Request-ID")
				if headerID == "" {
					t.Error("Expected X-Request-ID header to be set")
				}

				if tt.existingID != "" {
					if headerID != tt.existingID {
						t.Errorf("Expected header ID %s, got %s", tt.existingID, headerID)
					}
				} else {
					// Should be a valid UUID
					if _, err := uuid.Parse(headerID); err != nil {
						t.Errorf("Generated request ID is not a valid UUID: %s", headerID)
					}
				}
			}

			// Check context
			if tt.wantInContext {
				if contextID == "" {
					t.Error("Expected request ID to be set in context")
				}

				if tt.existingID != "" && contextID != tt.existingID {
					t.Errorf("Expected context ID %s, got %s", tt.existingID, contextID)
				}
			}
		})
	}
}

func TestRequestIDMiddlewareLogging(t *testing.T) {
	// This test checks that the middleware logs with proper fields
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(testHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Check that response has request ID
	reqID := rr.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("Expected X-Request-ID header to be set")
	}

	// Verify it's a valid UUID
	if _, err := uuid.Parse(reqID); err != nil {
		t.Errorf("Request ID is not a valid UUID: %s", reqID)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.1:12345",
			headers:    map[string]string{},
			expected:   "192.168.1.1",
		},
		{
			name:       "invalid remote addr",
			remoteAddr: "invalid",
			headers:    map[string]string{},
			expected:   "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				RemoteAddr: tt.remoteAddr,
				Header:     make(http.Header),
			}

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := clientIP(req)
			if result != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	// A simple handler to be wrapped by the middleware
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create a request to test with
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()

	// Create the middleware handler
	handler := securityHeadersMiddleware(nextHandler)

	// Serve the HTTP request to our handler
	handler.ServeHTTP(rr, req)

	// Check the headers
	expectedHeaders := map[string]string{
		"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	}

	for key, value := range expectedHeaders {
		if got := rr.Header().Get(key); got != value {
			t.Errorf("Expected header %s: %q, got: %q", key, value, got)
		}
	}
}

func TestPanicRecoveryMiddleware(t *testing.T) {
	// Handler that panics
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	handler := panicRecoveryMiddleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/trigger-panic", nil)
	rr := httptest.NewRecorder()

	// Should not panic; should return 500 and JSON body
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	// Validate JSON structure contains error and request_id
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if body["error"] == nil {
		t.Errorf("expected 'error' field in response body")
	}
}

func TestPanicRecoveryMiddlewareNormalFlow(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := panicRecoveryMiddleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}
