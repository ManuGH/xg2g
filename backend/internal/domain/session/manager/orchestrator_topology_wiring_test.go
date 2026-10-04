// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/receiverusage"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_TopologyService_StreamLifecycleAndMultiplexReuse(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	// 1 Demodulator, 1 Physical Input (Single Cable Sat)
	topo := receivertopology.ReceiverTopology{
		Model:      "Vu+ Uno 4K SE",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}

	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	registry := receivertopology.NewTransponderRegistry()
	// The ARD Digital 1 carrier as the receiver itself records it
	// (/etc/enigma2/lamedb: 00c00000:03fb:0001 -> 11493750 kHz horizontal, DVB-S2).
	registry.RegisterTransponder(0x03FB, 0x0001, 0x00C00000, receivertopology.TransponderKey{
		DeliverySystem:  receivertopology.DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493750000,
		Polarization:    receivertopology.PolarizationHorizontal,
		StreamID:        -1,
	})
	topoSvc.SetResolver(registry)

	evaluator := receiverusage.NewEvaluatorWithTopology(topoSvc)
	pipe := NewThreadSafeFakeMediaPipeline()

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       24 * time.Hour,
		HeartbeatEvery: 0,
		Owner:          "test-worker-topo",
		TunerSlots:     []int{0}, // Single flat slot
		ReceiverID:     "rec-1",
		Pipeline:       pipe,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode:                        receiverusage.ReceiverUsageModeEnforce,
			MaxLiveSessions:             4,
			MaxRestrictedAccessSessions: 4,
		},
		UsageEvaluator:  evaluator,
		TopologyService: topoSvc,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	// Two services on the EXACT SAME physical transport stream multiplex (ONID: 1, TSID: 0x3fb, Namespace: 0xc00000)
	dasErsteHD := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	arteHD := "1:0:19:283E:3FB:1:C00000:0:0:0:"

	// --- 1. Start Session 1 (Das Erste HD) ---
	evt1 := model.StartSessionEvent{
		SessionID:  "sess-live-1",
		ServiceRef: dasErsteHD,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-live-1",
		ServiceRef: dasErsteHD,
		State:      model.SessionNew,
	}))

	sessionCtx1 := &sessionContext{
		SessionID:  "sess-live-1",
		Mode:       model.ModeLive,
		ServiceRef: dasErsteHD,
	}

	leases1, err := orch.acquireLeases(ctx, sessionCtx1, evt1, "worker-1", zerolog.Nop())
	require.NoError(t, err, "Session 1 should acquire demodulator successfully")
	assert.Equal(t, 0, leases1.Slot)

	// Verify runtime allocation in TopologyService
	runtimeSnapshot := topoSvc.CloneRuntime()
	assert.Len(t, runtimeSnapshot.ActiveMultiplexes, 1)
	for _, alloc := range runtimeSnapshot.ActiveMultiplexes {
		assert.Contains(t, alloc.SessionIDs, "sess-live-1")
	}

	// --- 2. Start Session 2 (Arte HD - Same Transponder, Reuses Demodulator) ---
	evt2 := model.StartSessionEvent{
		SessionID:  "sess-live-2",
		ServiceRef: arteHD,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-live-2",
		ServiceRef: arteHD,
		State:      model.SessionNew,
	}))

	sessionCtx2 := &sessionContext{
		SessionID:  "sess-live-2",
		Mode:       model.ModeLive,
		ServiceRef: arteHD,
	}

	leases2, err := orch.acquireLeases(ctx, sessionCtx2, evt2, "worker-2", zerolog.Nop())
	require.NoError(t, err, "Session 2 should reuse the active demodulator via Multiplex-Reuse")
	assert.Equal(t, -1, leases2.Slot, "Multiplex-reuse session does not consume a physical tuner slot")

	// Verify both sessions share the same multiplex allocation in TopologyService
	runtimeSnapshot2 := topoSvc.CloneRuntime()
	assert.Len(t, runtimeSnapshot2.ActiveMultiplexes, 1)
	for _, alloc := range runtimeSnapshot2.ActiveMultiplexes {
		assert.Contains(t, alloc.SessionIDs, "sess-live-1")
		assert.Contains(t, alloc.SessionIDs, "sess-live-2")
	}

	// --- 3. Release Session 1 ---
	leases1.ReleaseTuner()

	runtimeSnapshot3 := topoSvc.CloneRuntime()
	assert.Len(t, runtimeSnapshot3.ActiveMultiplexes, 1)
	for _, alloc := range runtimeSnapshot3.ActiveMultiplexes {
		assert.NotContains(t, alloc.SessionIDs, "sess-live-1")
		assert.Contains(t, alloc.SessionIDs, "sess-live-2")
	}

	// --- 4. Release Session 2 ---
	leases2.ReleaseTuner()

	runtimeSnapshot4 := topoSvc.CloneRuntime()
	assert.Empty(t, runtimeSnapshot4.ActiveMultiplexes, "All demodulators and multiplexes must be completely freed")
}

