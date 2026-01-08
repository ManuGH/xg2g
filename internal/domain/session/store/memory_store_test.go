// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// TestMemoryStore_ScanSessions_NoLockContention verifies that slow callbacks
// don't block concurrent GetSession operations.
func TestMemoryStore_ScanSessions_NoLockContention(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Populate store with 100 sessions
	for i := 0; i < 100; i++ {
		session := &model.SessionRecord{
			SessionID:  string(rune('A' + i)),
			ServiceRef: "test-service",
			State:      model.SessionReady,
		}
		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("PutSession failed: %v", err)
		}
	}

	// Start slow ScanSessions in background
	var scanDone atomic.Bool
	go func() {
		_ = store.ScanSessions(ctx, func(rec *model.SessionRecord) error {
			time.Sleep(10 * time.Millisecond) // Simulate slow callback
			return nil
		})
		scanDone.Store(true)
	}()

	// Give scan time to start
	time.Sleep(5 * time.Millisecond)

	// Verify GetSession is not blocked by slow scan
	start := time.Now()
	_, err := store.GetSession(ctx, "A")
	latency := time.Since(start)

	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	// GetSession should complete quickly (<5ms), not wait for scan to finish
	if latency > 50*time.Millisecond {
		t.Errorf("GetSession blocked for %v (expected <50ms) - lock contention detected!", latency)
	}

	// Wait for scan to complete
	for !scanDone.Load() {
		time.Sleep(10 * time.Millisecond)
	}
}

// TestMemoryStore_ScanSessions_ContextCancellation verifies that ScanSessions
// respects context cancellation during iteration.
func TestMemoryStore_ScanSessions_ContextCancellation(t *testing.T) {
	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())

	// Populate store
	for i := 0; i < 50; i++ {
		session := &model.SessionRecord{
			SessionID:  string(rune('A' + i)),
			ServiceRef: "test-service",
			State:      model.SessionReady,
		}
		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("PutSession failed: %v", err)
		}
	}

	var callbackCount int
	err := store.ScanSessions(ctx, func(rec *model.SessionRecord) error {
		callbackCount++
		if callbackCount == 10 {
			cancel() // Cancel context mid-iteration
		}
		return nil
	})

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	// Should have stopped early (around 10-11 callbacks)
	if callbackCount > 20 {
		t.Errorf("ScanSessions didn't respect context cancellation - called %d times", callbackCount)
	}
}

// BenchmarkMemoryStore_ScanSessions_Concurrent measures throughput under concurrent
// ScanSessions (slow callbacks) and GetSession operations.
func BenchmarkMemoryStore_ScanSessions_Concurrent(b *testing.B) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Populate store with 1000 sessions
	for i := 0; i < 1000; i++ {
		session := &model.SessionRecord{
			SessionID:  string(rune(i)),
			ServiceRef: "test-service",
			State:      model.SessionReady,
		}
		_ = store.PutSession(ctx, session)
	}

	b.ResetTimer()

	// Run concurrent operations
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Slow ScanSessions
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			_ = store.ScanSessions(ctx, func(rec *model.SessionRecord) error {
				time.Sleep(100 * time.Microsecond) // Simulate slow callback
				return nil
			})
		}
	}()

	// Goroutine 2: Fast GetSession operations
	go func() {
		defer wg.Done()
		for i := 0; i < b.N*100; i++ {
			_, _ = store.GetSession(ctx, string(rune(i%1000)))
		}
	}()

	wg.Wait()
}

// TestMemoryStore_ScanSessions_IsolatedSnapshot verifies that modifications
// during scan don't affect the snapshot.
func TestMemoryStore_ScanSessions_IsolatedSnapshot(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// Populate store
	for i := 0; i < 10; i++ {
		session := &model.SessionRecord{
			SessionID:  string(rune('A' + i)),
			ServiceRef: "test-service",
			State:      model.SessionReady,
		}
		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("PutSession failed: %v", err)
		}
	}

	var scannedIDs []string
	err := store.ScanSessions(ctx, func(rec *model.SessionRecord) error {
		scannedIDs = append(scannedIDs, rec.SessionID)

		// Modify store during scan
		if len(scannedIDs) == 5 {
			newSession := &model.SessionRecord{
				SessionID:  "Z",
				ServiceRef: "new-service",
				State:      model.SessionNew,
			}
			_ = store.PutSession(ctx, newSession)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("ScanSessions failed: %v", err)
	}

	// Should have scanned exactly 10 sessions (snapshot taken before modification)
	if len(scannedIDs) != 10 {
		t.Errorf("Expected 10 sessions scanned, got %d", len(scannedIDs))
	}

	// "Z" should not be in scanned IDs (added after snapshot)
	for _, id := range scannedIDs {
		if id == "Z" {
			t.Errorf("Snapshot was not isolated - new session 'Z' appeared in scan")
		}
	}
}
