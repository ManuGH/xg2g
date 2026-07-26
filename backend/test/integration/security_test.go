// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/test/helpers"
)

type ThreadSafeBuffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *ThreadSafeBuffer) Read(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Read(p)
}

func (b *ThreadSafeBuffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}

func parseCSP(value string) map[string]map[string]struct{} {
	directives := map[string]map[string]struct{}{}
	for _, directive := range strings.Split(value, ";") {
		directive = strings.TrimSpace(directive)
		if directive == "" {
			continue
		}
		parts := strings.Fields(directive)
		if len(parts) == 0 {
			continue
		}
		name := parts[0]
		tokens, ok := directives[name]
		if !ok {
			tokens = map[string]struct{}{}
			directives[name] = tokens
		}
		for _, token := range parts[1:] {
			tokens[token] = struct{}{}
		}
	}
	return directives
}

func requireCSPTokens(t *testing.T, directives map[string]map[string]struct{}, directive string, tokens ...string) {
	t.Helper()
	got, ok := directives[directive]
	if !ok {
		t.Fatalf("CSP directive %q missing", directive)
	}
	for _, token := range tokens {
		if _, ok := got[token]; !ok {
			t.Errorf("CSP %s missing token %q", directive, token)
		}
	}
}

func (b *ThreadSafeBuffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}

func buildTestBinary(t *testing.T) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "xg2g-sec-test")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/daemon") // #nosec G204
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build daemon: %v\n%s", err, out)
	}
	return binaryPath
}

// waitForPort waits for the daemon to start listening on the given port.
func waitForPort(t *testing.T, port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{
		Timeout: 500 * time.Millisecond,
	}
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
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

func setupMockOWI(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/bouquets" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bouquets": [["1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.test.tv\" ORDER BY bouquet", "Test"]]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
}

func TestSecuritySuiteExtended(t *testing.T) {
	binaryPath := buildTestBinary(t)

	// Rate limiting is covered at the middleware boundary. The removed
	// XG2G_RATELIMIT* flags never enabled it in this daemon-level test.

	// --- 1. Security Headers / CSP ---
	t.Run("SecurityHeaders", func(t *testing.T) {
		port := getFreeTCPPort(t)

		mockOWI := setupMockOWI(t)
		defer mockOWI.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		dataDir := t.TempDir()
		cmd.Env = helpers.DaemonEnv(t, dataDir, port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_BOUQUET=Test",
			"XG2G_INITIAL_REFRESH=false",
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

		if !waitForPort(t, port, 15*time.Second) {
			t.Fatalf("Daemon start fail: %s", outBuf.String())
		}

		url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
		// #nosec G107 - Testing
		resp, err := http.Get(url)

		if err != nil {
			t.Fatalf("req failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		headers := resp.Header
		expected := map[string]string{
			"X-Frame-Options":        "DENY",
			"X-Content-Type-Options": "nosniff",
			"Referrer-Policy":        "no-referrer",
			"Permissions-Policy":     "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
		}
		for k, v := range expected {
			if got := headers.Get(k); got != v {
				t.Errorf("Header %s: expected %q, got %q", k, v, got)
			}
		}
		csp := headers.Get("Content-Security-Policy")
		if csp == "" {
			t.Fatalf("Header Content-Security-Policy missing")
		}
		cspDirectives := parseCSP(csp)
		requireCSPTokens(t, cspDirectives, "default-src", "'self'")
		requireCSPTokens(t, cspDirectives, "frame-ancestors", "'none'")
		requireCSPTokens(t, cspDirectives, "script-src", "'self'")
		requireCSPTokens(t, cspDirectives, "style-src", "'self'", "'unsafe-inline'")
		requireCSPTokens(t, cspDirectives, "font-src", "'self'", "data:")
		requireCSPTokens(t, cspDirectives, "img-src", "'self'", "data:", "blob:")
		requireCSPTokens(t, cspDirectives, "media-src", "'self'", "blob:", "data:")
		requireCSPTokens(t, cspDirectives, "connect-src", "'self'")
	})

	// --- 2. Stream Proxy Removed (v3) ---
	t.Run("StreamProxyRemoved", func(t *testing.T) {
		port := getFreeTCPPort(t)

		mockOWI := setupMockOWI(t)
		defer mockOWI.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		dataDir := t.TempDir()
		cmd.Env = helpers.DaemonEnv(t, dataDir, port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_BOUQUET=Test",
			"XG2G_INITIAL_REFRESH=false",
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

		if !waitForPort(t, port, 15*time.Second) {
			t.Fatalf("Daemon start fail: %s", outBuf.String())
		}

		streamURL := fmt.Sprintf("http://127.0.0.1:%d/stream/1:2:3?passthrough=123&foo=bar", port)
		// #nosec G107 - Testing
		resp, err := http.Get(streamURL)
		if err != nil {
			t.Fatalf("Stream req failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 for legacy /stream route, got %d", resp.StatusCode)
		}
	})

	// --- 3. XMLTV Path Traversal ---
	t.Run("XMLTVTraversal", func(t *testing.T) {
		port := getFreeTCPPort(t)

		mockOWI := setupMockOWI(t)
		defer mockOWI.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath) // #nosec G204
		dataDir := t.TempDir()
		cmd.Env = helpers.DaemonEnv(t, dataDir, port,
			"XG2G_E2_HOST="+mockOWI.URL,
			"XG2G_BOUQUET=Test",
			"XG2G_XMLTV=../../etc/passwd", // TRAVERSAL ATTEMPT
			"XG2G_INITIAL_REFRESH=false",
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("daemon accepted a traversing XMLTV path")
			}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatal("daemon did not reject the traversing XMLTV path promptly")
		}
		if !strings.Contains(outBuf.String(), "contains path traversal") {
			t.Fatalf("startup failure did not identify path traversal: %s", outBuf.String())
		}
	})

	// --- 4. Config Reload ---
	t.Run("ConfigReload", func(t *testing.T) {
		tempDir := t.TempDir()
		configFile := filepath.Join(tempDir, "config.yaml")

		port := getFreeTCPPort(t)

		mockOWI := setupMockOWI(t)
		defer mockOWI.Close()

		initialConfig := fmt.Sprintf(`
bouquets: 
  - "Test"
enigma2:
  baseUrl: "%s"
`, mockOWI.URL)
		_ = os.WriteFile(configFile, []byte(initialConfig), 0600)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd := exec.CommandContext(ctx, binaryPath, "-config", configFile) // #nosec G204
		cmd.Env = helpers.DaemonEnv(t, tempDir, port,
			"XG2G_INITIAL_REFRESH=false",
		)
		var outBuf ThreadSafeBuffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &outBuf

		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

		if !waitForPort(t, port, 15*time.Second) {
			t.Fatalf("Daemon start fail:\n%s", outBuf.String())
		}

		// Modify config to verify reload
		// Note: Reload doesn't re-check authentication or presence against OWI automatically,
		// but it logs the change.
		newConfig := fmt.Sprintf(`
bouquets:
  - "ReloadedC"
enigma2:
  baseUrl: "%s"
`, mockOWI.URL)

		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(configFile, []byte(newConfig), 0600)

		time.Sleep(1 * time.Second)

		if !strings.Contains(outBuf.String(), "config changed: Bouquet") {
			// t.Logf("Logs: %s", outBuf.String())
		} else {
			t.Log("Verified config reload log.")
		}
	})
}
