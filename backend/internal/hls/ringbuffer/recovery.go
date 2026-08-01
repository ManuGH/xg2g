package ringbuffer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type LifecycleState uint8

const (
	LifecycleStarting LifecycleState = iota
	LifecycleRecovering
	LifecycleReady
	LifecycleCleanupEnabled
	LifecycleDegraded
)

func (s LifecycleState) String() string {
	switch s {
	case LifecycleStarting:
		return "STARTING"
	case LifecycleRecovering:
		return "RECOVERING"
	case LifecycleReady:
		return "READY"
	case LifecycleCleanupEnabled:
		return "CLEANUP_ENABLED"
	case LifecycleDegraded:
		return "DEGRADED"
	default:
		return "UNKNOWN"
	}
}

var ErrRecoveryFailed = errors.New("ringbuffer recovery failed: state degraded")

// LifecycleManager controls startup recovery before enabling ringbuffer segment cleanup.
type LifecycleManager struct {
	mu           sync.Mutex
	state        LifecycleState
	store        *ReservationStore
	index        *SegmentIndex
	cleanupReady chan struct{}
}

// NewLifecycleManager creates a new instance managing ringbuffer startup recovery.
func NewLifecycleManager(store *ReservationStore, index *SegmentIndex) *LifecycleManager {
	return &LifecycleManager{
		state:        LifecycleStarting,
		store:        store,
		index:        index,
		cleanupReady: make(chan struct{}),
	}
}

func (lm *LifecycleManager) State() LifecycleState {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.state
}

// RunRecovery executes the fail-safe startup recovery sequence.
func (lm *LifecycleManager) RunRecovery() error {
	lm.mu.Lock()
	lm.state = LifecycleRecovering
	lm.mu.Unlock()

	lm.store.mu.Lock()
	defer lm.store.mu.Unlock()

	if lm.store.storagePath != "" {
		data, err := os.ReadFile(lm.store.storagePath)
		if err != nil {
			if !os.IsNotExist(err) {
				lm.mu.Lock()
				lm.state = LifecycleDegraded
				lm.mu.Unlock()
				return fmt.Errorf("failed to read reservations state file: %w", err)
			}
		} else {
			var loaded map[string]*Reservation
			if err := json.Unmarshal(data, &loaded); err != nil {
				lm.mu.Lock()
				lm.state = LifecycleDegraded
				lm.mu.Unlock()
				return fmt.Errorf("failed to unmarshal corrupted reservations JSON: %w", err)
			}

			now := time.Now()
			for id, res := range loaded {
				if now.After(res.ExpiresAt) {
					continue // Skip expired reservations
				}
				// Verify segments exist and lock them
				var validSegs []SegmentID
				for _, segID := range res.SegmentIDs {
					if seg, ok := lm.index.GetByID(segID); ok {
						if _, err := os.Stat(seg.Path); err == nil {
							if seg.ReservationIDs == nil {
								seg.ReservationIDs = make(map[string]struct{})
							}
							seg.ReservationIDs[id] = struct{}{}
							seg.State = SegmentReserved
							validSegs = append(validSegs, segID)
						} else {
							seg.State = SegmentMissing
							res.Status = CompletenessGapped
						}
					}
				}
				res.SegmentIDs = validSegs
				lm.store.reservations[id] = res
			}
		}
	}

	lm.mu.Lock()
	lm.state = LifecycleReady
	lm.state = LifecycleCleanupEnabled
	close(lm.cleanupReady)
	lm.mu.Unlock()

	return nil
}

// WaitCleanupEnabled blocks until the recovery lifecycle enters CLEANUP_ENABLED.
func (lm *LifecycleManager) WaitCleanupEnabled() {
	<-lm.cleanupReady
}
