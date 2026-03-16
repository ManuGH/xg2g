package v3

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type staticAdmissionSessionStore struct {
	sessions []*model.SessionRecord
}

func (s *staticAdmissionSessionStore) ListSessions(context.Context) ([]*model.SessionRecord, error) {
	return s.sessions, nil
}

func TestUpdateConfigRefreshesAdmissionTunerCount(t *testing.T) {
	store := &staticAdmissionSessionStore{}
	srv := &Server{
		admissionState: newStoreAdmissionState(store, 0),
	}

	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0, 1, 2, 3}

	srv.UpdateConfig(cfg, config.BuildSnapshot(cfg, config.DefaultEnv()))

	state := CollectRuntimeState(context.Background(), srv.admissionState)
	if state.TunerSlots != 4 {
		t.Fatalf("expected 4 tuner slots after config update, got %d", state.TunerSlots)
	}
}

func TestUpdateConfigRefreshesAdmissionTunerCountWithActiveSessions(t *testing.T) {
	store := &staticAdmissionSessionStore{
		sessions: []*model.SessionRecord{
			{State: model.SessionReady},
			{State: model.SessionStopped},
		},
	}
	srv := &Server{
		admissionState: newStoreAdmissionState(store, 1),
	}

	cfg := config.AppConfig{}
	cfg.Engine.TunerSlots = []int{0, 1, 2}

	srv.UpdateConfig(cfg, config.BuildSnapshot(cfg, config.DefaultEnv()))

	state := CollectRuntimeState(context.Background(), srv.admissionState)
	if state.TunerSlots != 2 {
		t.Fatalf("expected active sessions to reduce refreshed tuner slots to 2, got %d", state.TunerSlots)
	}
}
