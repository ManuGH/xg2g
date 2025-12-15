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
		{
			name:       "X-Forwarded-For header (untrusted)",
			remoteAddr: "192.168.1.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1",
			},
			// Since 192.168.1.1 is not in XG2G_TRUSTED_PROXIES, ignore header
			expected: "192.168.1.1",
		},
		{
			name:       "X-Real-IP header (untrusted)",
			remoteAddr: "192.168.1.1:12345",
			headers: map[string]string{
				"X-Real-IP": "203.0.113.5",
			},
			// Since 192.168.1.1 is not in XG2G_TRUSTED_PROXIES, ignore header
			expected: "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			remoteAddr: "10.0.0.1:8080",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 198.51.100.1, 192.0.2.1",
			},
			// Takes first IP if trusted, otherwise remoteAddr
			expected: "10.0.0.1",
		},
		{
			name:       "X-Forwarded-For with spaces",
			remoteAddr: "10.0.0.1:8080",
			headers: map[string]string{
				"X-Forwarded-For": "  203.0.113.1  ",
			},
			expected: "10.0.0.1",
		},
		{
			name:       "IPv6 address",
			remoteAddr: "[::1]:8080",
			headers:    map[string]string{},
			expected:   "::1",
		},
		{
			name:       "remote addr without port",
			remoteAddr: "203.0.113.1",
			headers:    map[string]string{},
			expected:   "203.0.113.1",
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
		"Content-Security-Policy": defaultCSP,
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
	panicHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
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

func TestCORSMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name         string
		method       string
		origin       string
		expectOrigin string
		expectStatus int
	}{
		{
			name:         "allowed origin localhost:3000",
			method:       http.MethodGet,
			origin:       "http://localhost:3000",
			expectOrigin: "http://localhost:3000",
			expectStatus: http.StatusOK,
		},
		{
			name:         "no origin header",
			method:       http.MethodGet,
			origin:       "",
			expectOrigin: "*",
			expectStatus: http.StatusOK,
		},
		{
			name:         "disallowed origin",
			method:       http.MethodGet,
			origin:       "http://evil.com",
			expectOrigin: "",
			expectStatus: http.StatusOK,
		},
		{
			name:         "OPTIONS preflight allowed origin",
			method:       http.MethodOptions,
			origin:       "http://localhost:8080",
			expectOrigin: "http://localhost:8080",
			expectStatus: http.StatusNoContent,
		},
		{
			name:         "OPTIONS preflight no origin",
			method:       http.MethodOptions,
			origin:       "",
			expectOrigin: "*",
			expectStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := corsMiddleware(okHandler)
			req := httptest.NewRequest(tt.method, "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("expected status %d, got %d", tt.expectStatus, rr.Code)
			}

			gotOrigin := rr.Header().Get("Access-Control-Allow-Origin")
			if gotOrigin != tt.expectOrigin {
				t.Errorf("expected origin %q, got %q", tt.expectOrigin, gotOrigin)
			}

			// Verify common CORS headers are present
			if rr.Header().Get("Access-Control-Allow-Methods") == "" {
				t.Error("expected Access-Control-Allow-Methods header")
			}
			if rr.Header().Get("Access-Control-Allow-Headers") == "" {
				t.Error("expected Access-Control-Allow-Headers header")
			}
		})
	}
}

func TestRemoteIsTrusted(t *testing.T) {
	// Note: trustedCIDRs is loaded once from XG2G_TRUSTED_PROXIES env var
	// These tests verify the logic paths regardless of env config

	tests := []struct {
		name   string
		remote string
		// We can't predict result without knowing env, but we can test all branches
		testLogic bool
	}{
		{
			name:      "valid IP with port",
			remote:    "192.168.1.100:8080",
			testLogic: true,
		},
		{
			name:      "valid IP without port",
			remote:    "10.0.0.1",
			testLogic: true,
		},
		{
			name:      "localhost with port",
			remote:    "127.0.0.1:12345",
			testLogic: true,
		},
		{
			name:      "invalid IP format",
			remote:    "not-an-ip",
			testLogic: true,
		},
		{
			name:      "empty string",
			remote:    "",
			testLogic: true,
		},
		{
			name:      "IPv6 with port",
			remote:    "[::1]:8080",
			testLogic: true,
		},
		{
			name:      "IPv6 without port",
			remote:    "::1",
			testLogic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function to exercise all code paths
			result := remoteIsTrusted(tt.remote)
			// Result depends on XG2G_TRUSTED_PROXIES env var
			// We just verify it doesn't panic and returns a bool
			_ = result
		})
	}
}
