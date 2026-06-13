// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestPiconPoolEnqueueStopNoPanic reproduces the shutdown race: a detached
// PrewarmPicons goroutine keeps calling Enqueue while the daemon calls Stop().
// Before the fix, Stop()'s close(jobs) could race a send and panic the process
// with "send on closed channel". Run with -race to also surface the data race.
func TestPiconPoolEnqueueStopNoPanic(t *testing.T) {
	for iter := 0; iter < 50; iter++ {
		pool := NewPiconPool("http://127.0.0.1:0", t.TempDir(), PiconPoolConfig{
			Workers:   2,
			QueueSize: 4,
		})
		pool.Start()

		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				for j := 0; j < 200; j++ {
					// context.Background() mirrors the detached PrewarmPicons ctx,
					// which is never cancelled — so only the pool's own shutdown
					// can stop these sends.
					pool.Enqueue(context.Background(), fmt.Sprintf("ref-%d-%d", i, j))
				}
			}(i)
		}

		// Close the queue while the enqueuers are still hammering it.
		pool.Stop()
		wg.Wait()
	}
}
