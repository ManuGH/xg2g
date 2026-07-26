// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/test/helpers"
)

// TestHardeningSuite covers Security, Resilience, and Configuration Fallbacks
func TestHardeningSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping hardening suite in short mode")
	}

	// 1. Build Daemon Once
	binaryPath := filepath.Join(t.TempDir(), "xg2g-hardening")
	// #nosec G204
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon")
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1") // Ensure race detector works if enabled, or just normal build
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}

	// Helper to get free port
	getPort := func() int {
		return getFreeTCPPort(t)
	}

	// Helper to wait for port
	waitForPort := func(t *testing.T, port int, timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
			if err == nil {
				// Accept 200 OK or 503 Service Unavailable (daemon running but maybe degraded/not ready)
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable {
					_ = resp.Body.Close()

					return true
				}
				_ = resp.Body.Close()

			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}

	// --- 1. Auth-Strict E2E ---
	t.Run("AuthEnforcement", func(t *testing.T) {
		// Mock OWI just enough to pass startup
		mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/bouquets" {
				_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))

				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer mockOWI.Close()

		port := getPort()
		token := "secret-token-123"

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		cmd.Env = helpers.DaemonEnv(t, t.TempDir(), port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_BOUQUET=Test",     // REQUIRED
			"XG2G_API_TOKEN="+token, // ENABLE AUTH — overrides the helper default
			"XG2G_INITIAL_REFRESH=false",
		)

		var outputBuffer bytes.Buffer
		cmd.Stdout = &outputBuffer
		cmd.Stderr = &outputBuffer

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start daemon: %v", err)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()

		}()

		// Wait for startup
		if !waitForPort(t, port, 5*time.Second) {
			t.Fatalf("daemon did not start in time. Output:\n%s", outputBuffer.String())
		}

		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		// A. No Header -> 401
		resp, err := http.Get(baseURL + "/api/v3/system/health")
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 401 Unauthorized for missing token, got %d", resp.StatusCode)
		}

		// B. Invalid Header -> 401. A rejected credential is "not authenticated",
		// not "authenticated but not allowed"; 403 is reserved for a valid token
		// with insufficient scope (see router_parity_test.go). This assertion used
		// to demand 403 against the retired /api/v2 surface.
		req, _ := http.NewRequest("GET", baseURL+"/api/v3/system/health", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 401 Unauthorized for invalid token, got %d", resp.StatusCode)
		}

		// C. Valid Header -> 200 (Health Check)
		req, _ = http.NewRequest("GET", baseURL+"/api/v3/system/health", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 200 OK for valid token, got %d", resp.StatusCode)
		}
	})

	// --- 2. Picon Serving ---
	// /logos/{filename} used to proxy the receiver, so this subtest asserted
	// upstream-500 -> 502 pass-through. Picons are now served from
	// {dataDir}/picons on disk (server_routes_wiring.go), so there is no upstream
	// to fail and the old expectation could never hold. What is worth pinning at
	// this layer is the handler's filename guard: it is the one place a request
	// path reaches the filesystem.
	t.Run("PiconServing", func(t *testing.T) {
		mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/bouquets" {
				_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer mockOWI.Close()

		dataDir := t.TempDir()
		piconDir := filepath.Join(dataDir, "picons")
		if err := os.MkdirAll(piconDir, 0o750); err != nil {
			t.Fatalf("create picon dir: %v", err)
		}
		// Minimal 1x1 PNG; the handler streams bytes, it does not decode them.
		piconPNG := []byte("\x89PNG\r\n\x1a\n" + "fake-picon-body")
		if err := os.WriteFile(filepath.Join(piconDir, "1_0_1_ABCD.png"), piconPNG, 0o600); err != nil {
			t.Fatalf("write picon: %v", err)
		}

		port := getPort()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		cmd.Env = helpers.DaemonEnv(t, dataDir, port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_BOUQUET=Test",
			"XG2G_INITIAL_REFRESH=false",
		)

		var outputBuffer bytes.Buffer
		cmd.Stdout = &outputBuffer
		cmd.Stderr = &outputBuffer

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start daemon: %v", err)
		}
		defer func() { _ = cmd.Process.Kill() }()

		if !waitForPort(t, port, 5*time.Second) {
			t.Fatalf("daemon did not start in time. Output:\n%s", outputBuffer.String())
		}

		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		cases := []struct {
			name       string
			path       string
			wantStatus int
		}{
			{"existing picon", "/logos/1_0_1_ABCD.png", http.StatusOK},
			{"absent picon", "/logos/DEADBEEF.png", http.StatusNotFound},
			{"filename outside the allowed charset", "/logos/not-a-picon.png", http.StatusNotFound},
			{"non-png extension", "/logos/1_0_1_ABCD.txt", http.StatusNotFound},
			{"traversal attempt", "/logos/..%2f..%2fconfig.yaml", http.StatusNotFound},
		}

		for _, tc := range cases {
			resp, err := http.Get(baseURL + tc.path)
			if err != nil {
				t.Logf("Daemon logs:\n%s", outputBuffer.String())
				t.Fatalf("%s: request failed: %v", tc.name, err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				t.Errorf("%s (%s): got %d, want %d", tc.name, tc.path, resp.StatusCode, tc.wantStatus)
				continue
			}
			if tc.wantStatus == http.StatusOK && !bytes.Equal(body, piconPNG) {
				t.Errorf("%s: served body does not match the file on disk", tc.name)
			}
		}
	})

	// --- 3. No FFmpeg Environment ---
	// ffmpeg is a hard startup requirement: the lifecycle preflight refuses to
	// wire services when the configured binary is missing (see
	// internal/health/lifecycle_preflight.go and bootstrap's skipIfNoFFmpeg).
	// This subtest previously asserted the opposite — that the daemon comes up
	// anyway and merely warns — which the product stopped doing. It now pins the
	// contract that actually ships: fail fast, with a diagnostic that names the
	// missing binary rather than a generic wiring error.
	t.Run("MissingFFmpegFailsStartup", func(t *testing.T) {
		// Mock OWI
		mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/bouquets" {
				_, _ = w.Write([]byte(`{"bouquets": [["...", "Test"]]}`))

				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer mockOWI.Close()

		port := getPort()
		proxyPort := getPort()
		for port == proxyPort {
			proxyPort = getPort()
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		// Point at a binary that cannot exist, so the preflight check is the
		// thing under test rather than whatever ffmpeg the host happens to have.
		cmd.Env = helpers.DaemonEnv(t, t.TempDir(), port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_INITIAL_REFRESH=false",
			"XG2G_BOUQUET=Test", // REQUIRED
			"XG2G_FFMPEG_BIN=/nonexistent/ffmpeg",
		)

		// Use buffers for logs
		var stdoutBuf, stderrBuf ThreadSafeBuffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start daemon: %v", err)
		}

		waitErr := make(chan error, 1)
		go func() { waitErr <- cmd.Wait() }()

		select {
		case err := <-waitErr:
			if err == nil {
				t.Fatalf("daemon exited 0 with a missing ffmpeg binary; expected a startup failure.\nSTDOUT:\n%s", stdoutBuf.String())
			}
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-waitErr
			t.Fatalf("daemon kept running with a missing ffmpeg binary; expected it to refuse startup.\nSTDOUT:\n%s", stdoutBuf.String())
		}

		if waitForPort(t, port, 500*time.Millisecond) {
			t.Errorf("daemon bound port %d despite failing the ffmpeg preflight", port)
		}

		// The operator-facing diagnostic must name the missing binary; a bare
		// "failed to wire daemon services" would leave the cause unclear.
		output := stdoutBuf.String() + stderrBuf.String()
		if !strings.Contains(output, "ffmpeg binary is not available") {
			t.Errorf("startup failure did not report the missing ffmpeg binary.\nOutput:\n%s", output)
		}
		if !strings.Contains(output, "/nonexistent/ffmpeg") {
			t.Errorf("startup failure did not name the configured path.\nOutput:\n%s", output)
		}
	})
}
