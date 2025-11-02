// SPDX-License-Identifier: MIT

package helpers

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// RequestOptions configures HTTP request creation
type RequestOptions struct {
	Method      string
	Path        string
	Body        io.Reader
	Token       string
	SetOrigin   bool // Automatically set Origin header for CSRF protection
	ExtraHeader map[string]string
}

// DoRequest creates and executes an HTTP request with common test settings.
// It automatically handles CSRF protection by setting the Origin header for
// state-changing requests (POST, PUT, DELETE, PATCH).
//
// Usage:
//
//	resp := helpers.DoRequest(t, ts.Server.URL, helpers.RequestOptions{
//	    Method: http.MethodPost,
//	    Path: "/api/v1/refresh",
//	    Token: "test-token",
//	})
//	defer resp.Body.Close()
func DoRequest(t *testing.T, baseURL string, opts RequestOptions) *http.Response {
	t.Helper()

	// Build full URL
	fullURL := baseURL + opts.Path

	// Create request
	req, err := http.NewRequestWithContext(
		context.Background(),
		opts.Method,
		fullURL,
		opts.Body,
	)
	require.NoError(t, err, "failed to create HTTP request")

	// Set Origin header for CSRF protection on state-changing requests
	if opts.SetOrigin || isStateChangingMethod(opts.Method) {
		req.Header.Set("Origin", baseURL)
	}

	// Set authentication token if provided
	if opts.Token != "" {
		req.Header.Set("X-API-Token", opts.Token)
	}

	// Set extra headers
	for key, value := range opts.ExtraHeader {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "failed to execute HTTP request")

	return resp
}

// isStateChangingMethod returns true for HTTP methods that change state
func isStateChangingMethod(method string) bool {
	return method == http.MethodPost ||
		method == http.MethodPut ||
		method == http.MethodDelete ||
		method == http.MethodPatch
}
