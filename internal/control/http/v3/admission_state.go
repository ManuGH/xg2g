package v3

import (
	"context"

	"fmt"

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

// storeAdmissionState implements AdmissionStateSource using the system state store.
type storeAdmissionState struct {
	store interface {
		ListSessions(ctx context.Context) ([]*model.SessionRecord, error)
	}
	tunerCount int
}

func (s *storeAdmissionState) Snapshot(ctx context.Context) (admission.RuntimeState, error) {
	if s.store == nil {
		return admission.RuntimeState{}, fmt.Errorf("admission state store is nil")
	}

	sessions, err := s.store.ListSessions(ctx)
	if err != nil {
		return admission.RuntimeState{}, fmt.Errorf("failed to list sessions: %w", err)
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

	availTuners := s.tunerCount - activeCount
	if availTuners < 0 {
		availTuners = 0
	}

	return admission.RuntimeState{
		TunerSlots:       availTuners,
		SessionsActive:   activeCount,
		TranscodesActive: transcodeCount,
	}, nil
}

// CollectRuntimeState is deprecated in favor of AdmissionState.Snapshot.
// Keeping it briefly for compatibility during transition if needed, but pointing to Snapshot.
func CollectRuntimeState(ctx context.Context, src AdmissionState) admission.RuntimeState {
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
