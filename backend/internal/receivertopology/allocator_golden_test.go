package receivertopology_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create standard FBC Single-Cable Legacy Topology (1 Input, 8 Demods)
func newFBCSingleCableLegacyTopology() receivertopology.ReceiverTopology {
	return receivertopology.ReceiverTopology{
		Model:      "Vu+ Uno 4K SE (FBC Legacy)",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{
				ID:           "input_a",
				DeliveryType: receivertopology.DeliveryLegacyUniversal,
				Satellites:   []receivertopology.SatellitePosition{192}, // Astra 19.2E
			},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_1", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_2", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_3", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_4", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_5", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_6", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_7", InputID: "input_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
}

// Helper to create Unicable SCR Topology (1 Input, 4 SCR User Bands, 4 Demods)
func newUnicableSCRTopology() receivertopology.ReceiverTopology {
	return receivertopology.ReceiverTopology{
		Model:      "Vu+ Uno 4K SE (Unicable EN50494)",
		Confidence: receivertopology.ConfidenceVerified,
		Inputs: []receivertopology.PhysicalInput{
			{
				ID:           "input_scr_a",
				DeliveryType: receivertopology.DeliveryUnicable1,
				UserBands:    4, // 4 SCR channels
				Satellites:   []receivertopology.SatellitePosition{192},
			},
		},
		Demodulators: []receivertopology.Demodulator{
			{ID: "demod_0", InputID: "input_scr_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_1", InputID: "input_scr_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_2", InputID: "input_scr_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
			{ID: "demod_3", InputID: "input_scr_a", DVBTypes: []receivertopology.DVBType{receivertopology.DVBTypeSat}},
		},
	}
}

// Test Golden 1: Transponder Multiplex-Reuse (2 services on same physical multiplex)
func TestGolden_MultiplexReuse(t *testing.T) {
	topo := newFBCSingleCableLegacyTopology()
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	// ZDF HD: 1:0:19:2B90:3F3:1:C00000:0:0:0: (TSID: 0x3F3, ONID: 0x1, Namespace: 0xC00000 -> 192:HIGH:H)
	service1 := "1:0:19:2B90:3F3:1:C00000:0:0:0:"
	// ZDFinfo HD: on the SAME transponder (TSID: 0x3F3, ONID: 0x1, Namespace: 0xC00000)
	service2 := "1:0:19:2BA2:3F3:1:C00000:0:0:0:"

	// 1. First stream starts
	dec1, err := svc.RegisterStream(service1, "session-1")
	require.NoError(t, err)
	assert.True(t, dec1.Allowed)
	assert.False(t, dec1.ReusedDemod, "First stream tunes the multiplex")
	assert.Equal(t, receivertopology.DemodulatorID("demod_0"), dec1.DemodID)

	// 2. Second stream starts on same multiplex
	dec2, err := svc.RegisterStream(service2, "session-2")
	require.NoError(t, err)
	assert.True(t, dec2.Allowed)
	assert.True(t, dec2.ReusedDemod, "Second stream reuses active multiplex (0 extra RF tune)")
	assert.Equal(t, receivertopology.DemodulatorID("demod_0"), dec2.DemodID, "Must bind to the same demodulator")
}

// Test Golden 2: Legacy FBC Same-Plane Allocation (Different transponders, same polarization/band)
func TestGolden_LegacyFBC_SamePlaneAllocation(t *testing.T) {
	topo := newFBCSingleCableLegacyTopology()
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	// Channel 1: High-H (ZDF HD)
	service1 := "1:0:19:2B90:3F3:1:C00000:0:0:0:"
	// Channel 2: High-H (Das Erste HD, TSID: 0x3F2 -> same High-H plane)
	service2 := "1:0:19:283D:3F2:1:C00000:0:0:0:"

	dec1, err := svc.RegisterStream(service1, "session-1")
	require.NoError(t, err)
	assert.True(t, dec1.Allowed)
	assert.Equal(t, receivertopology.DemodulatorID("demod_0"), dec1.DemodID)

	dec2, err := svc.RegisterStream(service2, "session-2")
	require.NoError(t, err)
	assert.True(t, dec2.Allowed)
	assert.False(t, dec2.ReusedDemod)
	assert.Equal(t, receivertopology.DemodulatorID("demod_1"), dec2.DemodID, "FBC multi-demod shares active High-H plane")
}

// Test Golden 3: Legacy FBC Single-Cable Plane Conflict
func TestGolden_LegacyFBC_PlaneConflict(t *testing.T) {
	topo := newFBCSingleCableLegacyTopology()
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	// Channel 1: High-H (ZDF HD)
	service1 := "1:0:19:2B90:3F3:1:C00000:0:0:0:"
	dec1, err := svc.RegisterStream(service1, "session-1")
	require.NoError(t, err)
	require.True(t, dec1.Allowed)

	allocator := svc.Allocator()
	runtime := svc.Runtime()

	lowVPlane := receivertopology.RFPlane{SatPosition: 192, Band: receivertopology.BandLow, Polarization: receivertopology.PolarizationVertical}
	lowVMux := receivertopology.MultiplexID{
		DVBType:      receivertopology.DVBTypeSat,
		TSID:         999,
		ONID:         1,
		DVBNamespace: 0xC00000,
		RFPlane:      &lowVPlane,
	}

	decConflict := allocator.CanAllocate(runtime, lowVMux, "session-conflicting")
	assert.False(t, decConflict.Allowed, "Must reject conflicting RFPlane on single-cable legacy input")
	assert.Equal(t, receivertopology.ProblemCodePlaneConflict, decConflict.ProblemCode)
}

// Test Golden 4: Unicable SCR Channel Agility
func TestGolden_UnicableSCR_Agility(t *testing.T) {
	topo := newUnicableSCRTopology() // 4 SCR channels
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	allocator := svc.Allocator()
	runtime := svc.Runtime()

	// 4 streams on 4 DIFFERENT satellite planes (High-H, High-V, Low-H, Low-V)
	planes := []receivertopology.RFPlane{
		{SatPosition: 192, Band: receivertopology.BandHigh, Polarization: receivertopology.PolarizationHorizontal},
		{SatPosition: 192, Band: receivertopology.BandHigh, Polarization: receivertopology.PolarizationVertical},
		{SatPosition: 192, Band: receivertopology.BandLow, Polarization: receivertopology.PolarizationHorizontal},
		{SatPosition: 192, Band: receivertopology.BandLow, Polarization: receivertopology.PolarizationVertical},
	}

	for i, pl := range planes {
		mux := receivertopology.MultiplexID{
			DVBType:      receivertopology.DVBTypeSat,
			TSID:         uint16(1000 + i),
			ONID:         1,
			DVBNamespace: 0xC00000,
			RFPlane:      &pl,
		}
		dec := allocator.CanAllocate(runtime, mux, "unicable-sess")
		assert.True(t, dec.Allowed, "Unicable must allow independent planes up to SCR user band limit")
		_, err := allocator.Allocate(runtime, mux, "unicable-sess", receivertopology.AllocationOwnerXG2G)
		require.NoError(t, err)
	}

	// 5th stream exceeds SCR user bands (4)
	fifthPlane := receivertopology.RFPlane{SatPosition: 192, Band: receivertopology.BandHigh, Polarization: receivertopology.PolarizationHorizontal}
	fifthMux := receivertopology.MultiplexID{
		DVBType:      receivertopology.DVBTypeSat,
		TSID:         1005,
		ONID:         1,
		DVBNamespace: 0xC00000,
		RFPlane:      &fifthPlane,
	}
	dec5 := allocator.CanAllocate(runtime, fifthMux, "unicable-overflow")
	assert.False(t, dec5.Allowed, "Must reject when all SCR user bands are exhausted")
	assert.Equal(t, receivertopology.ProblemCodeNoTuners, dec5.ProblemCode)
}

// Test Golden 5: Policy-Driven DVR Lookahead
func TestGolden_DVRLookaheadPolicy(t *testing.T) {
	topo := newFBCSingleCableLegacyTopology()
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	now := time.Now().UTC()

	// 1. Timer recording scheduled in 2 MINUTES (within default 5min lookahead window)
	lowVPlane := receivertopology.RFPlane{SatPosition: 192, Band: receivertopology.BandLow, Polarization: receivertopology.PolarizationVertical}
	svc.SyncTimers([]receivertopology.RecordingReservation{
		{
			ID: "timer-tatort",
			MultiplexID: receivertopology.MultiplexID{
				DVBType:      receivertopology.DVBTypeSat,
				TSID:         888,
				ONID:         1,
				DVBNamespace: 0xC00000,
				RFPlane:      &lowVPlane,
			},
			StartTime: now.Add(2 * time.Minute),
			EndTime:   now.Add(92 * time.Minute),
			Title:     "Tatort",
			Priority:  receivertopology.PriorityUpcomingRecording,
		},
	})

	// Live stream requests High-H plane right now -> would monopolize input_a and block the upcoming Low-V recording
	highHMux := receivertopology.MultiplexID{
		DVBType:      receivertopology.DVBTypeSat,
		TSID:         777,
		ONID:         1,
		DVBNamespace: 0xC00000,
		RFPlane:      &receivertopology.RFPlane{SatPosition: 192, Band: receivertopology.BandHigh, Polarization: receivertopology.PolarizationHorizontal},
	}

	dec := svc.Allocator().EvaluateWithUpcomingReservations(svc.Runtime(), svc.Planner(), highHMux, "live-sess", receivertopology.PriorityLive, now)
	assert.False(t, dec.Allowed, "Must block live stream when upcoming DVR recording in 2 minutes would be starved")
	assert.Equal(t, receivertopology.ProblemCodeRecordingReservationConflict, dec.ProblemCode)

	// 2. If the timer recording is scheduled in 30 MINUTES (> 5min lookahead window)
	svc.SyncTimers([]receivertopology.RecordingReservation{
		{
			ID: "timer-future",
			MultiplexID: receivertopology.MultiplexID{
				DVBType:      receivertopology.DVBTypeSat,
				TSID:         888,
				ONID:         1,
				DVBNamespace: 0xC00000,
				RFPlane:      &lowVPlane,
			},
			StartTime: now.Add(30 * time.Minute),
			EndTime:   now.Add(120 * time.Minute),
			Title:     "Late Night Show",
			Priority:  receivertopology.PriorityUpcomingRecording,
		},
	})

	decFuture := svc.Allocator().EvaluateWithUpcomingReservations(svc.Runtime(), svc.Planner(), highHMux, "live-sess", receivertopology.PriorityLive, now)
	assert.True(t, decFuture.Allowed, "Must permit live stream immediately when recording is outside the lookahead horizon")
}

// Test Golden 6: Store-Level Atomic ClaimSet Integration (SQLite & MemoryStore)
func TestGolden_StoreLevelAtomicClaimSet(t *testing.T) {
	topo := newFBCSingleCableLegacyTopology()
	svc, err := receivertopology.NewService(topo, receivertopology.EvaluationModeEnforce)
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "golden_claim.db")
	sqlStore, err := store.NewSqliteStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlStore.Close() })

	ctx := context.Background()
	serviceRef := "1:0:19:2B90:3F3:1:C00000:0:0:0:" // ZDF HD

	// 1. Acquire claim set atomically
	res, dec, err := svc.AcquireClaimSetAtomic(ctx, sqlStore, serviceRef, "sess-atom-1", receivertopology.PriorityLive, 10*time.Second)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.True(t, dec.Allowed)
	assert.False(t, res.ReusedMux)
	assert.Equal(t, "demod_0", res.DemodID)

	// 2. Second session on same multiplex
	res2, dec2, err := svc.AcquireClaimSetAtomic(ctx, sqlStore, serviceRef, "sess-atom-2", receivertopology.PriorityLive, 10*time.Second)
	require.NoError(t, err)
	assert.True(t, res2.Success)
	assert.True(t, dec2.Allowed)
	assert.True(t, res2.ReusedMux, "Must reuse active multiplex in database transaction")
	assert.Equal(t, "demod_0", res2.DemodID)

	// 3. Release first session -> hardware must remain active for second session
	err = sqlStore.ReleaseClaimSet(ctx, "sess-atom-1")
	require.NoError(t, err)

	demodLease, hasLease, err := sqlStore.GetLease(ctx, model.LeaseKeyDemod("demod_0"))
	require.NoError(t, err)
	assert.True(t, hasLease, "Demod lease must remain active in database while second session is connected")
	assert.NotNil(t, demodLease)

	// 4. Release second session -> hardware is cleanly freed
	err = sqlStore.ReleaseClaimSet(ctx, "sess-atom-2")
	require.NoError(t, err)

	_, hasLeaseAfter, _ := sqlStore.GetLease(ctx, model.LeaseKeyDemod("demod_0"))
	assert.False(t, hasLeaseAfter, "Demod lease must be deleted after all sessions leave")
}
