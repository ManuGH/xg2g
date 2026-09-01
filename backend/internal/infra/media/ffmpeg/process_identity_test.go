// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func TestTranscodeProcessIdentity(t *testing.T) {
	var zero TranscodeProcessIdentity
	if !zero.IsZero() {
		t.Errorf("expected zero struct to be IsZero()")
	}

	now := time.Now()
	ident := NewProcessIdentity("job-123", 1, 4567, now)

	if ident.IsZero() {
		t.Errorf("expected initialized struct not to be IsZero()")
	}
	if ident.JobID != "job-123" {
		t.Errorf("expected JobID job-123, got %s", ident.JobID)
	}
	if ident.Generation != 1 {
		t.Errorf("expected Generation 1, got %d", ident.Generation)
	}
	if ident.PID != 4567 {
		t.Errorf("expected PID 4567, got %d", ident.PID)
	}
	if !ident.StartedAt.Equal(now) {
		t.Errorf("expected StartedAt %v, got %v", now, ident.StartedAt)
	}
}

func TestProcessIdentity_OwnershipAndGenerationLifecycle(t *testing.T) {
	logger := zerolog.Nop()
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", "/tmp", nil, logger,
		"5000000", "5M", 5*time.Second, 5*time.Second,
		false, 5*time.Second, 6, 5*time.Second, 10*time.Second, "",
	)

	jobA := "job-alpha"
	handle1 := ports.RunHandle("sess-1-101")
	handle2 := ports.RunHandle("sess-2-102")

	now := time.Now()

	// 1. Initial start for Job A -> Generation 1
	ident1 := adapter.registerProcessIdentity(handle1, jobA, 101, now)
	if ident1.Generation != 1 {
		t.Fatalf("expected generation 1 for new job, got %d", ident1.Generation)
	}
	if ident1.JobID != jobA {
		t.Fatalf("expected jobID %s, got %s", jobA, ident1.JobID)
	}

	// Verify getProcessIdentity returns registered identity
	gotIdent, ok := adapter.getProcessIdentity(handle1)
	if !ok {
		t.Fatalf("expected process identity for handle1")
	}
	if gotIdent.Generation != 1 || gotIdent.PID != 101 {
		t.Fatalf("unexpected process identity: %+v", gotIdent)
	}

	// 2. Second start for SAME Job A (e.g. session replaced after stall) -> Generation 2
	ident2 := adapter.registerProcessIdentity(handle2, jobA, 102, now.Add(2*time.Second))
	if ident2.Generation != 2 {
		t.Fatalf("expected generation 2 for restarted job, got %d", ident2.Generation)
	}
	if ident2.JobID != jobA {
		t.Fatalf("expected jobID %s, got %s", jobA, ident2.JobID)
	}

	// 3. New Job B -> Generation 1
	jobB := "job-beta"
	handleB := ports.RunHandle("sess-b-103")
	identB := adapter.registerProcessIdentity(handleB, jobB, 103, now)
	if identB.Generation != 1 {
		t.Fatalf("expected generation 1 for new job B, got %d", identB.Generation)
	}

	// 4. Verify cleanup in removeActiveProcessLocked removes processIdentity from map
	adapter.mu.Lock()
	adapter.activeProcs[handle1] = nil
	adapter.removeActiveProcessLocked(handle1, false)
	adapter.mu.Unlock()

	_, okAfterCleanup := adapter.getProcessIdentity(handle1)
	if okAfterCleanup {
		t.Fatalf("expected process identity for handle1 to be deleted after removeActiveProcessLocked")
	}

	// Job A generation counter remains preserved for subsequent restarts
	handle3 := ports.RunHandle("sess-3-104")
	ident3 := adapter.registerProcessIdentity(handle3, jobA, 104, now.Add(5*time.Second))
	if ident3.Generation != 3 {
		t.Fatalf("expected generation 3 for third start of job A, got %d", ident3.Generation)
	}
}

func TestProcessIdentity_ConcurrentRegistration(t *testing.T) {
	logger := zerolog.Nop()
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", "/tmp", nil, logger,
		"5000000", "5M", 5*time.Second, 5*time.Second,
		false, 5*time.Second, 6, 5*time.Second, 10*time.Second, "",
	)

	var wg sync.WaitGroup
	const workers = 50
	gens := make(chan uint64, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handle := ports.RunHandle(fmt.Sprintf("handle-%d", idx))
			ident := adapter.registerProcessIdentity(handle, "concurrent-job", idx+1000, time.Now())
			gens <- ident.Generation
			if _, ok := adapter.getProcessIdentity(handle); !ok {
				t.Errorf("expected handle %s to be registered", handle)
			}
		}(i)
	}
	wg.Wait()
	close(gens)

	seen := make(map[uint64]bool)
	for g := range gens {
		if g < 1 || g > uint64(workers) {
			t.Errorf("generation %d out of expected range 1..%d", g, workers)
		}
		if seen[g] {
			t.Errorf("duplicate generation %d detected under concurrency", g)
		}
		seen[g] = true
	}
	if len(seen) != workers {
		t.Fatalf("expected %d unique generations, got %d", workers, len(seen))
	}
}

// newBoundTestAdapter builds an adapter with the production constructor so the
// generation bookkeeping under test is the one the daemon actually runs.
func newBoundTestAdapter() *LocalAdapter {
	return NewLocalAdapter(
		"ffmpeg", "ffprobe", "/tmp", nil, zerolog.Nop(),
		"5000000", "5M", 5*time.Second, 5*time.Second,
		false, 5*time.Second, 6, 5*time.Second, 10*time.Second, "",
	)
}

