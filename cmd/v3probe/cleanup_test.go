package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockRoundTripper allows us to mock HTTP responses
type MockRoundTripper struct {
	RoundTripFunc func(req *http.Request) *http.Response
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.RoundTripFunc(req), nil
}

func TestProbeCleanup_Hardened(t *testing.T) {
	// Swap global client
	origClient := httpClient
	defer func() { httpClient = origClient }()

	mockRT := &MockRoundTripper{}
	httpClient = &http.Client{Transport: mockRT}

	// State tracking
	createdID := "test-timer-123"
	deleteCalled := false

	// Mock Logic
	mockRT.RoundTripFunc = func(req *http.Request) *http.Response {
		// 1. Identity/Ref checks (return success)
		if strings.Contains(req.URL.Path, "/services") {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`[{"serviceRef":"1:0:1..."}]`)),
				Header:     make(http.Header),
			}
		}
		// 2. Router Checks (404/405 - simulate RFC7807)
		if strings.Contains(req.URL.Path, "non-existent") {
			return &http.Response{
				StatusCode: 404,
				Body:       io.NopCloser(strings.NewReader(`{"status":404,"type":"system/not_found","title":"Not Found","code":"NOT_FOUND","instance":"` + req.URL.Path + `"}`)),
				Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
			}
		}
		if strings.Contains(req.URL.Path, "auth/session") {
			return &http.Response{
				StatusCode: 405,
				Body:       io.NopCloser(strings.NewReader(`{"status":405,"type":"system/method_not_allowed","title":"Method Not Allowed","code":"METHOD_NOT_ALLOWED","instance":"` + req.URL.Path + `"}`)),
				Header:     http.Header{"Content-Type": []string{"application/problem+json"}},
			}
		}
		// 3. Helper: parseServiceRef sometimes calls GET /services again

		// 4. Timer Create
		if req.Method == "POST" && strings.Contains(req.URL.Path, "/timers") && !strings.Contains(req.URL.Path, "conflicts") {
			return &http.Response{
				StatusCode: 201,
				Body:       io.NopCloser(strings.NewReader(`{"timerId":"` + createdID + `"}`)),
				Header:     make(http.Header),
			}
		}

		// 5. Timer DELETE (The Assert Target)
		if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/timers/"+createdID) {
			deleteCalled = true
			return &http.Response{
				StatusCode: 204,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}
		}

		// 6. Conflicts (pass)
		if strings.Contains(req.URL.Path, "conflicts:preview") {
			return &http.Response{
				StatusCode: 400, // Expected fail-closed behavior from test logic if "garbage input" (empty body)
				Body:       io.NopCloser(strings.NewReader(`{"status":400}`)),
			}
		}

		// Fallback
		return &http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader(`{"error":"mock unhandled path ` + req.URL.Path + `"}`)),
		}
	}

	cfg := ProbeConfig{
		BaseURL:         "http://test-mock",
		FailAfterCreate: "panic",
	}

	// Expect Panic
	assert.PanicsWithValue(t, "simulated panic after create", func() {
		_ = run(cfg)
	})

	// Assert Cleanup
	assert.True(t, deleteCalled, "Cleanup DELETE must be called after panic")
}
