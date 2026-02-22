// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"context"
	"fmt"
	"sync"
)

// sessionRegistry tracks orchestrator-owned goroutines and provides a bounded join on shutdown.
type sessionRegistry struct {
	mu      sync.Mutex
	closing bool
	wg      sync.WaitGroup
}

func (r *sessionRegistry) Go(fn func()) bool {
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return false
	}
	r.wg.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.wg.Done()
		fn()
	}()

	return true
}

func (r *sessionRegistry) CloseAndWait(ctx context.Context) error {
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("session worker drain timeout: %w", ctx.Err())
	}
}
