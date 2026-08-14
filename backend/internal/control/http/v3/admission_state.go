package v3

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
)

// AdmissionState abstracts the retrieval of runtime metrics.
type AdmissionState interface {
	// Snapshot returns the current runtime state for admission decisions.
	// If any metric cannot be retrieved or store is nil, it returns an error (fail-closed).
	Snapshot(ctx context.Context) (admission.RuntimeState, error)
}

type admissionSessionStore interface {
	ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
}

// storeAdmissionState implements AdmissionStateSource using the system state store.
type storeAdmissionState struct {
	store      admissionSessionStore
	tunerCount atomic.Int64
	mu         sync.RWMutex
	lastState  admission.RuntimeState
	lastUpdate time.Time
}

func newStoreAdmissionState(store admissionSessionStore, tunerCount int) *storeAdmissionState {
	state := &storeAdmissionState{store: store}
	state.SetTunerCount(tunerCount)
	return state
}

func (s *storeAdmissionState) SetTunerCount(tunerCount int) {
	if tunerCount < 0 {
		tunerCount = 0
	}
	s.tunerCount.Store(int64(tunerCount))
}

func (s *storeAdmissionState) Snapshot(ctx context.Context) (admission.RuntimeState, error) {
	tuners := int(s.tunerCount.Load())
	if tuners <= 0 {
		tuners = 1
	}

	if s.store == nil {
		return admission.RuntimeState{
			TunerSlots:       tuners,
			SessionsActive:   0,
			TranscodesActive: 0,
		}, nil
	}

	// Use an isolated 2-second timeout context so aggressive caller context cancellation doesn't abort DB queries
	queryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sessions, err := s.store.ListSessions(queryCtx)
	if err != nil {
		s.mu.RLock()
		cached := s.lastState
		cachedTime := s.lastUpdate
		s.mu.RUnlock()

		// Fallback to last known good snapshot if recent (< 15 seconds) to handle transient DB locks gracefully
		if !cachedTime.IsZero() && time.Since(cachedTime) < 15*time.Second {
			log.L().Warn().Err(err).Time("last_update", cachedTime).Msg("admission state DB read failed, falling back to cached snapshot")
			return cached, nil
		}

		log.L().Warn().Err(err).Msg("admission state DB read failed without fresh cache, using baseline admission state")
		return admission.RuntimeState{
			TunerSlots:       tuners,
			SessionsActive:   0,
			TranscodesActive: 0,
		}, nil
	}

	activeCount := 0
	transcodeCount := 0
	for _, sess := range sessions {
		if !model.IsResourceOccupying(sess.State) {
			continue
		}
		activeCount++
		if sess.Profile.TranscodeVideo {
			transcodeCount++
		}
	}

	availTuners := max(tuners-activeCount, 0)
	state := admission.RuntimeState{
		TunerSlots:       availTuners,
		SessionsActive:   activeCount,
		TranscodesActive: transcodeCount,
	}

	s.mu.Lock()
	s.lastState = state
	s.lastUpdate = time.Now()
	s.mu.Unlock()

	return state, nil
}

// CollectRuntimeState is deprecated in favor of AdmissionState.Snapshot.
func CollectRuntimeState(ctx context.Context, src AdmissionState) admission.RuntimeState {
	if src == nil {
		return admission.RuntimeState{
			TunerSlots:       1,
			SessionsActive:   0,
			TranscodesActive: 0,
		}
	}
	state, err := src.Snapshot(ctx)
	if err != nil {
		log.L().Error().Err(err).Msg("admission state snapshot failed, failing closed")
		return admission.RuntimeState{
			TunerSlots:       -1,
			SessionsActive:   -1,
			TranscodesActive: -1,
		}
	}
	return state
}