func TestOrchestrator_TopologyService_HeartbeatLoss_EnforceMode(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	topo := receivertopology.ReceiverTopology{
		Model:      "Vu+ Uno 4K SE",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}

	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	registry := receivertopology.NewTransponderRegistry()
	// The ARD Digital 1 carrier as the receiver itself records it
	// (/etc/enigma2/lamedb: 00c00000:03fb:0001 -> 11493750 kHz horizontal, DVB-S2).
	registry.RegisterTransponder(0x03FB, 0x0001, 0x00C00000, receivertopology.TransponderKey{
		DeliverySystem:  receivertopology.DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493750000,
		Polarization:    receivertopology.PolarizationHorizontal,
		StreamID:        -1,
	})
	topoSvc.SetResolver(registry)

	evaluator := receiverusage.NewEvaluatorWithTopology(topoSvc)
	pipe := NewThreadSafeFakeMediaPipeline()

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       50 * time.Millisecond,
		HeartbeatEvery: 20 * time.Millisecond,
		Owner:          "test-worker-heartbeat",
		TunerSlots:     []int{0},
		ReceiverID:     "rec-hb",
		Pipeline:       pipe,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode:                        receiverusage.ReceiverUsageModeEnforce,
			MaxLiveSessions:             4,
			MaxRestrictedAccessSessions: 4,
		},
		UsageEvaluator:  evaluator,
		TopologyService: topoSvc,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
		startSem: make(chan struct{}, 10),
		stopSem:  make(chan struct{}, 10),
		active:   make(map[string]context.CancelFunc),
	}

	dasErsteHD := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	evt := model.StartSessionEvent{
		SessionID:  "sess-hb-loss-enforce",
		ServiceRef: dasErsteHD,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-hb-loss-enforce",
		ServiceRef: dasErsteHD,
		State:      model.SessionNew,
	}))

	sessionCtx := &sessionContext{
		SessionID:  "sess-hb-loss-enforce",
		Mode:       model.ModeLive,
		ServiceRef: dasErsteHD,
	}

	leases, err := orch.acquireLeases(ctx, sessionCtx, evt, "worker-hb", zerolog.Nop())
	require.NoError(t, err)
	defer leases.ReleaseTuner()

	// Deliberately trigger TTL sweep in TopologyService simulating lease expiration
	swept := topoSvc.SweepExpiredLeases(time.Now().UTC().Add(1 * time.Hour))
	require.Contains(t, swept, "sess-hb-loss-enforce", "SweepExpiredLeases must sweep the expired lease")

	// Wait for the heartbeat worker to tick and detect the lost lease in ENFORCE mode
	select {
	case <-leases.HBCtx.Done():
	case <-time.NewTimer(1 * time.Second).C:
		t.Fatalf("timeout waiting for heartbeat worker to cancel context on lost lease in ENFORCE mode")
	}

	sess, err := st.GetSession(ctx, "sess-hb-loss-enforce")
	require.NoError(t, err)
	assert.True(t, sess.State.IsTerminal(), "Session must be terminalized in ENFORCE mode when topology stream lease is lost")
	assert.Equal(t, model.RLeaseExpired, sess.Reason)

	// Perform deferred resource cleanup (as executed by orchestrator handleStart unwind)
	leases.ReleaseTuner()
	leases.ReleaseDedup()

	// 1. Verify Topology runtime allocations are completely empty
	assert.Empty(t, topoSvc.CloneRuntime().ActiveMultiplexes, "All topology runtime multiplexes must be fully cleared")

	// 2. Verify a subsequent session can cleanly re-acquire the freed tuner slot and demodulator without conflicts
	evtNext := model.StartSessionEvent{
		SessionID:  "sess-hb-reacquire",
		ServiceRef: dasErsteHD,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-hb-reacquire",
		ServiceRef: dasErsteHD,
		State:      model.SessionNew,
	}))
	sessionCtxNext := &sessionContext{
		SessionID:  "sess-hb-reacquire",
		Mode:       model.ModeLive,
		ServiceRef: dasErsteHD,
	}
	leasesNext, err := orch.acquireLeases(ctx, sessionCtxNext, evtNext, "worker-next", zerolog.Nop())
	require.NoError(t, err, "Subsequent session must acquire freed tuner and demodulator without resource contention")
	defer leasesNext.ReleaseTuner()
	assert.Equal(t, 0, leasesNext.Slot, "Must acquire slot 0 cleanly")
}

