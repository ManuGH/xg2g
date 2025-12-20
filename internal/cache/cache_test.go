// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// SPDX-License-Identifier: MIT

package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryCache_GetSet(t *testing.T) {
	cache := NewMemoryCache(0) // No cleanup for this test

	// Set a value
	cache.Set("key1", "value1", 5*time.Minute)

	// Get the value
	val, ok := cache.Get("key1")
	require.True(t, ok, "expected to find key1")
	assert.Equal(t, "value1", val)

	// Get non-existent key
	_, ok = cache.Get("nonexistent")
	assert.False(t, ok, "expected not to find nonexistent key")
}

func TestMemoryCache_Expiration(t *testing.T) {
	cache := NewMemoryCache(0)

	// Set with very short TTL
	cache.Set("shortlived", "value", 50*time.Millisecond)

	// Immediately retrieve - should exist
	val, ok := cache.Get("shortlived")
	require.True(t, ok)
	assert.Equal(t, "value", val)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired
	_, ok = cache.Get("shortlived")
	assert.False(t, ok, "expected key to be expired")
}

func TestMemoryCache_Delete(t *testing.T) {
	cache := NewMemoryCache(0)

	cache.Set("key1", "value1", 5*time.Minute)

	// Verify it exists
	_, ok := cache.Get("key1")
	require.True(t, ok)

	// Delete it
	cache.Delete("key1")

	// Verify it's gone
	_, ok = cache.Get("key1")
	assert.False(t, ok)
}

func TestMemoryCache_Clear(t *testing.T) {
	cache := NewMemoryCache(0)

	// Add multiple entries
	cache.Set("key1", "value1", 5*time.Minute)
	cache.Set("key2", "value2", 5*time.Minute)
	cache.Set("key3", "value3", 5*time.Minute)

	// Verify stats
	stats := cache.Stats()
	assert.Equal(t, 3, stats.CurrentSize)

	// Clear
	cache.Clear()

	// Verify empty
	stats = cache.Stats()
	assert.Equal(t, 0, stats.CurrentSize)

	_, ok := cache.Get("key1")
	assert.False(t, ok)
}

func TestMemoryCache_Stats(t *testing.T) {
	cache := NewMemoryCache(0)

	// Perform operations
	cache.Set("key1", "value1", 5*time.Minute)
	cache.Set("key2", "value2", 5*time.Minute)

	cache.Get("key1")        // Hit
	cache.Get("key1")        // Hit
	cache.Get("nonexistent") // Miss

	stats := cache.Stats()
	assert.Equal(t, int64(2), stats.Hits)
	assert.Equal(t, int64(1), stats.Misses)
	assert.Equal(t, int64(2), stats.Sets)
	assert.Equal(t, 2, stats.CurrentSize)
}

func TestMemoryCache_Janitor(t *testing.T) {
	// Create cache with fast cleanup
	cache := NewMemoryCache(50 * time.Millisecond)
	defer cache.(*memoryCache).Stop()

	// Add entries with short TTL
	cache.Set("key1", "value1", 30*time.Millisecond)
	cache.Set("key2", "value2", 30*time.Millisecond)
	cache.Set("longLived", "value3", 10*time.Second)

	// Wait for janitor to clean up
	time.Sleep(150 * time.Millisecond)

	stats := cache.Stats()

	// Should have cleaned up expired entries
	assert.Equal(t, 1, stats.CurrentSize, "janitor should have removed expired entries")
	assert.Greater(t, stats.Evictions, int64(0), "evictions should have occurred")

	// Long-lived entry should still exist
	_, ok := cache.Get("longLived")
	assert.True(t, ok, "long-lived entry should still exist")
}

func TestMemoryCache_ConcurrentAccess(_ *testing.T) {
	cache := NewMemoryCache(1 * time.Minute)
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set("key", i, 5*time.Minute)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get("key")
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// No panic = success
}

func TestNoOpCache(t *testing.T) {
	cache := NewNoOpCache()

	// Should do nothing
	cache.Set("key", "value", 5*time.Minute)

	_, ok := cache.Get("key")
	assert.False(t, ok, "NoOpCache should never return values")

	cache.Delete("key")
	cache.Clear()

	stats := cache.Stats()
	assert.Equal(t, CacheStats{}, stats, "NoOpCache stats should be empty")
}

func BenchmarkMemoryCache_Set(b *testing.B) {
	cache := NewMemoryCache(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set("key", "value", 5*time.Minute)
	}
}

func BenchmarkMemoryCache_Get(b *testing.B) {
	cache := NewMemoryCache(0)
	cache.Set("key", "value", 5*time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("key")
	}
}

func BenchmarkMemoryCache_GetMiss(b *testing.B) {
	cache := NewMemoryCache(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get("nonexistent")
	}
}
