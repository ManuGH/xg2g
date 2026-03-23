// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package daemon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/infra/media/stub"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
	"go.uber.org/goleak"
)

// contains is a helper to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func reserveListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve listen addr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitForListen(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("listen timeout")
}

func TestNewManager_ValidDeps(t *testing.T) {
	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     config.AppConfig{},
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

func TestNewManager_EngineEnabledRequiresV3OrchestratorFactory(t *testing.T) {
	deps := Deps{
		Logger:        log.WithComponent("test"),
		Config:        config.AppConfig{Engine: config.EngineConfig{Enabled: true}},
		APIHandler:    http.NotFoundHandler(),
		MediaPipeline: stub.NewAdapter(),
	}

	serverCfg := config.ServerConfig{
		ListenAddr: "127.0.0.1:0",
	}

	_, err := NewManager(serverCfg, deps)
	if err == nil {
		t.Fatal("NewManager() expected error for missing v3 orchestrator factory, got nil")
	}
	if !contains(err.Error(), ErrMissingV3OrchestratorFactory.Error()) {
		t.Errorf("NewManager() error = %v, want error containing %q", err, ErrMissingV3OrchestratorFactory.Error())
	}
}

func TestManager_StartStop_OK(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	// Create a simple test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     config.AppConfig{},
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
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	// Create a handler that blocks on shutdown
	requestStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		select {
		case <-r.Context().Done():
		case <-releaseHandler:
		}
	})

	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     config.AppConfig{},
		APIHandler: handler,
	}

	serverCfg := config.ServerConfig{
		ListenAddr:      reserveListenAddr(t),
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

	if err := waitForListen(serverCfg.ListenAddr, 2*time.Second); err != nil {
		t.Fatalf("server did not start listening: %v", err)
	}

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		client := &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+serverCfg.ListenAddr, nil)
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-requestStarted:
		// Request is in-flight; shutdown should now hit timeout path.
	case <-time.After(2 * time.Second):
		t.Fatal("expected in-flight request before shutdown")
	}

	// Trigger shutdown
	cancel()

	// Wait for shutdown to complete (should timeout quickly)
	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected shutdown timeout error, got nil")
		}
		if !contains(err.Error(), "shutdown errors") && !contains(err.Error(), "context deadline exceeded") {
			t.Fatalf("unexpected shutdown error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}

	close(releaseHandler)

	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked request did not terminate after shutdown")
	}
}

func TestManager_Shutdown_NotStarted(t *testing.T) {
	deps := Deps{
		Logger:     log.WithComponent("test"),
		Config:     config.AppConfig{},
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
	if !errors.Is(err, ErrManagerNotStarted) {
		t.Errorf("Shutdown() error = %v, want %v", err, ErrManagerNotStarted)
	}
}

func TestManager_WithMetrics(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	// Create test handlers
	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# HELP test_metric\n"))
	})

	deps := Deps{
		Logger:         log.WithComponent("test"),
		Config:         config.AppConfig{},
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

func TestManager_StartAPIServer_DoesNotWarnOnLoopbackCleartextTokenAuth(t *testing.T) {
	var logBuf safeBuffer
	testLogger := zerolog.New(&logBuf).With().Timestamp().Logger()

	m := &manager{
		serverCfg: config.ServerConfig{
			ListenAddr:      "127.0.0.1:0",
			ReadTimeout:     1 * time.Second,
			WriteTimeout:    1 * time.Second,
			IdleTimeout:     10 * time.Second,
			MaxHeaderBytes:  1 << 20,
			ShutdownTimeout: 1 * time.Second,
		},
		deps: Deps{
			Logger:     testLogger,
			Config:     config.AppConfig{APIToken: "secret-token"},
			APIHandler: http.NotFoundHandler(),
		},
		logger: testLogger,
	}

	errChan := make(chan error, 1)
	if err := m.startAPIServer(context.Background(), errChan); err != nil {
		t.Fatalf("startAPIServer() error = %v", err)
	}
	t.Cleanup(func() {
		if m.apiServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = m.apiServer.Shutdown(ctx)
		}
	})

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(logBuf.String(), "running with token auth over cleartext HTTP") {
		t.Fatalf("did not expect cleartext token auth warning on loopback listener, got: %s", logBuf.String())
	}
}

func TestManager_StartAPIServer_WarnsOnCleartextTokenAuthPublicListener(t *testing.T) {
	var logBuf safeBuffer
	testLogger := zerolog.New(&logBuf).With().Timestamp().Logger()

	m := &manager{
		serverCfg: config.ServerConfig{
			ListenAddr:      ":0",
			ReadTimeout:     1 * time.Second,
			WriteTimeout:    1 * time.Second,
			IdleTimeout:     10 * time.Second,
			MaxHeaderBytes:  1 << 20,
			ShutdownTimeout: 1 * time.Second,
		},
		deps: Deps{
			Logger:     testLogger,
			Config:     config.AppConfig{APIToken: "secret-token"},
			APIHandler: http.NotFoundHandler(),
		},
		logger: testLogger,
	}

	errChan := make(chan error, 1)
	if err := m.startAPIServer(context.Background(), errChan); err != nil {
		t.Fatalf("startAPIServer() error = %v", err)
	}
	t.Cleanup(func() {
		if m.apiServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = m.apiServer.Shutdown(ctx)
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logBuf.String(), "running with token auth over cleartext HTTP") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected cleartext token auth warning in logs, got: %s", logBuf.String())
}

func TestManager_StartAPIServer_DoesNotWarnWhenTrustedProxiesConfigured(t *testing.T) {
	var logBuf safeBuffer
	testLogger := zerolog.New(&logBuf).With().Timestamp().Logger()

	m := &manager{
		serverCfg: config.ServerConfig{
			ListenAddr:      ":0",
			ReadTimeout:     1 * time.Second,
			WriteTimeout:    1 * time.Second,
			IdleTimeout:     10 * time.Second,
			MaxHeaderBytes:  1 << 20,
			ShutdownTimeout: 1 * time.Second,
		},
		deps: Deps{
			Logger: testLogger,
			Config: config.AppConfig{
				APIToken:       "secret-token",
				TrustedProxies: "10.10.55.12/32",
			},
			APIHandler: http.NotFoundHandler(),
		},
		logger: testLogger,
	}

	errChan := make(chan error, 1)
	if err := m.startAPIServer(context.Background(), errChan); err != nil {
		t.Fatalf("startAPIServer() error = %v", err)
	}
	t.Cleanup(func() {
		if m.apiServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = m.apiServer.Shutdown(ctx)
		}
	})

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(logBuf.String(), "running with token auth over cleartext HTTP") {
		t.Fatalf("did not expect cleartext token auth warning with trusted proxy config, got: %s", logBuf.String())
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
		Config:     config.AppConfig{},
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
