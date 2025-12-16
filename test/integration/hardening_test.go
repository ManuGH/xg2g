//go:build integration

package test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

		// Ensure distinct ports
		port := getPort()
		proxyPort := getPort()
		for port == proxyPort {
			proxyPort = getPort()
		}

		token := "secret-token-123"

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		cmd.Env = []string{
			"XG2G_DATA=" + t.TempDir(),
			fmt.Sprintf("XG2G_LISTEN=:%d", port),
			fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
			"XG2G_OWI_BASE=" + mockOWI.URL,
			"XG2G_BOUQUET=Test",       // REQUIRED
			"XG2G_API_TOKEN=" + token, // ENABLE AUTH
			"XG2G_INITIAL_REFRESH=false",
			"PATH=" + os.Getenv("PATH"),
		}

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
		resp, err := http.Get(baseURL + "/api/v2/system/health")
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 401 Unauthorized for missing token, got %d", resp.StatusCode)
		}

		// B. Invalid Header -> 403
		req, _ := http.NewRequest("GET", baseURL+"/api/v2/system/health", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 403 Forbidden for invalid token, got %d", resp.StatusCode)
		}

		// C. Valid Header -> 200 (Health Check)
		req, _ = http.NewRequest("GET", baseURL+"/api/v2/system/health", nil)
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

	// --- 2. Chaos & Picon Handling ---
	t.Run("PiconResilience", func(t *testing.T) {
		// Mock OWI with faults
		mockOWI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/bouquets":
				_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))

			case "/picon/500_test.png":
				w.WriteHeader(http.StatusInternalServerError)
			case "/picon/timeout_test.png":
				time.Sleep(200 * time.Millisecond) // Client should have shorter timeout or we wait
				w.WriteHeader(http.StatusOK)
			case "/picon/404_test.png":
				w.WriteHeader(http.StatusNotFound)
			default:
				w.WriteHeader(http.StatusOK)
			}
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
		cmd.Env = []string{
			"XG2G_DATA=" + t.TempDir(),
			fmt.Sprintf("XG2G_LISTEN=:%d", port),
			fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
			"XG2G_OWI_BASE=" + mockOWI.URL,
			"XG2G_BOUQUET=Test",
			"XG2G_INITIAL_REFRESH=false",
			"PATH=" + os.Getenv("PATH"),
		}

		var outputBuffer bytes.Buffer
		cmd.Stdout = &outputBuffer
		cmd.Stderr = &outputBuffer

		_ = cmd.Start()
		defer func() { _ = cmd.Process.Kill() }()

		if !waitForPort(t, port, 5*time.Second) {
			t.Fatalf("daemon did not start in time. Output:\n%s", outputBuffer.String())
		}

		baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

		// A. 500 Upstream -> 502 Downstream
		resp, err := http.Get(baseURL + "/logos/500_test.png")
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadGateway {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 502 Bad Gateway for upstream 500, got %d", resp.StatusCode)
		}

		// B. 404 Upstream -> 404 Downstream
		resp, err = http.Get(baseURL + "/logos/404_test.png")
		if err != nil {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Logf("Daemon logs:\n%s", outputBuffer.String())
			t.Errorf("Expected 404 NotFound for upstream 404, got %d", resp.StatusCode)
		}
	})

	// --- 3. No FFmpeg Environment ---
	t.Run("NoFFmpegFallback", func(t *testing.T) {
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
		// Set invalid FFmpeg path to trigger fallback logic
		cmd.Env = []string{
			"XG2G_DATA=" + t.TempDir(),
			fmt.Sprintf("XG2G_LISTEN=:%d", port),
			fmt.Sprintf("XG2G_PROXY_LISTEN=:%d", proxyPort),
			"XG2G_OWI_BASE=" + mockOWI.URL,
			"XG2G_INITIAL_REFRESH=false",
			"XG2G_BOUQUET=Test",                    // REQUIRED
			"XG2G_FFMPEG_PATH=/nonexistent/ffmpeg", // Trigger "FFmpeg not found"
			"PATH=" + os.Getenv("PATH"),
		}

		// Use buffers for logs
		var stdoutBuf, stderrBuf ThreadSafeBuffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start daemon: %v", err)
		}
		defer func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()

		}()

		// Wait for startup
		if !waitForPort(t, port, 5*time.Second) {
			t.Logf("Daemon STDOUT:\n%s", stdoutBuf.String())
			t.Logf("Daemon STDERR:\n%s", stderrBuf.String())
			t.Fatal("daemon failed to start without ffmpeg")
		}

		// Verify warning in stderr
		output := stderrBuf.String()
		if !strings.Contains(output, "FFmpeg not found") && !strings.Contains(output, "ffmpeg") {
			t.Logf("Daemon stderr: %s", output)
			// t.Log("Could not verify stderr log for warning")
		} else {
			t.Log("Verified FFmpeg warning in logs.")
		}
	})
}
