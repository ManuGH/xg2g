package ringbuffer

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// LifecycleState tracks startup, recovery, and cleanup states.
type LifecycleState int

const (
	LifecycleStarting LifecycleState = iota
	LifecycleRecovering
	LifecycleCleanupEnabled
	LifecycleDegraded
)

func (s LifecycleState) String() string {
	switch s {
	case LifecycleStarting:
		return "STARTING"
	case LifecycleRecovering:
		return "RECOVERING"
	case LifecycleCleanupEnabled:
		return "CLEANUP_ENABLED"
	case LifecycleDegraded:
		return "DEGRADED"
	default:
		return "UNKNOWN"
	}
}

// LifecycleManager controls startup recovery and delays segment cleanup until recovery completes.
type LifecycleManager struct {
	mu        sync.RWMutex
	state     LifecycleState
	store     *ReservationStore
	index     *SegmentIndex
	readyCond *sync.Cond
}

// NewLifecycleManager initializes a LifecycleManager.
func NewLifecycleManager(store *ReservationStore, index *SegmentIndex) *LifecycleManager {
	lm := &LifecycleManager{
		state: LifecycleStarting,
		store: store,
		index: index,
	}
	lm.readyCond = sync.NewCond(&lm.mu)
	return lm
}

// State returns the current lifecycle state.
func (lm *LifecycleManager) State() LifecycleState {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.state
}

// WaitCleanupEnabled blocks until recovery completes successfully and cleanup is enabled.
func (lm *LifecycleManager) WaitCleanupEnabled() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	for lm.state != LifecycleCleanupEnabled {
		lm.readyCond.Wait()
	}
}

// RunRecovery performs startup recovery of persistent reservations.
func (lm *LifecycleManager) RunRecovery() error {
	lm.mu.Lock()
	lm.state = LifecycleRecovering
	lm.mu.Unlock()

	err := lm.performRecovery()

	lm.mu.Lock()
	if err != nil {
		lm.state = LifecycleDegraded
		lm.mu.Unlock()
		return fmt.Errorf("recovery failed, entering degraded state: %w", err)
	}

	lm.state = LifecycleCleanupEnabled
	lm.readyCond.Broadcast()
	lm.mu.Unlock()
	return nil
}

func (lm *LifecycleManager) performRecovery() error {
	lm.store.mu.Lock()
	defer lm.store.mu.Unlock()

	if lm.store.storagePath == "" {
		return nil
	}

	if _, err := os.Stat(lm.store.storagePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(lm.store.storagePath)
	if err != nil {
		return fmt.Errorf("failed to read reservations JSON: %w", err)
	}

	var loaded map[string]*Reservation
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("failed to unmarshal corrupted reservations JSON: %w", err)
	}

	now := time.Now()
	for id, res := range loaded {
		if now.After(res.ExpiresAt) {
			continue // Skip expired reservations
		}

		var validSegs []SegmentID
		for _, segID := range res.SegmentIDs {
			seg, ok := lm.index.GetByID(segID)
			if !ok {
				continue
			}

			switch seg.Location.Kind {
			case StorageKindDisk:
				// Disk segments survive restart: stat file
				if seg.Location.Path != "" {
					if _, err := os.Stat(seg.Location.Path); err == nil {
						if seg.ReservationIDs == nil {
							seg.ReservationIDs = make(map[string]struct{})
						}
						seg.ReservationIDs[id] = struct{}{}
						seg.State = SegmentReserved
						validSegs = append(validSegs, segID)
					} else {
						seg.State = SegmentMissing
					}
				} else {
					seg.State = SegmentMissing
				}
			case StorageKindRAM:
				// RAM segments NEVER survive process restart!
				seg.State = SegmentMissing
			}
		}

		res.SegmentIDs = validSegs
		if len(validSegs) > 0 {
			lm.store.reservations[id] = res
		}
	}

	return nil
}
