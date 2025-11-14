// SPDX-License-Identifier: MIT

//nolint:noctx // Test code uses simplified HTTP calls for readability
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

const testHeaderValue = "test-value"

// getFreeAddr returns an available local address by binding to ":0" and closing the listener.
// It reduces collisions with hard-coded ports.
func getFreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free addr: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// waitForServer waits until the TCP port at addr accepts connections or the timeout elapses.
//
//nolint:unparam // timeout parameter kept for test flexibility
func waitForServer(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("server not listening on %s (after %v)", addr, timeout)
}

// TestServerStart_ErrorPaths tests Server.Start() error scenarios
func TestServerStart_ErrorPaths(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name       string
		listenAddr string
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "invalid_listen_address",
			listenAddr: "invalid:99999", // Port out of range
			wantErr:    true,
			errMsg:     "listen tcp",
		},
		{
			name:       "address_already_in_use",
			listenAddr: ":0", // We'll bind this first
			wantErr:    true,
			errMsg:     "address already in use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()

			// For "address_already_in_use" test, bind the address first
			var existingListener net.Listener
			if tt.name == "address_already_in_use" {
				var err error
				lc := &net.ListenConfig{}
				existingListener, err = lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatalf("Failed to create existing listener: %v", err)
				}
				defer func() { _ = existingListener.Close() }()
				tt.listenAddr = existingListener.Addr().String()
			}

			srv, err := New(Config{
				ListenAddr: tt.listenAddr,
				TargetURL:  target.URL,
				Logger:     logger,
			})
			if err != nil {
				// Skip if New() itself fails (expected for invalid port)
				if tt.wantErr {
					return
				}
				t.Fatalf("New() failed: %v", err)
			}

			err = srv.Start()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Start() succeeded, want error containing %q", tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Start() failed: %v", err)
			}
		})
	}
}

// TestServerStart_Success tests successful server startup
func TestServerStart_Success(t *testing.T) {
	logger := zerolog.New(io.Discard)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: "127.0.0.1:0", // Use random port
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server in background
	startErr := make(chan error, 1)
	go func() {
		startErr <- srv.Start()
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}

	// Check that Start() returned no error (or http.ErrServerClosed)
	if err := <-startErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Start() returned error: %v", err)
	}
}

// TestServerShutdown_GracefulShutdown tests graceful shutdown behavior
func TestServerShutdown_GracefulShutdown(t *testing.T) {
	logger := zerolog.New(io.Discard)

	testAddr := getFreeAddr(t)

	// Track if target handler completes
	handlerDone := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond) // Simulate slow handler
		w.WriteHeader(http.StatusOK)
		select {
		case handlerDone <- struct{}{}:
		default:
		}
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: testAddr,
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Wait until server is listening
	if err := waitForServer(testAddr, 1*time.Second); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	// Make a request that will take time
	reqStarted := make(chan struct{})
	go func() {
		close(reqStarted)
		resp, err := http.Get(fmt.Sprintf("http://%s/test", testAddr))
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-reqStarted
	time.Sleep(30 * time.Millisecond) // Ensure request is in-flight

	// Shutdown with generous timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}

	// Check Start() error
	if err := <-startErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Start() returned error: %v", err)
	}

	// Verify handler completed
	select {
	case <-handlerDone:
		// Good - handler completed before shutdown
	case <-time.After(500 * time.Millisecond):
		t.Error("Handler did not complete - shutdown may not have been graceful")
	}
}

// TestServerShutdown_ContextTimeout tests shutdown with context timeout
func TestServerShutdown_ContextTimeout(t *testing.T) {
	logger := zerolog.New(io.Discard)

	testAddr := getFreeAddr(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second) // Long-running handler
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: testAddr,
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Wait until server is listening
	if err := waitForServer(testAddr, 1*time.Second); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	// Make a long-running request
	reqStarted := make(chan struct{})
	go func() {
		close(reqStarted)
		resp, err := http.Get(fmt.Sprintf("http://%s/test", testAddr))
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-reqStarted
	time.Sleep(30 * time.Millisecond)

	// Shutdown with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = srv.Shutdown(ctx)
	// We expect either no error (shutdown completed) or context deadline exceeded
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() returned unexpected error: %v", err)
	}

	// Check Start() error - ignore http.ErrServerClosed
	if err := <-startErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Logf("Start() returned error (acceptable after forced shutdown): %v", err)
	}
}

