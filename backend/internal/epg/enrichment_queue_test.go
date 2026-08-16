// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package epg

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	mu           sync.Mutex
	lookupFn     func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error)
	callsCount   int64
	lastLookedUp ProgrammeFingerprint
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Lookup(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
	atomic.AddInt64(&m.callsCount, 1)
	m.mu.Lock()
	m.lastLookedUp = fp
	fn := m.lookupFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, fp)
	}
	return &EnrichmentData{
		FingerprintKey:     fp.Key(),
		FingerprintVersion: fp.FingerprintVersion,
		MatcherVersion:     CurrentMatcherVersion,
		Status:             MatchStatusFound,
		Identity: ProviderIdentity{
			Provider: "mock",
			ID:       "123",
		},
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}, nil
}

type mockStore struct {
	mu      sync.Mutex
	records map[string]*EnrichmentData
}

func newMockStore() *mockStore {
	return &mockStore{records: make(map[string]*EnrichmentData)}
}

func (m *mockStore) Get(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[fp.Key()]
	return rec, ok, nil
}

func (m *mockStore) Put(ctx context.Context, fp ProgrammeFingerprint, data *EnrichmentData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[fp.Key()] = data
	return nil
}

func TestEnrichmentQueue_Deduplication(t *testing.T) {
	// Provider that holds execution until released
	blockCh := make(chan struct{})
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			select {
			case <-blockCh:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	cfg := QueueConfig{Capacity: 10, WorkerCount: 1, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, q.Start(ctx))
	defer q.Stop()

	fp := ProgrammeFingerprint{NormalizedTitle: "tatort", FingerprintVersion: CurrentFingerprintVersion}

	// First enqueue must succeed
	ok, reason := q.Enqueue(fp)
	assert.True(t, ok)
	assert.Empty(t, reason)

	// Second immediate enqueue with same fingerprint must be rejected as already_active
	ok2, reason2 := q.Enqueue(fp)
	assert.False(t, ok2)
	assert.Equal(t, "already_active", reason2)

	// Release worker
	close(blockCh)

	// Wait for worker to finish processing
	require.Eventually(t, func() bool {
		return q.ActiveKeyCount() == 0
	}, 1*time.Second, 10*time.Millisecond)

	// Now that job finished and key is released, it must be enqueuable again
	ok3, reason3 := q.Enqueue(fp)
	assert.True(t, ok3)
	assert.Empty(t, reason3)
}

func TestEnrichmentQueue_QueueFullNonBlockingAndNoKeyLeak(t *testing.T) {
	worker1Started := make(chan struct{})
	var worker1Once sync.Once
	blockCh := make(chan struct{})
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			worker1Once.Do(func() {
				close(worker1Started)
			})
			<-blockCh
			return nil, nil
		},
	}

	cfg := QueueConfig{Capacity: 1, WorkerCount: 1, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, q.Start(ctx))
	defer q.Stop()

	fp1 := ProgrammeFingerprint{NormalizedTitle: "job 1", FingerprintVersion: CurrentFingerprintVersion}
	fp2 := ProgrammeFingerprint{NormalizedTitle: "job 2", FingerprintVersion: CurrentFingerprintVersion}
	fp3 := ProgrammeFingerprint{NormalizedTitle: "job 3", FingerprintVersion: CurrentFingerprintVersion}

	// fp1 enqueued
	ok1, _ := q.Enqueue(fp1)
	assert.True(t, ok1)

	// Wait until worker is actively holding fp1 inside lookupFn
	select {
	case <-worker1Started:
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not start job 1 in time")
	}

	// fp2 fills the 1-slot buffer
	ok2, _ := q.Enqueue(fp2)
	assert.True(t, ok2)

	// fp3 exceeds buffer: must return immediately (false, "queue_full")
	start := time.Now()
	ok3, reason3 := q.Enqueue(fp3)
	elapsed := time.Since(start)

	assert.False(t, ok3)
	assert.Equal(t, "queue_full", reason3)
	assert.Less(t, elapsed, 50*time.Millisecond, "Enqueue must be non-blocking when queue is full")

	// Verify fp3 was NOT added to activeKeys (no key leak on drop)
	q.mu.Lock()
	_, fp3Leaked := q.activeKeys[fp3.Key()]
	q.mu.Unlock()
	assert.False(t, fp3Leaked, "Dropped job must not leak into activeKeys")

	close(blockCh)
}

