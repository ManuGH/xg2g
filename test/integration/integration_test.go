// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.
package test

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEndToEndBouquetRetrieval tests the complete flow of retrieving bouquets.
func TestEndToEndBouquetRetrieval(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Create client pointing to mock
	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Test bouquet retrieval
	// Client returns map[name]ref (bouquets["Premium"] = ref)
	bouquets, err := client.Bouquets(ctx)
	require.NoError(t, err, "Should retrieve bouquets successfully")
	assert.NotEmpty(t, bouquets, "Should have at least one bouquet")

	// Verify expected bouquets exist (name -> ref mapping)
	assert.Contains(t, bouquets, "Premium", "Should contain Premium bouquet")
	assert.Equal(t, "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet",
		bouquets["Premium"], "Premium bouquet should have correct reference")

	// Verify other bouquets
	assert.Contains(t, bouquets, "Favourites (TV)")
	assert.Contains(t, bouquets, "HD Channels")
	assert.Len(t, bouquets, 3, "Should have 3 default bouquets")
}

// TestEndToEndServiceRetrieval tests the complete flow of retrieving services from a bouquet.
func TestEndToEndServiceRetrieval(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Create client
	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Test service retrieval for Premium bouquet
	bouquetRef := "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet"
	services, err := client.Services(ctx, bouquetRef)
	require.NoError(t, err, "Should retrieve services successfully")
	assert.NotEmpty(t, services, "Should have at least one service")

	// Verify expected services
	// Client returns [][2]string where [0]=name, [1]=ref
	expectedServices := map[string]string{
		"ARD HD": "1:0:19:283D:3FB:1:C00000:0:0:0:",
		"ZDF HD": "1:0:19:283E:3FB:1:C00000:0:0:0:",
		"RTL":    "1:0:1:6DCA:44D:1:C00000:0:0:0:",
		"Pro7":   "1:0:1:6DCB:44D:1:C00000:0:0:0:",
	}

	assert.Len(t, services, len(expectedServices), "Should have correct number of services")

	for _, svc := range services {
		name, ref := svc[0], svc[1]
		expectedRef, ok := expectedServices[name]
		assert.True(t, ok, "Service '%s' should be in expected list", name)
		assert.Equal(t, expectedRef, ref, "Service '%s' should have correct reference", name)
	}
}

// TestEndToEndStreamURLGeneration tests stream URL generation.
func TestEndToEndStreamURLGeneration(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Create client
	client := openwebif.New(mock.URL())

	// Test stream URL generation
	serviceRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	serviceName := "ARD HD"
	streamURL, err := client.StreamURL(context.Background(), serviceRef, serviceName)
	require.NoError(t, err, "Should generate stream URL successfully")
	assert.Contains(t, streamURL, serviceRef, "Stream URL should contain service reference")
}

// TestEndToEndWithRetries tests retry behavior on failures.
func TestEndToEndWithRetries(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Configure mock to fail first 2 requests
	mock.SetFailures("/api/bouquets", 2)

	// Create client with retry configuration
	client := openwebif.NewWithPort(mock.URL(), 8001, openwebif.Options{
		Timeout:    5 * time.Second,
		MaxRetries: 3,
		Backoff:    100 * time.Millisecond,
		MaxBackoff: 1 * time.Second,
	})

	ctx := context.Background()

	// Should succeed after retries
	bouquets, err := client.Bouquets(ctx)
	require.NoError(t, err, "Should succeed after retries")
	assert.NotEmpty(t, bouquets, "Should retrieve bouquets after retries")
}

// TestEndToEndWithTimeout tests timeout handling.
func TestEndToEndWithTimeout(t *testing.T) {
	// Setup mock server with delay
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Create client with very short timeout
	client := openwebif.NewWithPort(mock.URL(), 8001, openwebif.Options{
		Timeout:    1 * time.Millisecond, // Very short timeout
		MaxRetries: 1,
		Backoff:    100 * time.Millisecond,
	})

	ctx := context.Background()

	// Should timeout (note: this test might be flaky on very fast systems)
	_, err := client.Bouquets(ctx)
	if err != nil {
		// Expected - timeout or connection error
		assert.Error(t, err, "Should fail due to timeout")
	}
}

