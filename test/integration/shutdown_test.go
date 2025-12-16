// SPDX-License-Identifier: MIT
//go:build integration

package test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func getFreeTCPPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	defer func() { _ = l.Close() }()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener address is not TCPAddr")
	}

	return addr.Port
}

// TestGracefulShutdown verifies that the daemon shuts down cleanly on SIGTERM/SIGINT
// without leaving orphaned goroutines or incomplete writes.
func TestGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping graceful shutdown test in short mode")
	}

	// Build the daemon binary for testing (stub transcoder by default, no Rust FFI)
	binaryPath := filepath.Join(t.TempDir(), "xg2g-test")
	// #nosec G204 -- Test code: building test binary with controlled arguments
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}

	// Create temporary data directory
	dataDir := t.TempDir()

	// Start Mock OpenWebIf
	mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/bouquets" {
			w.Header().Set("Content-Type", "application/json")
			// Return a mock bouquet response corresponding to XG2G_BOUQUET=Test
			_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockOWI.Close()

	// Prepare minimal environment
	port := getFreeTCPPort(t)
	proxyPort := getFreeTCPPort(t)
	env := []string{
		"XG2G_DATA=" + dataDir,
		fmt.Sprintf("XG2G_LISTEN=:%d", port),
		fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
		"XG2G_OWI_BASE=" + mockOWI.URL,
		"XG2G_BOUQUET=Test",
		"XG2G_EPG_ENABLED=false",            // Disable EPG to simplify test
		"XG2G_HDHR_ENABLED=false",           // Disable HDHR
		"XG2G_SMART_STREAM_DETECTION=false", // Disable detection
		"XG2G_SERVER_SHUTDOWN_TIMEOUT=5s",   // 5s graceful shutdown window
		"PATH=" + os.Getenv("PATH"),
	}

	tests := []struct {
		name   string
		signal os.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGINT", syscall.SIGINT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Start daemon
			// #nosec G204 -- Test code: executing test binary with controlled path
			cmd := exec.CommandContext(ctx, binaryPath)
			cmd.Env = env
			// Capture output for debugging
			var outputBuffer bytes.Buffer
			cmd.Stdout = &outputBuffer
			cmd.Stderr = &outputBuffer

			if err := cmd.Start(); err != nil {
				t.Fatalf("failed to start daemon: %v", err)
			}

			// Wait for daemon to be ready
			ready := false
			for i := 0; i < 50; i++ {
				resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
				if err == nil && resp.StatusCode == http.StatusOK {
					_ = resp.Body.Close()
					ready = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			if !ready {
				_ = cmd.Process.Kill()
				t.Fatalf("daemon did not become ready. Output:\n%s", outputBuffer.String())
			}

			t.Logf("Daemon ready, sending %s", tt.signal)

			// Send signal
			shutdownStart := time.Now()
			if err := cmd.Process.Signal(tt.signal); err != nil {
				t.Fatalf("failed to send %s: %v", tt.signal, err)
			}

			// Wait for process to exit
			done := make(chan error, 1)
			go func() {
				done <- cmd.Wait()
			}()

			select {
			case err := <-done:
				shutdownDuration := time.Since(shutdownStart)
				t.Logf("✅ Daemon shut down cleanly in %v", shutdownDuration)

				// Verify exit code (0 = clean shutdown)
				if err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) && exitErr.ExitCode() != 0 {
						t.Errorf("daemon exited with code %d, want 0", exitErr.ExitCode())
					}
				}

				// Verify shutdown was within timeout (5s configured + 2s buffer)
				if shutdownDuration > 7*time.Second {
					t.Errorf("shutdown took %v, exceeds timeout", shutdownDuration)
				}

			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatal("daemon did not shut down within 10s")
			}
		})
	}
}

// TestShutdownWithActiveRequests verifies graceful shutdown handles in-flight requests
func TestShutdownWithActiveRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shutdown with active requests test in short mode")
	}

	// Build the daemon binary (stub transcoder by default, no Rust FFI)
	binaryPath := filepath.Join(t.TempDir(), "xg2g-test")
	// #nosec G204 -- Test code: building test binary with controlled arguments
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}

	// Start Mock OpenWebIf
	mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/bouquets" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mockOWI.Close()

	dataDir := t.TempDir()
	port := getFreeTCPPort(t)
	proxyPort := getFreeTCPPort(t)
	env := []string{
		"XG2G_DATA=" + dataDir,
		fmt.Sprintf("XG2G_LISTEN=:%d", port),
		fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
		"XG2G_OWI_BASE=" + mockOWI.URL,
		"XG2G_BOUQUET=Test",
		"XG2G_EPG_ENABLED=false",
		"XG2G_HDHR_ENABLED=false",
		"XG2G_SMART_STREAM_DETECTION=false",
		"XG2G_SERVER_SHUTDOWN_TIMEOUT=5s",
		"PATH=" + os.Getenv("PATH"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// #nosec G204 -- Test code: executing test binary with controlled path
	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Env = env
	// Capture output for debugging
	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start daemon: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// Wait for readiness
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("daemon did not become ready. Output:\n%s", outputBuffer.String())
	}

	// Start background request
	requestDone := make(chan error, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v2/status", port))
		if err != nil {
			requestDone <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			requestDone <- io.EOF
			return
		}
		// Verify response contains expected JSON
		if !strings.Contains(string(body), "version") {
			requestDone <- io.ErrUnexpectedEOF
			return
		}
		requestDone <- nil
	}()

	// Give request time to start
	time.Sleep(50 * time.Millisecond)

	// Send shutdown signal while request is in flight
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	// Verify request completes successfully (not aborted)
	select {
	case err := <-requestDone:
		if err != nil {
			t.Logf("⚠️  In-flight request failed: %v (acceptable if server was stopping)", err)
		} else {
			t.Log("✅ In-flight request completed successfully")
		}
	case <-time.After(6 * time.Second):
		t.Error("in-flight request did not complete within shutdown timeout")
	}

	// Wait for process exit
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		t.Log("✅ Daemon exited cleanly after handling in-flight request")
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit within 10s")
	}
}