func TestEnrichmentQueue_KeyReleasedOnWorkerError(t *testing.T) {
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			return nil, errors.New("network error")
		},
	}

	cfg := QueueConfig{Capacity: 10, WorkerCount: 1, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, q.Start(ctx))
	defer q.Stop()

	fp := ProgrammeFingerprint{NormalizedTitle: "failed job", FingerprintVersion: CurrentFingerprintVersion}

	ok, _ := q.Enqueue(fp)
	assert.True(t, ok)

	// Wait for worker to finish error path
	require.Eventually(t, func() bool {
		return q.ActiveKeyCount() == 0
	}, 1*time.Second, 10*time.Millisecond)

	// Key must be cleanly released despite error
	okRetry, _ := q.Enqueue(fp)
	assert.True(t, okRetry, "Key must be re-enqueuable after failure")
}

func TestEnrichmentQueue_GracefulShutdownLifecycle(t *testing.T) {
	inFlightStarted := make(chan struct{})
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			close(inFlightStarted)
			<-ctx.Done() // Wait for shutdown cancellation
			return nil, ctx.Err()
		},
	}

	cfg := QueueConfig{Capacity: 10, WorkerCount: 2, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	ctx := context.Background()
	require.NoError(t, q.Start(ctx))

	fp := ProgrammeFingerprint{NormalizedTitle: "shutdown test", FingerprintVersion: CurrentFingerprintVersion}
	ok, _ := q.Enqueue(fp)
	assert.True(t, ok)

	// Wait until worker picked up job
	<-inFlightStarted

	// Stop must cleanly cancel worker context and join WaitGroup
	shutdownDone := make(chan struct{})
	go func() {
		q.Stop()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("EnrichmentQueue.Stop() timed out - possible goroutine leak")
	}

	// Post-stop enqueue must return stopped
	okPost, reasonPost := q.Enqueue(fp)
	assert.False(t, okPost)
	assert.Equal(t, "stopped", reasonPost)
}

func TestEnrichmentQueue_ParentContextCancel_StopsEnqueueAndReleasesKeys(t *testing.T) {
	// Tests parent context cancellation without calling Stop() explicitly
	blockCh := make(chan struct{})
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			select {
			case <-blockCh:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	cfg := QueueConfig{Capacity: 10, WorkerCount: 1, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	parentCtx, parentCancel := context.WithCancel(context.Background())
	require.NoError(t, q.Start(parentCtx))

	fp1 := ProgrammeFingerprint{NormalizedTitle: "job 1", FingerprintVersion: CurrentFingerprintVersion}
	fp2 := ProgrammeFingerprint{NormalizedTitle: "job 2", FingerprintVersion: CurrentFingerprintVersion}

	ok1, _ := q.Enqueue(fp1)
	assert.True(t, ok1)
	ok2, _ := q.Enqueue(fp2)
	assert.True(t, ok2)

	assert.Equal(t, 2, q.ActiveKeyCount())

	// Cancel parent context
	parentCancel()

	// Enqueue must immediately reject new items as stopped
	require.Eventually(t, func() bool {
		ok, reason := q.Enqueue(ProgrammeFingerprint{NormalizedTitle: "new job", FingerprintVersion: CurrentFingerprintVersion})
		return !ok && reason == "stopped"
	}, 1*time.Second, 10*time.Millisecond)

	// All queued active keys must be cleanly released (ActiveKeyCount becomes 0)
	require.Eventually(t, func() bool {
		return q.ActiveKeyCount() == 0
	}, 1*time.Second, 10*time.Millisecond)
}

func TestEnrichmentQueue_StopWhileJobsQueued_ReleasesAllKeys(t *testing.T) {
	// Worker blocked on 1 job, 3 jobs queued in buffer
	blockCh := make(chan struct{})
	provider := &mockProvider{
		lookupFn: func(ctx context.Context, fp ProgrammeFingerprint) (*EnrichmentData, error) {
			select {
			case <-blockCh:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	cfg := QueueConfig{Capacity: 10, WorkerCount: 1, JobTimeout: 5 * time.Second}
	q := NewEnrichmentQueue(cfg, newMockStore(), provider)

	ctx := context.Background()
	require.NoError(t, q.Start(ctx))

	for i := 1; i <= 4; i++ {
		fp := ProgrammeFingerprint{NormalizedTitle: string(rune('a' + i)), FingerprintVersion: CurrentFingerprintVersion}
		ok, _ := q.Enqueue(fp)
		assert.True(t, ok)
	}

	assert.Equal(t, 4, q.ActiveKeyCount())

	// Stop must cleanly terminate and drain all active keys
	q.Stop()

	assert.Equal(t, 0, q.ActiveKeyCount(), "All active keys must be released after Stop()")
}
