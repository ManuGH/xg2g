package v3

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
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

type failingAdmissionSessionStore struct {
	fail     bool
	sessions []*model.SessionRecord
}

func (s *failingAdmissionSessionStore) ListSessions(context.Context) ([]*model.SessionRecord, error) {
	if s.fail {
		return nil, fmt.Errorf("simulated sqlite read error")
	}
	return s.sessions, nil
}

func TestAdmissionStateResilience_CachedFallbackAndBaseline(t *testing.T) {
	store := &failingAdmissionSessionStore{
		fail: false,
		sessions: []*model.SessionRecord{
			{State: model.SessionReady},
		},
	}
	adm := newStoreAdmissionState(store, 4)

	// First read succeeds and populates cache
	state1, err := adm.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first snapshot failed: %v", err)
	}
	if state1.TunerSlots != 3 || state1.SessionsActive != 1 {
		t.Fatalf("expected 3 available tuners and 1 active session, got tuners=%d active=%d", state1.TunerSlots, state1.SessionsActive)
	}

	// Now simulate DB failure -> should return cached state seamlessly
	store.fail = true
	state2, err := adm.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot during transient error failed: %v", err)
	}
	if state2.TunerSlots != 3 || state2.SessionsActive != 1 {
		t.Fatalf("expected cached state tuners=3 active=1 during DB error, got tuners=%d active=%d", state2.TunerSlots, state2.SessionsActive)
	}

	// Un-cached store failure -> should return baseline state (TunerSlots=4) instead of failing closed (-1)
	freshStore := &failingAdmissionSessionStore{fail: true}
	freshAdm := newStoreAdmissionState(freshStore, 4)
	state3, err := freshAdm.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot on un-cached failing store failed: %v", err)
	}
	if state3.TunerSlots != 4 || state3.SessionsActive != 0 {
		t.Fatalf("expected baseline tuners=4 active=0 on fresh failing store, got tuners=%d active=%d", state3.TunerSlots, state3.SessionsActive)
	}
}

func TestAdmissionState_SlotIdentityUnification(t *testing.T) {
	store := &staticAdmissionSessionStore{
		sessions: []*model.SessionRecord{
			{SessionID: "sess1", State: model.SessionReady, ContextData: map[string]string{"tuner_scope": "tuner:0"}},
			{SessionID: "sess2", State: model.SessionReady, ContextData: map[string]string{"tuner_scope": "tuner:0"}}, // Historical duplicate slot
		},
	}
	adm := newStoreAdmissionState(store, 2)
	state, err := adm.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	// 2 total tuners - 1 unique occupied slot (tuner:0) = 1 available tuner
	if state.TunerSlots != 1 {
		t.Fatalf("expected 1 available tuner due to slot unification, got %d", state.TunerSlots)
	}
	if state.SessionsActive != 2 {
		t.Fatalf("expected 2 active sessions counted, got %d", state.SessionsActive)
	}
}

func TestAdmissionState_OverbookingGuard_ActiveLeaseStoreLease(t *testing.T) {
	memStore := store.NewMemoryStore()
	ctx := context.Background()

	// Acquire authoritative lease for tuner:0 with 5 minute TTL
	_, ok, err := memStore.TryAcquireLease(ctx, "tuner:0", "worker-1", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("failed to acquire lease: %v, ok=%v", err, ok)
	}

	// Store has session record with expired lease
	now := time.Now().Unix()
	_ = memStore.PutSession(ctx, &model.SessionRecord{
		SessionID:          "sess-old",
		State:              model.SessionReady,
		ContextData:        map[string]string{"tuner_scope": "tuner:0"},
		LeaseExpiresAtUnix: now - 60, // Expired session
	})

	adm := newStoreAdmissionState(memStore, 1)
	state, err := adm.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	// Since authoritative lease tuner:0 is still active in LeaseStore, tuner MUST remain occupied!
	if state.TunerSlots != 0 {
		t.Fatalf("expected 0 available tuners (overbooking guard), got %d", state.TunerSlots)
	}

	// Release authoritative lease in LeaseStore
	_ = memStore.ReleaseLease(ctx, "tuner:0", "worker-1")

	stateAfterRelease, err := adm.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot after release failed: %v", err)
	}

	// Now that lease is released AND session is expired, tuner:0 is available!
	if stateAfterRelease.TunerSlots != 1 {
		t.Fatalf("expected 1 available tuner after lease release and expired session, got %d", stateAfterRelease.TunerSlots)
	}
}