func TestOrchestrator_TopologyService_HeartbeatLoss_AuditOnly_FailOpen(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	// Observed topology clamped unconditionally to AUDIT_ONLY
	topo := receivertopology.ReceiverTopology{
		Model:      "Vu+ Uno 4K SE",
		Confidence: receivertopology.ConfidenceObserved,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "input_a", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}

	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeAuditOnly)
	require.NoError(t, err)

	evaluator := receiverusage.NewEvaluatorWithTopology(topoSvc)
	pipe := NewThreadSafeFakeMediaPipeline()

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       50 * time.Millisecond,
		HeartbeatEvery: 20 * time.Millisecond,
		Owner:          "test-worker-heartbeat-audit",
		TunerSlots:     []int{0},
		ReceiverID:     "rec-hb-audit",
		Pipeline:       pipe,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode:                        receiverusage.ReceiverUsageModeEnforce,
			MaxLiveSessions:             4,
			MaxRestrictedAccessSessions: 4,
		},
		UsageEvaluator:  evaluator,
		TopologyService: topoSvc,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
		startSem: make(chan struct{}, 10),
		stopSem:  make(chan struct{}, 10),
		active:   make(map[string]context.CancelFunc),
	}

	dasErsteHD := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	evt := model.StartSessionEvent{
		SessionID:  "sess-hb-loss-audit",
		ServiceRef: dasErsteHD,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-hb-loss-audit",
		ServiceRef: dasErsteHD,
		State:      model.SessionNew,
	}))

	sessionCtx := &sessionContext{
		SessionID:  "sess-hb-loss-audit",
		Mode:       model.ModeLive,
		ServiceRef: dasErsteHD,
	}

	leases, err := orch.acquireLeases(ctx, sessionCtx, evt, "worker-hb-audit", zerolog.Nop())
	require.NoError(t, err)
	defer leases.ReleaseTuner()

	// Deliberately trigger TTL sweep in TopologyService simulating lease expiration
	swept := topoSvc.SweepExpiredLeases(time.Now().UTC().Add(1 * time.Hour))
	require.Contains(t, swept, "sess-hb-loss-audit")

	// Verify heartbeat ticker runs without canceling context (fail-open)
	select {
	case <-leases.HBCtx.Done():
		t.Fatalf("Heartbeat context must not be canceled in AUDIT_ONLY mode (fail-open)")
	case <-time.NewTimer(60 * time.Millisecond).C:
		// Ticked through multiple heartbeat intervals successfully
	}

	assert.Nil(t, leases.HBCtx.Err(), "Heartbeat context must remain active in AUDIT_ONLY mode (fail-open)")

	sess, err := st.GetSession(ctx, "sess-hb-loss-audit")
	require.NoError(t, err)
	assert.False(t, sess.State.IsTerminal(), "Session must NOT be terminalized in AUDIT_ONLY mode on missing lease")
}

