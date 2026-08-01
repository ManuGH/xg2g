package ringbuffer

import (
	"encoding/json"
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
	default:
		return "UNKNOWN"
	}
}

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

// RunRecovery executes the startup recovery sequence.
func (lm *LifecycleManager) RunRecovery() error {
	lm.mu.Lock()
	lm.state = LifecycleRecovering
	lm.mu.Unlock()

	// Load persistent reservations
	lm.store.mu.Lock()
	defer lm.store.mu.Unlock()

	if lm.store.storagePath != "" {
		if data, err := os.ReadFile(lm.store.storagePath); err == nil {
			var loaded map[string]*Reservation
			if err := json.Unmarshal(data, &loaded); err == nil {
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
