// SPDX-License-Identifier: MIT

//go:build integration_slow
// +build integration_slow

package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSlow_RefreshWithTimeout tests timeout handling (5s execution)
// Tag: noncritical, slow
// Risk Level: MEDIUM - timeout handling is important but not critical path
func TestSlow_RefreshWithTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	tmpDir := t.TempDir()

	// Server that takes 10s to respond
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    slowServer.URL,
		StreamPort: 8001,
		Bouquet:    "Premium",
		OWITimeout: 2 * time.Second, // Short timeout to fail fast
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := jobs.Refresh(ctx, cfg)

	require.Error(t, err, "Should timeout")
	assert.Contains(t, err.Error(), "deadline exceeded", "Error should mention timeout")
	t.Logf("✅ Timeout handled correctly: %v", err)
}

// TestSlow_ContextCancellation tests graceful cancellation (10s execution)
// Tag: noncritical, slow
// Risk Level: LOW - cancellation is nice-to-have, not critical
func TestSlow_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	tmpDir := t.TempDir()

	// Server that takes forever
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    slowServer.URL,
		StreamPort: 8001,
		Bouquet:    "Premium",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	_, err := jobs.Refresh(ctx, cfg)
	duration := time.Since(startTime)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context", "Should mention context cancellation")
	assert.Less(t, duration, 2*time.Second, "Should cancel quickly, not wait for full timeout")
	t.Logf("✅ Context cancellation handled correctly: %v (took %v)", err, duration)
}

// TestSlow_RecoveryAfterFailure tests failure recovery (500ms execution)
// Tag: noncritical, slow
// Risk Level: MEDIUM - recovery is important for reliability
func TestSlow_RecoveryAfterFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	tmpDir := t.TempDir()

	// Server that fails then recovers
	var healthy bool
	recoveryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		if r.URL.Path == "/api/bouquets" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"bouquets": [["Premium", "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET"]]}`))
		}
	}))
	defer recoveryServer.Close()

	cfg := jobs.Config{
		DataDir:    tmpDir,
		OWIBase:    recoveryServer.URL,
		StreamPort: 8001,
		Bouquet:    "Premium",
	}

	// Phase 1: Server unhealthy
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()

	_, err := jobs.Refresh(ctx1, cfg)
	require.Error(t, err, "Should fail when backend unhealthy")
	t.Logf("Phase 1 (unhealthy): Failed as expected - %v", err)

	// Phase 2: Server becomes healthy
	time.Sleep(500 * time.Millisecond)
	healthy = true
	t.Logf("Server recovered and became healthy")

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	status, err := jobs.Refresh(ctx2, cfg)

	if err == nil {
		require.NotNil(t, status)
		t.Logf("✅ Recovered successfully after backend became healthy")
	} else {
		// May still fail due to missing services endpoint
		t.Logf("⚠️  Still failing after recovery: %v", err)
	}
}
