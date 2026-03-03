// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// setupMiniRedis creates a test Redis server using miniredis.
func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *RedisCache) {
	t.Helper()

	// Create mini Redis server
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	// Create Redis client directly for testing
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	cache := &RedisCache{
		client: client,
		logger: zerolog.Nop(),
	}

	return mr, cache
}

func TestRedisCache_SetGet(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Set a value
	cache.Set("test-key", "test-value", 5*time.Minute)

	// Get the value
	val, found := cache.Get("test-key")
	if !found {
		t.Fatal("expected value to be found")
	}

	if val != "test-value" {
		t.Errorf("expected 'test-value', got %v", val)
	}

	// Check stats
	stats := cache.Stats()
	if stats.Sets != 1 {
		t.Errorf("expected 1 set, got %d", stats.Sets)
	}
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
}

func TestRedisCache_GetMissing(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	val, found := cache.Get("nonexistent")
	if found {
		t.Error("expected value to not be found")
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}

	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestRedisCache_TTL(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Set with 100ms TTL
	cache.Set("ttl-key", "ttl-value", 100*time.Millisecond)

	// Should be available immediately
	val, found := cache.Get("ttl-key")
	if !found {
		t.Fatal("expected value to be found immediately")
	}
	if val != "ttl-value" {
		t.Errorf("expected 'ttl-value', got %v", val)
	}

	// Fast-forward time in miniredis
	mr.FastForward(200 * time.Millisecond)

	// Should be expired
	_, found = cache.Get("ttl-key")
	if found {
		t.Error("expected value to be expired")
	}
}

func TestRedisCache_Delete(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	cache.Set("delete-key", "delete-value", 5*time.Minute)

	// Verify it exists
	_, found := cache.Get("delete-key")
	if !found {
		t.Fatal("expected value to exist before delete")
	}

	// Delete it
	cache.Delete("delete-key")

	// Should no longer exist
	_, found = cache.Get("delete-key")
	if found {
		t.Error("expected value to be deleted")
	}
}

func TestRedisCache_Clear(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Set multiple values
	cache.Set("key1", "value1", 5*time.Minute)
	cache.Set("key2", "value2", 5*time.Minute)
	cache.Set("key3", "value3", 5*time.Minute)

	// Verify they exist
	stats := cache.Stats()
	if stats.CurrentSize != 3 {
		t.Fatalf("expected 3 items, got %d", stats.CurrentSize)
	}

	// Clear cache
	cache.Clear()

	// All should be gone
	stats = cache.Stats()
	if stats.CurrentSize != 0 {
		t.Errorf("expected 0 items after clear, got %d", stats.CurrentSize)
	}

	_, found := cache.Get("key1")
	if found {
		t.Error("expected key1 to be cleared")
	}
}

func TestRedisCache_ComplexData(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Test with map
	data := map[string]interface{}{
		"name":  "test",
		"count": float64(42), // JSON numbers are float64
		"items": []interface{}{"a", "b", "c"},
	}

	cache.Set("complex", data, 5*time.Minute)

	val, found := cache.Get("complex")
	if !found {
		t.Fatal("expected complex data to be found")
	}

	// Verify the structure
	retrieved, ok := val.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", val)
	}

	if retrieved["name"] != "test" {
		t.Errorf("expected name='test', got %v", retrieved["name"])
	}
	if retrieved["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", retrieved["count"])
	}
}

func TestRedisCache_Stats(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Perform various operations
	cache.Set("k1", "v1", 5*time.Minute)
	cache.Set("k2", "v2", 5*time.Minute)
	cache.Get("k1")       // Hit
	cache.Get("k1")       // Hit
	cache.Get("nonexist") // Miss
	cache.Get("nonexist") // Miss

	stats := cache.Stats()

	if stats.Sets != 2 {
		t.Errorf("expected 2 sets, got %d", stats.Sets)
	}
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.CurrentSize != 2 {
		t.Errorf("expected size=2, got %d", stats.CurrentSize)
	}
}

func TestRedisCache_HealthCheck(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	ctx := context.Background()

	// Should be healthy
	if err := cache.HealthCheck(ctx); err != nil {
		t.Errorf("expected healthy Redis, got error: %v", err)
	}

	// Close Redis
	mr.Close()

	// Should fail health check
	if err := cache.HealthCheck(ctx); err == nil {
		t.Error("expected health check to fail after Redis shutdown")
	}
}

func TestRedisCache_ConcurrentAccess(t *testing.T) {
	mr, cache := setupMiniRedis(t)
	defer mr.Close()

	// Multiple goroutines accessing cache concurrently
	const numGoroutines = 10
	const numOps = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOps; j++ {
				key := "concurrent-key"
				cache.Set(key, id, 5*time.Minute)
				cache.Get(key)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify stats
	stats := cache.Stats()
	expectedSets := int64(numGoroutines * numOps)

	if stats.Sets != expectedSets {
		t.Errorf("expected %d sets, got %d", expectedSets, stats.Sets)
	}
}

func BenchmarkRedisCache_Set(b *testing.B) {
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &RedisCache{client: client, logger: zerolog.Nop()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("bench-key", "bench-value", 5*time.Minute)
	}
}

func BenchmarkRedisCache_Get(b *testing.B) {
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &RedisCache{client: client, logger: zerolog.Nop()}

	// Populate cache
	cache.Set("bench-key", "bench-value", 5*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("bench-key")
	}
}
