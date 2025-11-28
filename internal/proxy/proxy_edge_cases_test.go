//nolint:noctx // Tests don't require context in HTTP requests
package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

const testHeaderValue = "test-value"

// TestProxyWithQueryParameters tests proxying requests with query params
func TestProxyWithQueryParameters(t *testing.T) {
	receivedQuery := ""
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	// Test with query parameters
	resp, err := http.Get(proxyServer.URL + "/test?foo=bar&baz=qux")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if receivedQuery != "foo=bar&baz=qux" {
		t.Errorf("Expected query 'foo=bar&baz=qux', got '%s'", receivedQuery)
	}
}

// TestProxyWithLargeResponse tests handling of large responses
func TestProxyWithLargeResponse(t *testing.T) {
	// 5MB response
	largeData := strings.Repeat("A", 5*1024*1024)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(largeData))
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	resp, err := http.Get(proxyServer.URL + "/large")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if len(body) != len(largeData) {
		t.Errorf("Expected %d bytes, got %d", len(largeData), len(body))
	}
}

// TestProxyBackendErrors tests handling of backend errors
func TestProxyBackendErrors(t *testing.T) {
	tests := []struct {
		name           string
		backendStatus  int
		expectedStatus int
	}{
		{"404 Not Found", http.StatusNotFound, http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError, http.StatusInternalServerError},
		{"503 Service Unavailable", http.StatusServiceUnavailable, http.StatusServiceUnavailable},
		{"401 Unauthorized", http.StatusUnauthorized, http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.backendStatus)
			}))
			defer backend.Close()

			proxy, err := New(Config{
				ListenAddr: ":0",
				TargetURL:  backend.URL,
				Logger:     zerolog.New(io.Discard),
			})
			if err != nil {
				t.Fatalf("Failed to create proxy: %v", err)
			}

			proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
			defer proxyServer.Close()

			resp, err := http.Get(proxyServer.URL + "/test")
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
		})
	}
}

// TestProxyWithCustomHeaders tests header forwarding (both request and response)
func TestProxyWithCustomHeaders(t *testing.T) {
	receivedHeaders := http.Header{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("X-Custom-Response", testHeaderValue)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, proxyServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("X-Custom-Header", testHeaderValue)
	req.Header.Set("User-Agent", "xg2g-test/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Verify request headers were forwarded
	if receivedHeaders.Get("X-Custom-Header") != testHeaderValue {
		t.Error("Custom header not forwarded")
	}
	if receivedHeaders.Get("User-Agent") != "xg2g-test/1.0" {
		t.Error("User-Agent not forwarded")
	}

	// Verify response headers were returned
	if resp.Header.Get("X-Custom-Response") != testHeaderValue {
		t.Errorf("Response header not returned: got %q", resp.Header.Get("X-Custom-Response"))
	}
}

// TestProxyUnsupportedMethods tests handling of unsupported HTTP methods
func TestProxyUnsupportedMethods(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), method, proxyServer.URL+"/test", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			// All methods should be proxied successfully
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", method, resp.StatusCode)
			}
		})
	}
}

// TestShutdownWithActiveConnections tests graceful shutdown with active requests
func TestShutdownWithActiveConnections(t *testing.T) {
	// Backend with slow response
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Start server

	go func() {
		_ = proxy.Start()
	}()

	time.Sleep(50 * time.Millisecond)

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	// Start a slow request
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := http.Get(proxyServer.URL + "/slow")
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.ReadAll(resp.Body)
		}
	}()

	// Give request time to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown with generous timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := proxy.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Wait for request to complete
	select {
	case <-requestDone:
		// Request completed successfully
	case <-time.After(6 * time.Second):
		t.Error("Request didn't complete after shutdown")
	}
}

// TestStartWithInvalidAddress tests error handling for invalid listen addresses
func TestStartWithInvalidAddress(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: "invalid:address:format",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	err = proxy.Start()
	if err == nil {
		t.Error("Expected error for invalid listen address, got nil")
	}

	// Check that error is not just context cancellation
	if errors.Is(err, context.DeadlineExceeded) {
		t.Error("Got context deadline exceeded, expected listen error")
	}
}

// TestServerShutdown_ContextTimeout tests shutdown with context timeout
func TestServerShutdown_ContextTimeout(t *testing.T) {
	logger := zerolog.New(io.Discard)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second) // Long-running handler
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Start server
	go func() { _ = proxy.Start() }()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Make a long-running request
	reqStarted := make(chan struct{})
	go func() {
		close(reqStarted)
		proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
		defer proxyServer.Close()

		resp, err := http.Get(proxyServer.URL + "/test")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-reqStarted
	time.Sleep(30 * time.Millisecond)

	// Shutdown with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = proxy.Shutdown(ctx)
	// We expect either no error (shutdown completed) or context deadline exceeded
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() returned unexpected error: %v", err)
	}
}

// TestServerIntegration_HTTPClientTimeouts tests timeout behavior
func TestServerIntegration_HTTPClientTimeouts(t *testing.T) {
	logger := zerolog.New(io.Discard)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond) // Simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	// Test with short timeout
	client := &http.Client{Timeout: 100 * time.Millisecond}
	resp, err := client.Get(proxyServer.URL + "/test")
	if err == nil {
		_ = resp.Body.Close()
		t.Error("Expected timeout error, got nil")
	}

	// Test with sufficient timeout (generous for CI environments)
	client = &http.Client{Timeout: 2 * time.Second}
	resp, err = client.Get(proxyServer.URL + "/test")
	if err != nil {
		t.Errorf("Request with sufficient timeout failed: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
}
