package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// TestConcurrentRequests tests race conditions with concurrent proxy requests
func TestConcurrentRequests(t *testing.T) {
	// Create a slow backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond) // Simulate slow response
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer backend.Close()

	// Create proxy
	proxy, err := New(Config{
		ListenAddr: ":0",
		TargetURL:  backend.URL,
		Logger:     zerolog.New(io.Discard),
	})
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Start proxy server
	proxyServer := httptest.NewServer(http.HandlerFunc(proxy.handleRequest))
	defer proxyServer.Close()

	// Send concurrent requests
	const numRequests = 50
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resp, err := http.Get(proxyServer.URL + "/test")
			if err != nil {
				errors <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Request %d: expected status 200, got %d", id, resp.StatusCode)
			}

			// Read response body
			_, err = io.ReadAll(resp.Body)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Request error: %v", err)
	}
}

// TestConcurrentStartShutdown tests race conditions during server lifecycle
func TestConcurrentStartShutdown(t *testing.T) {
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

	// Start server in background

	errCh := make(chan error, 1)
	go func() {
		errCh <- proxy.Start()
	}()

	// Wait a bit for server to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown immediately
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	if err := proxy.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown error: %v", err)
	}

	// Check start error
	select {
	case err := <-errCh:
		// Context cancellation is expected
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Errorf("Start error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Start didn't return after shutdown")
	}
}

// TestContextCancellation tests proper handling of context cancellation
func TestContextCancellation(t *testing.T) {
	// Create a backend that never responds
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Second) // Simulate very slow response
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

	// Create request with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyServer.URL+"/slow", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	// This should timeout
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Do(req)
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		t.Error("Expected timeout error, got nil")
	}
}

// TestMultipleShutdowns tests that multiple shutdowns don't cause panics
func TestMultipleShutdowns(t *testing.T) {
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

	// Start server

	go func() {
		_ = proxy.Start()
	}()

	time.Sleep(50 * time.Millisecond)

	// Call shutdown multiple times
	for i := 0; i < 3; i++ {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		err := proxy.Shutdown(shutdownCtx)
		shutdownCancel()

		if i == 0 && err != nil {
			t.Errorf("First shutdown failed: %v", err)
		}
		// Subsequent shutdowns may error, but shouldn't panic
	}
}

// TestConcurrentHeadRequests tests race conditions with HEAD requests
func TestConcurrentHeadRequests(t *testing.T) {
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

	// Send concurrent HEAD requests
	const numRequests = 100
	var wg sync.WaitGroup
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, proxyServer.URL+"/test", nil)
			if err != nil {
				errors <- err
				return
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", resp.StatusCode)
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("HEAD request error: %v", err)
	}
}
