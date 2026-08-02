// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestManagerRaceConditions(t *testing.T) {
	mgr := NewManager(ManagerConfig{SweepInterval: 10 * time.Millisecond})
	defer mgr.Close()

	const numWorkers = 50
	const opsPerWorker = 100
	var wg sync.WaitGroup

	ctx := context.Background()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		workerID := Owner(fmt.Sprintf("worker-%d", i))
		scope := Scope(fmt.Sprintf("tuner:%d", i%5)) // 5 shared scopes across 50 workers

		go func(wOwner Owner, wScope Scope) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				l, err := mgr.Acquire(ctx, wOwner, wScope, 50*time.Millisecond)
				if err == nil && l != nil {
					// Simulate short work
					time.Sleep(1 * time.Millisecond)
					if j%2 == 0 {
						_, _ = mgr.Renew(l.ID, wOwner, 100*time.Millisecond)
					}
					_, _ = mgr.Release(l.ID, wOwner, ReasonReleasedByOwner)
				} else {
					// Query active leases
					_ = mgr.ActiveLeases(wScope)
				}
			}
		}(workerID, scope)
	}

	wg.Wait()
}
