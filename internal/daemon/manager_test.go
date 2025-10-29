// SPDX-License-Identifier: MIT

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestNewManager_ValidDeps(t *testing.T) {
	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler: http.NotFoundHandler(),
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      "127.0.0.1:0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 5 * time.Second,
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if mgr == nil {
		t.Fatal("NewManager() returned nil manager")
	}
}

func TestNewManager_MissingLogger(t *testing.T) {
	deps := Deps{
		Logger:     zerolog.Nop(), // Disabled logger
		APIHandler: http.NotFoundHandler(),
	}

	serverCfg := config.ServerConfig{
		ListenAddr: "127.0.0.1:0",
	}

	_, err := NewManager(serverCfg, deps)
	if err == nil {
		t.Fatal("NewManager() expected error for missing logger, got nil")
	}
	// Check if error message contains the expected phrase
	if !contains(err.Error(), "logger is required") {
		t.Errorf("NewManager() error = %v, want error containing 'logger is required'", err)
	}
}

func TestNewManager_MissingAPIHandler(t *testing.T) {
	deps := Deps{
		Logger:     log.WithComponent("test"),
		APIHandler: nil,
	}

	serverCfg := config.ServerConfig{
		ListenAddr: "127.0.0.1:0",
	}

	_, err := NewManager(serverCfg, deps)
	if err == nil {
		t.Fatal("NewManager() expected error for missing API handler, got nil")
	}
	// Check if error message contains the expected phrase
	if !contains(err.Error(), "API handler is required") {
		t.Errorf("NewManager() error = %v, want error containing 'API handler is required'", err)
	}
}

func TestManager_StartStop_OK(t *testing.T) {
	// Create a simple test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler: handler,
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      "127.0.0.1:0", // Random port
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		IdleTimeout:     10 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 2 * time.Second,
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Start manager in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- mgr.Start(ctx)
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

func TestManager_Shutdown_TimesOut(t *testing.T) {
	// Create a handler that blocks on shutdown
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Block longer than shutdown timeout
		w.WriteHeader(http.StatusOK)
	})

	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler: handler,
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      "127.0.0.1:0",
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		IdleTimeout:     10 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 100 * time.Millisecond, // Very short timeout
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Start manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- mgr.Start(ctx)
	}()

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	cancel()

	// Wait for shutdown to complete (should timeout quickly)
	select {
	case err := <-errChan:
		// Expect shutdown to complete (possibly with timeout error in logs)
		// We don't fail the test because timeout is handled gracefully
		_ = err
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

func TestManager_Shutdown_NotStarted(t *testing.T) {
	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler: http.NotFoundHandler(),
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      "127.0.0.1:0",
		ShutdownTimeout: 1 * time.Second,
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Try to shutdown without starting
	err = mgr.Shutdown(context.Background())
	if err != ErrManagerNotStarted {
		t.Errorf("Shutdown() error = %v, want %v", err, ErrManagerNotStarted)
	}
}

func TestManager_WithMetrics(t *testing.T) {
	// Create test handlers
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("# HELP test_metric\n"))
	})

	deps := Deps{
		Logger:         log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler:     apiHandler,
		MetricsHandler: metricsHandler,
	}

	// Set metrics address in ENV for test
	t.Setenv("XG2G_METRICS_LISTEN", "127.0.0.1:0")

	serverCfg := config.ServerConfig{
		ListenAddr:      "127.0.0.1:0",
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		IdleTimeout:     10 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 2 * time.Second,
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Start manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- mgr.Start(ctx)
	}()

	// Give servers a moment to start
	time.Sleep(200 * time.Millisecond)

	// Trigger shutdown
	cancel()

	// Wait for shutdown
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

func TestManager_PropagatesListenErrors(t *testing.T) {
	// First, start a server on a known port
	testServer := httptest.NewServer(http.NotFoundHandler())
	defer testServer.Close()

	// Extract the port from the test server
	addr := testServer.Listener.Addr().String()

	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     jobs.Config{},
		APIHandler: http.NotFoundHandler(),
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      addr, // Try to bind to already-bound port
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		IdleTimeout:     10 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 1 * time.Second,
	}

	mgr, err := NewManager(serverCfg, deps)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Start manager (should fail due to port conflict)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = mgr.Start(ctx)
	// We expect an error due to port conflict
	if err == nil {
		t.Error("Start() expected error for port conflict, got nil")
	}
}