// TestNew_InvalidTargetURL tests various invalid target URL scenarios
func TestNew_InvalidTargetURL(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name      string
		targetURL string
		wantErr   string
	}{
		{
			name:      "empty_scheme",
			targetURL: "://example.com",
			wantErr:   "parse target URL",
		},
		{
			name:      "invalid_characters",
			targetURL: "http://exa mple.com",
			wantErr:   "parse target URL",
		},
		{
			name:      "malformed_url",
			targetURL: "ht!tp://example.com",
			wantErr:   "parse target URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Config{
				ListenAddr: ":18000",
				TargetURL:  tt.targetURL,
				Logger:     logger,
			})
			if err == nil {
				t.Errorf("New() succeeded, want error containing %q", tt.wantErr)
			}
		})
	}
}

// TestNew_InvalidListenAddr tests invalid listen address scenarios
func TestNew_InvalidListenAddr(t *testing.T) {
	logger := zerolog.New(io.Discard)

	tests := []struct {
		name       string
		listenAddr string
		wantErr    string
	}{
		{
			name:       "empty_listen_addr",
			listenAddr: "",
			wantErr:    "listen address is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Config{
				ListenAddr: tt.listenAddr,
				TargetURL:  "http://example.com:17999",
				Logger:     logger,
			})
			if err == nil {
				t.Errorf("New() succeeded, want error containing %q", tt.wantErr)
			}
		})
	}
}

// TestServerIntegration_ReverseProxyHeaders tests that reverse proxy preserves headers
func TestServerIntegration_ReverseProxyHeaders(t *testing.T) {
	logger := zerolog.New(io.Discard)

	testAddr := getFreeAddr(t)

	// Track headers received by target
	var receivedHeaders http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("X-Custom-Response", testHeaderValue)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response"))
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: testAddr,
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Wait until server is listening
	if err := waitForServer(testAddr, 1*time.Second); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Check Start() error
		if err := <-startErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Start() returned error: %v", err)
		}
	}()

	// Make request with custom headers
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/test", testAddr), nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("X-Custom-Header", testHeaderValue)
	req.Header.Set("User-Agent", "test-agent")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Verify request headers were forwarded
	if receivedHeaders.Get("X-Custom-Header") != testHeaderValue {
		t.Errorf("Custom header not forwarded: got %q", receivedHeaders.Get("X-Custom-Header"))
	}

	// Verify response headers were returned
	if resp.Header.Get("X-Custom-Response") != testHeaderValue {
		t.Errorf("Response header not returned: got %q", resp.Header.Get("X-Custom-Response"))
	}
}

// TestServerIntegration_HTTPClientTimeouts tests timeout behavior
func TestServerIntegration_HTTPClientTimeouts(t *testing.T) {
	logger := zerolog.New(io.Discard)

	testAddr := getFreeAddr(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond) // Simulate slow response
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	srv, err := New(Config{
		ListenAddr: testAddr,
		TargetURL:  target.URL,
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start server
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	// Wait until server is listening
	if err := waitForServer(testAddr, 1*time.Second); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Check Start() error
		if err := <-startErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Start() returned error: %v", err)
		}
	}()

	// Test with short timeout
	client := &http.Client{Timeout: 100 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://%s/test", testAddr))
	if err == nil {
		_ = resp.Body.Close()
		t.Error("Expected timeout error, got nil")
	}

	// Test with sufficient timeout (generous for CI environments)
	client = &http.Client{Timeout: 2 * time.Second}
	resp, err = client.Get(fmt.Sprintf("http://%s/test", testAddr))
	if err != nil {
		t.Errorf("Request with sufficient timeout failed: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
}
