// SPDX-License-Identifier: MIT
package daemon

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	// Setup test config
	cfg := Config{
		Version:         "test-1.0.0",
		ConfigPath:      "", // No config file
		ListenAddr:      ":0",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    10 * time.Second,
		IdleTimeout:     120 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 15 * time.Second,
	}

	// Create temporary data directory
	tmpDir := t.TempDir()
	os.Setenv("XG2G_DATA", tmpDir)
	defer os.Unsetenv("XG2G_DATA")

	// Create daemon
	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if d == nil {
		t.Fatal("Expected non-nil daemon")
	}

	if d.config.Version != "test-1.0.0" {
		t.Errorf("Expected version test-1.0.0, got %s", d.config.Version)
	}
}

func TestDaemon_StartShutdown(t *testing.T) {
	// Setup
	cfg := Config{
		Version:         "test-1.0.0",
		ConfigPath:      "", // No config file
		ListenAddr:      "127.0.0.1:0", // Random port
		ReadTimeout:     1 * time.Second,
		WriteTimeout:    1 * time.Second,
		IdleTimeout:     10 * time.Second,
		MaxHeaderBytes:  1 << 20,
		ShutdownTimeout: 5 * time.Second,
	}

	tmpDir := t.TempDir()
	os.Setenv("XG2G_DATA", tmpDir)
	defer os.Unsetenv("XG2G_DATA")

	// Disable telemetry for test
	os.Setenv("XG2G_TELEMETRY_ENABLED", "false")
	defer os.Unsetenv("XG2G_TELEMETRY_ENABLED")

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Create test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Start daemon in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- d.Start(ctx, handler)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	cancel()

	// Wait for shutdown
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Start() returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown timeout")
	}
}

func TestDaemon_Shutdown(t *testing.T) {
	cfg := Config{
		Version:         "test-1.0.0",
		ConfigPath:      "", // No config file
		ShutdownTimeout: 5 * time.Second,
	}

	tmpDir := t.TempDir()
	os.Setenv("XG2G_DATA", tmpDir)
	defer os.Unsetenv("XG2G_DATA")

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Create test server
	d.server = &http.Server{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}

	// Start server in background
	go func() {
		_ = d.server.ListenAndServe()
	}()

	time.Sleep(100 * time.Millisecond)

	// Test shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() failed: %v", err)
	}
}

func TestGetEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		want         string
	}{
		{
			name:         "env set",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "custom",
			want:         "custom",
		},
		{
			name:         "env not set",
			key:          "TEST_KEY_MISSING",
			defaultValue: "default",
			envValue:     "",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvOrDefault(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvOrDefault() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"1.0", 1.0},
		{"0.5", 0.5},
		{"0.1", 0.1},
		{"invalid", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFloat(tt.input)
			if got != tt.want {
				t.Errorf("parseFloat(%s) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestWaitForShutdown(t *testing.T) {
	// This is hard to test without actually sending signals
	// Just verify it returns a valid context
	ctx := WaitForShutdown()
	if ctx == nil {
		t.Fatal("WaitForShutdown() returned nil context")
	}

	// Verify context is not already done
	select {
	case <-ctx.Done():
		t.Error("Context should not be done immediately")
	default:
		// Expected
	}
}
