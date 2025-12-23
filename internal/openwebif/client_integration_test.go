// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package openwebif

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHardenedClient_ResponseHeaderTimeout(t *testing.T) {
	// Create a test server that delays writing headers.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// This delay should be longer than the client's ResponseHeaderTimeout.
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create a client with a short ResponseHeaderTimeout.
	// Note: The overall Timeout must be longer than the header timeout to ensure
	// that the header timeout is the one that triggers.
	opts := Options{
		Timeout:               500 * time.Millisecond, // Overall request timeout
		ResponseHeaderTimeout: 50 * time.Millisecond,  // Specific timeout for headers
		MaxRetries:            0,                      // Disable retries for this test
	}
	client := NewWithPort(ts.URL, 0, opts)

	// Make a request that is expected to fail due to the header timeout.
	_, err := client.Bouquets(context.Background())

	// Check that an error occurred and that it's a timeout error.
	if err == nil {
		t.Fatal("expected a timeout error, but got nil")
	}

	// The error should be related to the context deadline being exceeded,
	// as the transport's timeout will cancel the request's context.
	if !isTimeoutError(err) {
		t.Errorf("expected a timeout error, but got: %v", err)
	}
}

// isTimeoutError checks if the error is a timeout error.
// This is a helper to make the test more robust against different
// underlying error messages.
func isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}
