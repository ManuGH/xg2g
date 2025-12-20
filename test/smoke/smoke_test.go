// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSmoke(t *testing.T) {
	// 0. Start Mock OpenWebIF
	mockOWI := http.NewServeMux()

	// Mock Bouquets response for startup check
	mockOWI.HandleFunc("/api/bouquets", func(w http.ResponseWriter, r *http.Request) {
		t.Log("Mock OWI: /api/bouquets called")
		w.Header().Set("Content-Type", "application/json")
		// Return a bouquet named "SmokeTest"
		w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.smoketest.tv\" ORDER BY bouquet", "SmokeTest"]]}`))
	})

	// Mock Services response for refresh
	mockOWI.HandleFunc("/api/getservices", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Mock OWI: /api/getservices called with %s", r.URL.Query().Get("sRef"))
		w.Header().Set("Content-Type", "application/json")
		// Simple service list
		w.Write([]byte(`{
				"services": [
					{
						"servicename": "TestChannel1",
						"servicereference": "1:0:1:1:1:1:1:0:0:0:"
					},
					{
						"servicename": "TestChannel2",
						"servicereference": "1:0:1:2:2:2:2:0:0:0:"
					}
				]
			}`))
	})

	// Mock EPG to prevent 404s and timeouts
	emptyEPG := []byte(`{"events": []}`)
	mockOWI.HandleFunc("/api/epgservice", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyEPG)
	})
	mockOWI.HandleFunc("/web/epgservice", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyEPG)
	})

	mockServer := &http.Server{
		Addr:    "127.0.0.1:0", // Random port
		Handler: mockOWI,
	}

	// Start mock server listener to get port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen for mock server: %v", err)
	}
	mockAddr := fmt.Sprintf("http://%s", listener.Addr().String())
	t.Logf("Mock OpenWebIF running at %s", mockAddr)

	go mockServer.Serve(listener)
	defer mockServer.Close()

	// 1. Build or locate binary
	var binPath string
	if envBin := os.Getenv("XG2G_SMOKE_BIN"); envBin != "" {
		// Use pre-built binary
		absBin, err := filepath.Abs(envBin)
		if err != nil {
			t.Fatalf("Failed to resolve XG2G_SMOKE_BIN: %v", err)
		}
		binPath = absBin
		t.Logf("Using pre-built binary at %s", binPath)
	} else {
		// Build from source (fallback)
		rootDir, err := filepath.Abs("../../")
		if err != nil {
			t.Fatalf("Failed to resolve root dir: %v", err)
		}

		binPath = filepath.Join(rootDir, "bin", "xg2g-smoke")
		buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/daemon")
		buildCmd.Dir = rootDir
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		if err := buildCmd.Run(); err != nil {
			t.Fatalf("Failed to build binary: %v", err)
		}
		t.Logf("Binary built at %s", binPath)
		defer os.Remove(binPath) // Cleanup only if we built it
	}

	// 2. Prepare environment
	rootDir, _ := filepath.Abs("../../") // Re-resolve rootDir for data dir
	// We need a valid environment. We assume the test is running in an env
	// where .env might exist or we pass sufficient env vars.
	// For a smoke test on the user's machine, inheriting env is likely desired.
	// But we should use a temporary data directory to avoid overwriting real data.
	dataDir, cleanupDataDir, err := smokeDataDir(rootDir)
	if err != nil {
		t.Fatalf("Failed to resolve smoke data dir: %v", err)
	}
	if cleanupDataDir {
		defer os.RemoveAll(dataDir)
	}
	t.Logf("Using smoke data dir: %s", dataDir)

	// 3. Start the process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = rootDir
	cmd.Env = os.Environ()
	cmd.Env = withEnv(cmd.Env, "XG2G_DATA", dataDir)
	cmd.Env = withEnv(cmd.Env, "XG2G_LISTEN", ":58080") // Use non-standard port
	cmd.Env = withEnv(cmd.Env, "XG2G_LOG_LEVEL", "debug")
	// Point to Mock OWI
	cmd.Env = withEnv(cmd.Env, "XG2G_OWI_BASE", mockAddr)
	cmd.Env = withEnv(cmd.Env, "XG2G_BOUQUET", "SmokeTest") // Must match mock return

	// Avoid port conflicts
	cmd.Env = withEnv(cmd.Env, "XG2G_HDHR_ENABLED", "false") // Disable SSDP
	cmd.Env = withEnv(cmd.Env, "XG2G_PROXY_LISTEN", ":0")    // Random proxy port
	cmd.Env = withEnv(cmd.Env, "XG2G_PROXY_PORT", "0")       // Fallback
	cmd.Env = withEnv(cmd.Env, "XG2G_API_TOKEN", "smoke-secret")
	cmd.Env = withEnv(cmd.Env, "XG2G_METRICS_LISTEN", ":58081") // Metrics port

	// Capture output
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start xg2g: %v", err)
	}
	t.Log("xg2g process started with PID", cmd.Process.Pid)

	// Ensure process is killed on exit
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	// 4. Wait for Health (Startup)
	baseURL := "http://localhost:58080"
	if err := waitForHealth(baseURL, 5*time.Second); err != nil {
		t.Fatalf("Service did not become healthy: %v", err)
	}
	t.Log("Service is healthy")

	// 5. Trigger Refresh / Check Channels
	// Since we configured a valid bouquet and mock server, the initial sync (if any) or a manual refresh should work.
	// In the default config, does it sync on start?
	// xg2g usually syncs on start.
	// We check /lineup.json

	if err := waitForChannels(baseURL, 20*time.Second); err != nil {
		t.Fatalf("Channel check failed: %v", err)
	}

	if err := waitForFile(filepath.Join(dataDir, "playlist.m3u"), 20*time.Second); err != nil {
		t.Fatalf("playlist.m3u not written: %v", err)
	}
	if err := waitForFile(filepath.Join(dataDir, "xmltv.xml"), 20*time.Second); err != nil {
		t.Fatalf("xmltv.xml not written: %v", err)
	}

	// 6. Verify Metrics
	metricsURL := "http://localhost:58081"
	metricsResp, err := http.Get(fmt.Sprintf("%s/metrics", metricsURL))
	if err != nil {
		t.Fatalf("Failed to fetch metrics: %v", err)
	}
	defer metricsResp.Body.Close()

	if metricsResp.StatusCode != http.StatusOK {
		t.Fatalf("Metrics endpoint returned status: %d", metricsResp.StatusCode)
	}

	metricsBody, _ := io.ReadAll(metricsResp.Body)
	metricsStr := string(metricsBody)
	if !strings.Contains(metricsStr, "xg2g_active_streams") {
		t.Errorf("Metrics output missing 'xg2g_active_streams'")
	}
	if !strings.Contains(metricsStr, "xg2g_transcode_errors_total") {
		t.Errorf("Metrics output missing 'xg2g_transcode_errors_total'")
	}

	t.Log("Smoke Test Passed!")
}

func smokeDataDir(rootDir string) (dir string, cleanup bool, err error) {
	configured := strings.TrimSpace(os.Getenv("XG2G_SMOKE_DATA_DIR"))
	if configured == "" {
		tmp, err := os.MkdirTemp("", "xg2g-smoke-data")
		if err != nil {
			return "", false, err
		}
		return tmp, true, nil
	}

	if !filepath.IsAbs(configured) {
		configured = filepath.Join(rootDir, configured)
	}
	configured = filepath.Clean(configured)

	if err := os.MkdirAll(configured, 0o755); err != nil {
		return "", false, err
	}

	return configured, false, nil
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	out = append(out, prefix+value)
	return out
}

func waitForHealth(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for health")
}

func waitForChannels(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	// Since we disabled HDHR (and thus lineup.json), we check the internal API
	url := baseURL + "/api/v2/services"

	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("Authorization", "Bearer smoke-secret")

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			// Response is likely [{"service_name":...}, ...] or similar
			// Let's decode into generic interface and check length
			var channels []interface{}
			if err := json.NewDecoder(resp.Body).Decode(&channels); err == nil {
				resp.Body.Close()
				if len(channels) > 0 {
					return nil
				}
			} else {
				resp.Body.Close()
			}
		} else if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for channels at %s", url)
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for file: %s", path)
}
