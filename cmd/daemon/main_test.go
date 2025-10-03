// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAtoi(t *testing.T) {
	t.Parallel()
	if got := atoi("42"); got != 42 {
		t.Fatalf("atoi returned %d, want 42", got)
	}
	// Note: failure path triggers log.Fatalf(os.Exit(1)), which would kill the test process.
}

func TestResolveStreamPort(t *testing.T) {
	// Unset var → default
	if err := os.Unsetenv("XG2G_STREAM_PORT"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if port, err := resolveStreamPort(); err != nil || port != defaultStreamPort {
		t.Fatalf("resolveStreamPort default got (%d,%v), want (%d,<nil>)", port, err, defaultStreamPort)
	}

	// Valid override
	t.Setenv("XG2G_STREAM_PORT", "9000")
	if port, err := resolveStreamPort(); err != nil || port != 9000 {
		t.Fatalf("resolveStreamPort valid got (%d,%v), want (9000,<nil>)", port, err)
	}

	// Invalid string
	t.Setenv("XG2G_STREAM_PORT", "not-a-number")
	if _, err := resolveStreamPort(); err == nil {
		t.Fatalf("resolveStreamPort should fail for non-numeric value")
	}

	// Out of range values
	t.Setenv("XG2G_STREAM_PORT", "0")
	if _, err := resolveStreamPort(); err == nil {
		t.Fatalf("resolveStreamPort should fail for 0")
	}
	t.Setenv("XG2G_STREAM_PORT", "70000")
	if _, err := resolveStreamPort(); err == nil {
		t.Fatalf("resolveStreamPort should fail for >65535")
	}
}

func TestResolveMetricsListen(t *testing.T) {
	if err := os.Unsetenv("XG2G_METRICS_LISTEN"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if got := resolveMetricsListen(); got != "" {
		t.Fatalf("resolveMetricsListen default got %q, want empty", got)
	}
	t.Setenv("XG2G_METRICS_LISTEN", ":9090")
	if got := resolveMetricsListen(); got != ":9090" {
		t.Fatalf("resolveMetricsListen set got %q, want :9090", got)
	}
}

func TestResolveOWISettings_DefaultsAndErrors(t *testing.T) {
	// Clear variables to hit defaults
	if err := os.Unsetenv("XG2G_OWI_TIMEOUT_MS"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("XG2G_OWI_RETRIES"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}
	if err := os.Unsetenv("XG2G_OWI_BACKOFF_MS"); err != nil {
		t.Fatalf("failed to unset env: %v", err)
	}

	timeout, retries, backoff, maxBackoff, err := resolveOWISettings()
	if err != nil {
		t.Fatalf("resolveOWISettings unexpected error: %v", err)
	}
	if timeout != defaultOWITimeout {
		t.Fatalf("timeout got %v, want %v", timeout, defaultOWITimeout)
	}
	if retries != defaultOWIRetries {
		t.Fatalf("retries got %d, want %d", retries, defaultOWIRetries)
	}
	if backoff != defaultOWIBackoff {
		t.Fatalf("backoff got %v, want %v", backoff, defaultOWIBackoff)
	}
	// With defaults (retries=3, backoff=500ms) → 8*500ms = 4s (well below the global 30s cap)
	if maxBackoff != 4*time.Second {
		t.Fatalf("maxBackoff got %v, want %v", maxBackoff, 4*time.Second)
	}

	// Invalid timeout
	t.Setenv("XG2G_OWI_TIMEOUT_MS", "-1")
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for negative timeout")
	}

	// Invalid retries (non-numeric)
	t.Setenv("XG2G_OWI_TIMEOUT_MS", "1000") // valid
	t.Setenv("XG2G_OWI_RETRIES", "abc")
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for non-numeric retries")
	}

	// Invalid retries (out of range)
	t.Setenv("XG2G_OWI_RETRIES", "20")
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for retries > max")
	}

	// Invalid backoff (non-numeric)
	t.Setenv("XG2G_OWI_RETRIES", "3") // valid
	t.Setenv("XG2G_OWI_BACKOFF_MS", "not-a-number")
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for non-numeric backoff")
	}

	// Invalid backoff (<=0)
	t.Setenv("XG2G_OWI_BACKOFF_MS", "0")
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for zero backoff")
	}

	// Invalid backoff (>max)
	t.Setenv("XG2G_OWI_BACKOFF_MS", "40000") // 40s > 30s max
	if _, _, _, _, err := resolveOWISettings(); err == nil {
		t.Fatalf("resolveOWISettings should fail for backoff > max")
	}
}

func TestEnsureDataDir(t *testing.T) {
	t.Parallel()
	// Relative path should fail
	if err := ensureDataDir("relative/path"); err == nil {
		t.Fatalf("ensureDataDir should fail for non-absolute path")
	}

	// Absolute non-existing path should be created
	tmp := t.TempDir()
	absNew := filepath.Join(tmp, "nested", "dir")
	if !filepath.IsAbs(absNew) {
		t.Fatalf("expected absolute path, got %q", absNew)
	}
	if err := ensureDataDir(absNew); err != nil {
		t.Fatalf("ensureDataDir failed to create path: %v", err)
	}

	// Existing directory should pass and be writable
	if err := ensureDataDir(tmp); err != nil {
		t.Fatalf("ensureDataDir failed for existing temp dir: %v", err)
	}

	// Symlink to a valid dir should be accepted
	linkPath := filepath.Join(tmp, "link")
	if err := os.Symlink(tmp, linkPath); err == nil {
		if err2 := ensureDataDir(linkPath); err2 != nil {
			t.Fatalf("ensureDataDir rejected valid symlink: %v", err2)
		}
	} else if runtime.GOOS == "windows" {
		t.Logf("symlink creation skipped on Windows: %v", err)
	}

	// System directory should be rejected (use /etc on Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := ensureDataDir("/etc"); err == nil {
			t.Fatalf("ensureDataDir should reject system directory /etc")
		}
	}
}