func TestOrchestrator_IPTV_ReceiverUsageEnforcement_BypassesTopology(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemoryStore()
	memBus := NewStubBus()

	// Setup single-tuner physical topology in strict ENFORCE mode
	topo := receivertopology.ReceiverTopology{
		Model:      "Single Tuner Test",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{ID: "in_a", DeliveryType: receivertopology.DeliveryLegacyUniversal},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "in_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}

	topoSvc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	registry := receivertopology.NewTransponderRegistry()
	registry.RegisterTransponder(0x03FB, 0x0001, 0x00C00000, receivertopology.TransponderKey{
		DeliverySystem:  receivertopology.DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11493750000,
		Polarization:    receivertopology.PolarizationHorizontal,
		StreamID:        -1,
	})
	registry.RegisterTransponder(0x0437, 0x0001, 0x00C00000, receivertopology.TransponderKey{
		DeliverySystem:  receivertopology.DeliverySystemDVBS2,
		OrbitalPosition: 192,
		FrequencyHz:     11361750000,
		Polarization:    receivertopology.PolarizationHorizontal,
		StreamID:        -1,
	})
	topoSvc.SetResolver(registry)

	evaluator := receiverusage.NewEvaluatorWithTopology(topoSvc)
	pipe := NewThreadSafeFakeMediaPipeline()

	orch := &Orchestrator{
		Bus:            memBus,
		Store:          st,
		LeaseTTL:       24 * time.Hour,
		HeartbeatEvery: 0,
		Owner:          "test-worker-usage",
		TunerSlots:     []int{0}, // Exactly 1 tuner slot
		ReceiverID:     "rec-1",
		Pipeline:       pipe,
		UsagePolicy: receiverusage.ReceiverUsagePolicy{
			Mode:                        receiverusage.ReceiverUsageModeEnforce,
			MaxLiveSessions:             4,
			MaxRestrictedAccessSessions: 4,
		},
		UsageEvaluator:  evaluator,
		TopologyService: topoSvc,
		LeaseKeyFunc: func(e model.StartSessionEvent) string {
			return model.LeaseKeyService(e.ServiceRef)
		},
	}

	// 1. DVB Session 1 tunes Transponder 0x3FB and consumes the only physical tuner slot (Slot 0)
	dvbRef1 := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	evt1 := model.StartSessionEvent{
		SessionID:  "sess-dvb-1",
		ServiceRef: dvbRef1,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-dvb-1",
		ServiceRef: dvbRef1,
		State:      model.SessionNew,
	}))
	sCtx1 := &sessionContext{SessionID: "sess-dvb-1", Mode: model.ModeLive, ServiceRef: dvbRef1}
	leases1, err := orch.acquireLeases(ctx, sCtx1, evt1, "worker-1", zerolog.Nop())
	require.NoError(t, err)
	defer leases1.ReleaseTuner()
	assert.Equal(t, 0, leases1.Slot)

	// 2. DVB Session 2 on a DIFFERENT transponder (0x437) -> MUST FAIL because tuner is busy
	dvbRef2 := "1:0:19:2B66:437:1:C00000:0:0:0:"
	evt2 := model.StartSessionEvent{
		SessionID:  "sess-dvb-2",
		ServiceRef: dvbRef2,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-dvb-2",
		ServiceRef: dvbRef2,
		State:      model.SessionNew,
	}))
	sCtx2 := &sessionContext{SessionID: "sess-dvb-2", Mode: model.ModeLive, ServiceRef: dvbRef2}
	_, err2 := orch.acquireLeases(ctx, sCtx2, evt2, "worker-2", zerolog.Nop())
	require.Error(t, err2, "DVB on different transponder must be rejected when tuner is busy")
	reason, _, _, ok := lifecycle.ReasonFromError(err2)
	require.True(t, ok)
	assert.Equal(t, model.RLeaseBusy, reason)

	// 3. IPTV Session with receiver-usage policy ENFORCED and DVB topology 100% full:
	// Invariant: IPTV must be a first-class source that bypasses topology evaluation,
	// acquires 0 tuner slots (leases.Slot == -1), and is NOT rejected by full DVB topology!
	iptvRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//provider.invalid/live/stream.m3u8:FirstClassIPTV"
	evtIPTV := model.StartSessionEvent{
		SessionID:  "sess-iptv-1",
		ServiceRef: iptvRef,
		ProfileID:  "hd",
	}
	require.NoError(t, st.PutSession(ctx, &model.SessionRecord{
		SessionID:  "sess-iptv-1",
		ServiceRef: iptvRef,
		State:      model.SessionNew,
	}))
	sCtxIPTV := &sessionContext{SessionID: "sess-iptv-1", Mode: model.ModeLive, ServiceRef: iptvRef}
	leasesIPTV, errIPTV := orch.acquireLeases(ctx, sCtxIPTV, evtIPTV, "worker-iptv", zerolog.Nop())
	require.NoError(t, errIPTV, "IPTV must succeed even when DVB tuner topology is 100% full")
	defer leasesIPTV.ReleaseTuner()

	assert.Equal(t, -1, leasesIPTV.Slot, "IPTV must consume -1 (zero) physical tuner slots")
}

