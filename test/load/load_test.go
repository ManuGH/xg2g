// SPDX-License-Identifier: MIT

//go:build integration_slow
// +build integration_slow

package load

import (
	"context"
	"fmt"
	
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadBaseline measures baseline performance with realistic mock
func TestLoadBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Setup mock server
	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 3
	mockCfg.ChannelsPerBouquet = 50

	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	// Test configuration
	tmpDir := t.TempDir()
	cfg := jobs.Config{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Bouquet_0",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 10 * time.Second,
		OWIRetries: 3,
		OWIBackoff: 100 * time.Millisecond,
	}

	// Warmup
	ctx := context.Background()
	_, err := jobs.Refresh(ctx, cfg)
	require.NoError(t, err, "Warmup refresh should succeed")

	// Run baseline test
	iterations := 10
	var totalDuration time.Duration

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := jobs.Refresh(ctx, cfg)
		duration := time.Since(start)

		require.NoError(t, err, "Refresh iteration %d should succeed", i)
		totalDuration += duration
	}

	avgDuration := totalDuration / time.Duration(iterations)
	metrics := mock.GetMetrics()

	t.Logf("Baseline Performance:")
	t.Logf("  Average refresh duration: %v", avgDuration)
	t.Logf("  Total requests: %d", metrics.RequestsTotal)
	t.Logf("  Success rate: %.2f%%", float64(metrics.RequestsSuccess)/float64(metrics.RequestsTotal)*100)
	t.Logf("  Average latency: %v", metrics.AverageLatency)

	// Performance assertions
	assert.Less(t, avgDuration, 5*time.Second, "Average refresh should complete in <5s")
	assert.Greater(t, float64(metrics.RequestsSuccess)/float64(metrics.RequestsTotal), 0.95,
		"Success rate should be >95%")
}

// TestLoadConcurrentRefreshes tests concurrent refresh operations
func TestLoadConcurrentRefreshes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent load test in short mode")
	}

	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 5
	mockCfg.ChannelsPerBouquet = 100

	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	// Test multiple concurrent clients
	concurrency := 5
	refreshesPerClient := 3

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failureCount atomic.Int64

	start := time.Now()

	for client := 0; client < concurrency; client++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			tmpDir := t.TempDir()
			cfg := jobs.Config{
				Version:    "test",
				DataDir:    tmpDir,
				OWIBase:    server.URL,
				Bouquet:    fmt.Sprintf("Bouquet_%d", clientID%3),
				StreamPort: 8001 + clientID,
				XMLTVPath:  "xmltv.xml",
				OWITimeout: 10 * time.Second,
				OWIRetries: 3,
				OWIBackoff: 100 * time.Millisecond,
			}

			for i := 0; i < refreshesPerClient; i++ {
				ctx := context.Background()
				_, err := jobs.Refresh(ctx, cfg)
				if err != nil {
					failureCount.Add(1)
					t.Logf("Client %d refresh %d failed: %v", clientID, i, err)
				} else {
					successCount.Add(1)
				}
			}
		}(client)
	}

	wg.Wait()
	duration := time.Since(start)

	totalOps := int64(concurrency * refreshesPerClient)
	metrics := mock.GetMetrics()

	t.Logf("Concurrent Load Test:")
	t.Logf("  Clients: %d", concurrency)
	t.Logf("  Refreshes per client: %d", refreshesPerClient)
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f ops/sec", float64(totalOps)/duration.Seconds())
	t.Logf("  Successful: %d", successCount.Load())
	t.Logf("  Failed: %d", failureCount.Load())
	t.Logf("  Mock requests: %d", metrics.RequestsTotal)

	// Assertions
	assert.Greater(t, successCount.Load(), int64(0), "Should have successful operations")
	assert.Less(t, duration, 30*time.Second, "Should complete in reasonable time")
}

// TestLoadHighLoad simulates high load with errors
func TestLoadHighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high load test in short mode")
	}

	mockCfg := HighLoadConfig()
	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := jobs.Config{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Bouquet_0",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 5 * time.Second,
		OWIRetries: 3,
		OWIBackoff: 100 * time.Millisecond,
	}

	iterations := 20
	var successCount, failureCount int

	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := jobs.Refresh(ctx, cfg)
		cancel()

		if err != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	metrics := mock.GetMetrics()

	t.Logf("High Load Test:")
	t.Logf("  Iterations: %d", iterations)
	t.Logf("  Success: %d", successCount)
	t.Logf("  Failures: %d", failureCount)
	t.Logf("  Mock config: %.0f%% errors, %.0f%% timeouts",
		mockCfg.ErrorRate*100, mockCfg.TimeoutRate*100)
	t.Logf("  Mock requests: %d (success: %d, error: %d)",
		metrics.RequestsTotal, metrics.RequestsSuccess, metrics.RequestsError)

	// With 5% error rate and retries, we should still have good success rate
	successRate := float64(successCount) / float64(iterations)
	assert.Greater(t, successRate, 0.5,
		"Should maintain >50%% success rate even with failures")
}

// TestLoadUnstableBackend tests resilience against unstable backend
func TestLoadUnstableBackend(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping unstable backend test in short mode")
	}

	mockCfg := UnstableConfig()
	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := jobs.Config{
		Version:    "test",
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Bouquet_0",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 2 * time.Second,
		OWIRetries: 5, // More retries for unstable backend
		OWIBackoff: 200 * time.Millisecond,
		OWIMaxBackoff: 2 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	_, err := jobs.Refresh(ctx, cfg)
	duration := time.Since(start)

	metrics := mock.GetMetrics()

	t.Logf("Unstable Backend Test:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Result: %v", err)
	t.Logf("  Mock config: %.0f%% errors, %.0f%% timeouts",
		mockCfg.ErrorRate*100, mockCfg.TimeoutRate*100)
	t.Logf("  Mock requests: %d (success: %d, error: %d, timeout: %d)",
		metrics.RequestsTotal, metrics.RequestsSuccess,
		metrics.RequestsError, metrics.RequestsTimeout)

	// Should either succeed or fail gracefully (not panic)
	if err != nil {
		t.Logf("  Refresh failed as expected with unstable backend: %v", err)
	} else {
		t.Logf("  Refresh succeeded despite unstable backend")
	}

	// Should not take excessively long
	assert.Less(t, duration, 60*time.Second,
		"Should timeout or complete within reasonable time")
}