// retireHandle simulates the process for a handle exiting, which is what makes
// its job eligible for generation eviction.
func retireHandle(a *LocalAdapter, handle ports.RunHandle) {
	a.mu.Lock()
	a.removeActiveProcessLocked(handle, false)
	a.mu.Unlock()
}

func (a *LocalAdapter) generationCountsForTest() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.generations), len(a.generationOrder)
}

// The generation counter has to survive the process it names, so it cannot be
// cleared in removeActiveProcessLocked like every other per-handle map. That is
// what makes it the one map able to grow for the lifetime of the daemon, so the
// bound is the thing that has to hold.
func TestProcessIdentity_GenerationsStayBounded(t *testing.T) {
	adapter := newBoundTestAdapter()
	now := time.Now()

	for i := 0; i < maxTrackedGenerations+64; i++ {
		handle := ports.RunHandle(fmt.Sprintf("handle-%d", i))
		adapter.registerProcessIdentity(handle, fmt.Sprintf("job-%d", i), 1000+i, now)
		retireHandle(adapter, handle)
	}

	gens, order := adapter.generationCountsForTest()
	if gens > maxTrackedGenerations {
		t.Fatalf("generations grew past the bound: %d > %d", gens, maxTrackedGenerations)
	}
	if order != gens {
		t.Fatalf("eviction order desynced from generations: order=%d generations=%d", order, gens)
	}
}

// Eviction may only ever drop a job with nothing running to correlate. A job
// whose process is still alive keeps its counter no matter how many other jobs
// start after it, so a respawn still observes N+1.
func TestProcessIdentity_LiveJobKeepsGenerationUnderPressure(t *testing.T) {
	adapter := newBoundTestAdapter()
	now := time.Now()

	const liveJob = "job-live"
	liveHandle := ports.RunHandle("handle-live")
	if gen := adapter.registerProcessIdentity(liveHandle, liveJob, 4242, now).Generation; gen != 1 {
		t.Fatalf("expected generation 1 for first start, got %d", gen)
	}

	for i := 0; i < maxTrackedGenerations*2; i++ {
		handle := ports.RunHandle(fmt.Sprintf("handle-%d", i))
		adapter.registerProcessIdentity(handle, fmt.Sprintf("job-%d", i), 1000+i, now)
		retireHandle(adapter, handle)
	}

	if gens, _ := adapter.generationCountsForTest(); gens > maxTrackedGenerations {
		t.Fatalf("generations grew past the bound: %d > %d", gens, maxTrackedGenerations)
	}

	retireHandle(adapter, liveHandle)
	respawn := adapter.registerProcessIdentity(ports.RunHandle("handle-live-2"), liveJob, 4243, now.Add(time.Second))
	if respawn.Generation != 2 {
		t.Fatalf("live job lost its counter to eviction: expected generation 2, got %d", respawn.Generation)
	}
}

// A job that respawns is moved to the back of the eviction order, so activity —
// not first-seen order — decides what is dropped.
func TestProcessIdentity_RespawnRefreshesEvictionOrder(t *testing.T) {
	adapter := newBoundTestAdapter()
	now := time.Now()

	const oldJob = "job-oldest"
	firstHandle := ports.RunHandle("handle-oldest")
	adapter.registerProcessIdentity(firstHandle, oldJob, 7000, now)
	retireHandle(adapter, firstHandle)

	// Fill to just under the bound, then respawn the oldest job so it is the
	// most recently spawned entry rather than the first eviction candidate.
	for i := 0; i < maxTrackedGenerations-1; i++ {
		handle := ports.RunHandle(fmt.Sprintf("filler-a-%d", i))
		adapter.registerProcessIdentity(handle, fmt.Sprintf("filler-a-%d", i), 2000+i, now)
		retireHandle(adapter, handle)
	}
	refreshHandle := ports.RunHandle("handle-oldest-2")
	if gen := adapter.registerProcessIdentity(refreshHandle, oldJob, 7001, now).Generation; gen != 2 {
		t.Fatalf("expected generation 2 on respawn, got %d", gen)
	}
	retireHandle(adapter, refreshHandle)

	// Push the pre-refresh fillers out.
	for i := 0; i < maxTrackedGenerations/2; i++ {
		handle := ports.RunHandle(fmt.Sprintf("filler-b-%d", i))
		adapter.registerProcessIdentity(handle, fmt.Sprintf("filler-b-%d", i), 3000+i, now)
		retireHandle(adapter, handle)
	}

	next := adapter.registerProcessIdentity(ports.RunHandle("handle-oldest-3"), oldJob, 7002, now)
	if next.Generation != 3 {
		t.Fatalf("refreshed job was evicted despite recent activity: expected generation 3, got %d", next.Generation)
	}
}

// When every tracked job is running there is no counter that can be dropped
// without resetting a live correlation chain, so the map is allowed over its
// bound instead — capped by the number of concurrent processes.
func TestProcessIdentity_AllLiveJobsSurviveTheBound(t *testing.T) {
	adapter := newBoundTestAdapter()
	now := time.Now()

	const total = maxTrackedGenerations + 16
	for i := 0; i < total; i++ {
		handle := ports.RunHandle(fmt.Sprintf("handle-%d", i))
		adapter.registerProcessIdentity(handle, fmt.Sprintf("job-%d", i), 1000+i, now)
	}

	gens, order := adapter.generationCountsForTest()
	if gens != total {
		t.Fatalf("evicted a live job's counter: expected %d tracked jobs, got %d", total, gens)
	}
	if order != total {
		t.Fatalf("eviction order desynced: expected %d entries, got %d", total, order)
	}
}