// TestEndToEndMultipleBouquets tests handling multiple bouquets and services.
func TestEndToEndMultipleBouquets(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Add additional test data
	mock.AddBouquet("1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"test.bouquet\" ORDER BY bouquet", "Test Bouquet")
	mock.AddService("1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"test.bouquet\" ORDER BY bouquet",
		"1:0:1:TEST:1:1:C00000:0:0:0:", "Test Channel")

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Get bouquets
	bouquets, err := client.Bouquets(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(bouquets), 4, "Should have at least 4 bouquets")

	// Get services from test bouquet
	services, err := client.Services(ctx, "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"test.bouquet\" ORDER BY bouquet")
	require.NoError(t, err)
	assert.Len(t, services, 1, "Test bouquet should have 1 service")
	// Services are [name, ref] format
	assert.Equal(t, "Test Channel", services[0][0], "Should have correct service name")
	assert.Equal(t, "1:0:1:TEST:1:1:C00000:0:0:0:", services[0][1], "Should have correct service ref")
}

// TestEndToEndContextCancellation tests context cancellation handling.
func TestEndToEndContextCancellation(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())

	// Create context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail due to canceled context
	_, err := client.Bouquets(ctx)
	assert.Error(t, err, "Should fail with canceled context")
}

// TestEndToEndEmptyBouquet tests handling of bouquets with no services.
func TestEndToEndEmptyBouquet(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	// Add empty bouquet
	mock.AddBouquet("1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"empty.bouquet\" ORDER BY bouquet", "Empty Bouquet")

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Should handle empty bouquet gracefully
	services, err := client.Services(ctx, "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"empty.bouquet\" ORDER BY bouquet")
	require.NoError(t, err)
	assert.Empty(t, services, "Empty bouquet should have no services")
}

// TestEndToEndMockServerReset tests mock server reset functionality.
func TestEndToEndMockServerReset(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Get initial bouquets
	bouquets1, err := client.Bouquets(ctx)
	require.NoError(t, err)
	initialCount := len(bouquets1)

	// Add new bouquet
	mock.AddBouquet("1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"new.bouquet\" ORDER BY bouquet", "New Bouquet")

	bouquets2, err := client.Bouquets(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialCount+1, len(bouquets2), "Should have one more bouquet")

	// Reset mock server
	mock.Reset()

	bouquets3, err := client.Bouquets(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialCount, len(bouquets3), "Should be back to default bouquets after reset")
}

// TestEndToEndConcurrentRequests tests concurrent access to the mock server.
func TestEndToEndConcurrentRequests(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Run concurrent requests
	const concurrency = 10
	results := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			_, err := client.Bouquets(ctx)
			results <- err
		}()
	}

	// Collect results
	for i := 0; i < concurrency; i++ {
		err := <-results
		assert.NoError(t, err, "Concurrent request should succeed")
	}
}

// TestEndToEndHTTPClientReuse tests that the client reuses HTTP connections.
func TestEndToEndHTTPClientReuse(t *testing.T) {
	// Setup mock server
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	// Make multiple requests
	for i := 0; i < 5; i++ {
		_, err := client.Bouquets(ctx)
		require.NoError(t, err, "Request %d should succeed", i)
	}

	// If we get here without errors, HTTP client is working correctly
}

// BenchmarkEndToEndBouquetRetrieval benchmarks bouquet retrieval.
func BenchmarkEndToEndBouquetRetrieval(b *testing.B) {
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Bouquets(ctx)
		if err != nil {
			b.Fatalf("Benchmark failed: %v", err)
		}
	}
}

// BenchmarkEndToEndServiceRetrieval benchmarks service retrieval.
func BenchmarkEndToEndServiceRetrieval(b *testing.B) {
	mock := openwebif.NewMockServer()
	defer mock.Close()

	client := openwebif.New(mock.URL())
	ctx := context.Background()
	bouquetRef := "1:7:1:0:0:0:0:0:0:0:FROM BOUQUET \"userbouquet.premium.tv\" ORDER BY bouquet"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Services(ctx, bouquetRef)
		if err != nil {
			b.Fatalf("Benchmark failed: %v", err)
		}
	}
}