// TestLoadMemoryUsage monitors memory usage during load
func TestLoadMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 10
	mockCfg.ChannelsPerBouquet = 200 // Large channel count

	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := t.TempDir()

	// Run multiple refreshes and check for memory leaks
	iterations := 10

	for i := 0; i < iterations; i++ {
		// Use fresh config each time
		cfg := jobs.Config{
			Version:    "test",
			DataDir:    tmpDir,
			OWIBase:    server.URL,
			Bouquet:    "Bouquet_0,Bouquet_1,Bouquet_2",
			StreamPort: 8001,
			XMLTVPath:  "xmltv.xml",
			OWITimeout: 10 * time.Second,
			OWIRetries: 3,
			OWIBackoff: 100 * time.Millisecond,
		}

		ctx := context.Background()
		_, err := jobs.Refresh(ctx, cfg)
		require.NoError(t, err, "Refresh should succeed")

		// Check playlist file size is reasonable
		m3uPath := filepath.Join(tmpDir, "playlist.m3u")
		stat, err := os.Stat(m3uPath)
		require.NoError(t, err, "Playlist file should exist")

		t.Logf("Iteration %d: Generated %d KB playlist", i, stat.Size()/1024)

		// File should exist and be reasonable size (not growing unbounded)
		assert.Greater(t, stat.Size(), int64(1000), "Playlist should have content")
		assert.Less(t, stat.Size(), int64(10*1024*1024), "Playlist should not be huge")
	}
}

// TestLoadEPGWithManyChannels tests EPG fetching under load
func TestLoadEPGWithManyChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping EPG load test in short mode")
	}

	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 3
	mockCfg.ChannelsPerBouquet = 100

	epgCfg := DefaultEPGConfig()
	epgCfg.EventsPerChannel = 48 // 2 days of programs

	mock := NewEPGMockServer(mockCfg, epgCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := jobs.Config{
		Version:           "test",
		DataDir:           tmpDir,
		OWIBase:           server.URL,
		Bouquet:           "Bouquet_0,Bouquet_1",
		StreamPort:        8001,
		XMLTVPath:         "xmltv.xml",
		EPGEnabled:        true,
		EPGDays:           2,
		EPGMaxConcurrency: 10,
		EPGTimeoutMS:      5000,
		EPGRetries:        2,
		OWITimeout:        10 * time.Second,
		OWIRetries:        3,
		OWIBackoff:        100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	status, err := jobs.Refresh(ctx, cfg)
	duration := time.Since(start)

	metrics := mock.GetMetrics()

	t.Logf("EPG Load Test:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Channels: %d", status.Channels)
	t.Logf("  Mock requests: %d", metrics.RequestsTotal)
	t.Logf("  Average latency: %v", metrics.AverageLatency)

	require.NoError(t, err, "EPG refresh should succeed")
	assert.Less(t, duration, 45*time.Second, "EPG fetch should complete in reasonable time")

	// Check XMLTV file was created
	xmltvPath := filepath.Join(tmpDir, "xmltv.xml")
	stat, err := os.Stat(xmltvPath)
	require.NoError(t, err, "XMLTV file should exist")
	assert.Greater(t, stat.Size(), int64(1000), "XMLTV should have content")
}

// BenchmarkRefreshSmall benchmarks small bouquet refresh
func BenchmarkRefreshSmall(b *testing.B) {
	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 1
	mockCfg.ChannelsPerBouquet = 10
	mockCfg.MinLatency = 1 * time.Millisecond
	mockCfg.MaxLatency = 5 * time.Millisecond

	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := b.TempDir()
	cfg := jobs.Config{
		Version:    "bench",
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Bouquet_0",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 5 * time.Second,
		OWIRetries: 1,
		OWIBackoff: 10 * time.Millisecond,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_, err := jobs.Refresh(ctx, cfg)
		if err != nil {
			b.Fatalf("Refresh failed: %v", err)
		}
	}
}

// BenchmarkRefreshLarge benchmarks large bouquet refresh
func BenchmarkRefreshLarge(b *testing.B) {
	mockCfg := DefaultMockConfig()
	mockCfg.BouquetCount = 5
	mockCfg.ChannelsPerBouquet = 200
	mockCfg.MinLatency = 5 * time.Millisecond
	mockCfg.MaxLatency = 20 * time.Millisecond

	mock := NewMockServer(mockCfg)
	server := httptest.NewServer(mock.Handler())
	defer server.Close()

	tmpDir := b.TempDir()
	cfg := jobs.Config{
		Version:    "bench",
		DataDir:    tmpDir,
		OWIBase:    server.URL,
		Bouquet:    "Bouquet_0,Bouquet_1,Bouquet_2",
		StreamPort: 8001,
		XMLTVPath:  "xmltv.xml",
		OWITimeout: 10 * time.Second,
		OWIRetries: 2,
		OWIBackoff: 50 * time.Millisecond,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_, err := jobs.Refresh(ctx, cfg)
		if err != nil {
			b.Fatalf("Refresh failed: %v", err)
		}
	}
}
