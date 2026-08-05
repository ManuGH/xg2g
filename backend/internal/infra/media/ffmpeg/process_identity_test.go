// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"os"
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

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handle := ports.RunHandle(os.Getenv("TMP") + string(rune(idx)))
			ident := adapter.registerProcessIdentity(handle, "concurrent-job", idx+1000, time.Now())
			if ident.Generation == 0 {
				t.Errorf("got zero generation under concurrency")
			}
			adapter.getProcessIdentity(handle)
		}(i)
	}
	wg.Wait()
}
