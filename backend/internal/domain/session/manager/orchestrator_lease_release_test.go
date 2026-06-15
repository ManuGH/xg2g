// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/manager/testkit"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleStart_AbortAfterLeaseAcquire_ReleasesTunerLease is M3's RED control. It forces a
// start failure in the exact [tuner lease acquired … transitionStarting commits B) window:
// a session already in STOPPING makes transitionStarting abort (orchestrator_transitions.go)
// AFTER acquireLeases grabbed tuner slot 0 but BEFORE the tuner_slot is written into
// ContextData (B). Before M3, the only hot-path tuner release read B (empty here) so the slot
// leaked until LeaseTTL. With M3, finalizeDeferred releases the slot via the in-memory handle.
// This drives the real "stop arrived before start" path, not an injected fault.
func TestHandleStart_AbortAfterLeaseAcquire_ReleasesTunerLease(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	pipe := testkit.NewStepperPipeline()

	orch := &Orchestrator{
		Store:               st,
		LeaseTTL:            24 * time.Hour,
		HeartbeatEvery:      0,
		Owner:               "worker-m3-leak",
		TunerSlots:          []int{0},
		Pipeline:            pipe,
		Platform:            NewStubPlatform(),
		PipelineStopTimeout: 10 * time.Second,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	sessID := "m3-leak"
	serviceRef := "ref:m3-leak"
	// Already STOPPING: transitionStarting aborts after the tuner lease is held but before B
	// is persisted — the leak window.
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  sessID,
		ServiceRef: serviceRef,
		State:      model.SessionStopping,
	}))

	err := orch.handleStart(ctx, model.StartSessionEvent{SessionID: sessID, ServiceRef: serviceRef, ProfileID: "p1"})
	require.Error(t, err, "start must abort because the session is STOPPING")
	require.ErrorIs(t, err, ErrSessionCanceled)

	// Sanity: B was never written, so a B-based release cannot find the slot.
	rec, gerr := st.GetSession(ctx, sessID)
	require.NoError(t, gerr)
	require.Empty(t, rec.ContextData[model.CtxKeyTunerSlot],
		"precondition: ContextData tuner_slot (B) must be unset in this window")

	// The tuner lease must be released via the in-memory handle despite B being empty.
	_, ok, gerr := st.GetLease(ctx, model.LeaseKeyTunerSlot(0))
	require.NoError(t, gerr)
	assert.False(t, ok,
		"tuner slot 0 lease leaked: held after start aborted in the [acquired … B committed) window")

	// The dedup lease has always had an in-memory release (ReleaseDedup); assert it too as a
	// regression guard that the failure path frees everything.
	_, ok, gerr = st.GetLease(ctx, model.LeaseKeyService(serviceRef))
	require.NoError(t, gerr)
	assert.False(t, ok, "dedup lease leaked after start abort")
}
