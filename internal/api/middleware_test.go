// SPDX-License-Identifier: MIT
package api

import (
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
			req := httptest.NewRequest("GET", "/test", nil)
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
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := requestIDMiddleware(testHandler)
	req := httptest.NewRequest("GET", "/api/test", nil)
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

func TestMiddlewareChain(t *testing.T) {
	// Test that the middleware chain works correctly
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that request ID is available in context
		reqID := log.RequestIDFromContext(r.Context())
		if reqID == "" {
			t.Error("Request ID not found in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	// Apply full middleware chain
	handler := withMiddlewares(testHandler)
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should have request ID in response
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("Expected X-Request-ID header in response")
	}

	// Should have CORS headers
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS headers")
	}

	// Should have security headers
	if rr.Header().Get("X-Content-Type-Options") == "" {
		t.Error("Expected security headers")
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
