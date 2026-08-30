// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockPollerClient struct {
	aboutFunc      func(ctx context.Context) (*openwebif.AboutInfo, error)
	timersFunc     func(ctx context.Context) ([]openwebif.Timer, error)
	statusInfoFunc func(ctx context.Context) (*openwebif.StatusInfo, error)
	currentFunc    func(ctx context.Context) (*openwebif.CurrentInfo, error)
}

func (m *mockPollerClient) About(ctx context.Context) (*openwebif.AboutInfo, error) {
	if m.aboutFunc != nil {
		return m.aboutFunc(ctx)
	}
	return nil, nil
}

func (m *mockPollerClient) GetTimers(ctx context.Context) ([]openwebif.Timer, error) {
	if m.timersFunc != nil {
		return m.timersFunc(ctx)
	}
	return nil, nil
}

func (m *mockPollerClient) GetStatusInfo(ctx context.Context) (*openwebif.StatusInfo, error) {
	if m.statusInfoFunc != nil {
		return m.statusInfoFunc(ctx)
	}
	return nil, nil
}

func (m *mockPollerClient) GetCurrent(ctx context.Context) (*openwebif.CurrentInfo, error) {
	if m.currentFunc != nil {
		return m.currentFunc(ctx)
	}
	return nil, nil
}

func TestCollectRuntimeSnapshot_MultiEndpointAggregation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 16, 18, 0, 0, 0, time.UTC)

	client := &mockPollerClient{
		statusInfoFunc: func(ctx context.Context) (*openwebif.StatusInfo, error) {
			return &openwebif.StatusInfo{
				InStandby: "false",
			}, nil
		},
		currentFunc: func(ctx context.Context) (*openwebif.CurrentInfo, error) {
			curr := &openwebif.CurrentInfo{
				Result: true,
			}
			curr.Info.ServiceRef = "1:0:19:283D:3FB:1:C00000:0:0:0:"
			curr.Info.ServiceName = "Das Erste HD"
			curr.Info.VideoPID = 5101
			curr.Info.AudioPID = 5102
			return curr, nil
		},
		timersFunc: func(ctx context.Context) ([]openwebif.Timer, error) {
			return []openwebif.Timer{
				{
					Name:       "Active Movie",
					ServiceRef: "1:0:19:283E:3FB:1:C00000:0:0:0:",
					Begin:      now.Add(-30 * time.Minute).Unix(),
					End:        now.Add(30 * time.Minute).Unix(),
					Disabled:   0,
				},
				{
					Name:       "Future Show",
					ServiceRef: "1:0:19:283F:3FB:1:C00000:0:0:0:",
					Begin:      now.Add(2 * time.Hour).Unix(),
					End:        now.Add(3 * time.Hour).Unix(),
					Disabled:   0,
				},
			}, nil
		},
		aboutFunc: func(ctx context.Context) (*openwebif.AboutInfo, error) {
			var about openwebif.AboutInfo
			about.Info.Tuners = []openwebif.AboutTuner{
				{Name: "Tuner A", Type: "DVB-S2", Live: "1:0:19:283D:3FB:1:C00000:0:0:0:"},
				{Name: "Tuner B", Type: "DVB-S2", Rec: "1:0:19:283E:3FB:1:C00000:0:0:0:"},
			}
			about.Info.Streams = []any{} // Explicitly empty
			return &about, nil
		},
	}

	snap := CollectRuntimeSnapshot(ctx, client, now)

	assert.False(t, snap.InStandby)
	assert.Equal(t, EvidenceObserved, snap.StandbyEvidence)

	require.NotNil(t, snap.HDMIPlayback)
	assert.Equal(t, "1:0:19:283D:3FB:1:C00000:0:0:0:", snap.HDMIPlayback.ServiceRef)
	assert.Equal(t, "Das Erste HD", snap.HDMIPlayback.ServiceName)
	assert.Equal(t, EvidenceObserved, snap.HDMIPlayback.Evidence)
	assert.Equal(t, 5101, snap.HDMIPlayback.PIDs["vpid"])

	require.Len(t, snap.ActiveRecordings, 1)
	assert.Equal(t, "Active Movie", snap.ActiveRecordings[0].Title)
	assert.True(t, snap.ActiveRecordings[0].IsActiveNow)

	require.Len(t, snap.UpcomingRecordings, 1)
	assert.Equal(t, "Future Show", snap.UpcomingRecordings[0].Title)
	assert.False(t, snap.UpcomingRecordings[0].IsActiveNow)

	assert.Equal(t, StreamPresenceEmpty, snap.StreamPresence)
}

func TestExtractExternalAllocations_StandbyAwarenessPreservesObservation(t *testing.T) {
	muxLive, _ := ParseServiceRef("1:0:19:283D:3FB:1:C00000:0:0:0:")
	muxRec, _ := ParseServiceRef("1:0:19:283E:3FB:1:C00000:0:0:0:")

	topo := ReceiverTopology{
		Inputs: []PhysicalInput{
			{ID: "in-0", Label: "Input 1", DeliveryType: DeliveryLegacyUniversal},
		},
		Demodulators: []Demodulator{
			{ID: "tuner-0", InputID: "in-0"},
			{ID: "tuner-1", InputID: "in-0"},
		},
	}

	// Case 1: Active TV (Not in Standby) -> HDMI Live TV is allocated
	activeSnap := ReceiverRuntimeSnapshot{
		InStandby:       false,
		StandbyEvidence: EvidenceObserved,
		HDMIPlayback: &ObservedService{
			ServiceRef:  "1:0:19:283D:3FB:1:C00000:0:0:0:",
			ServiceName: "Das Erste HD",
			MultiplexID: &muxLive,
			RFPlane:     muxLive.RFPlane,
			Evidence:    EvidenceObserved,
		},
		ActiveRecordings: []ObservedRecording{
			{
				Title:       "Recording 1",
				ServiceRef:  "1:0:19:283E:3FB:1:C00000:0:0:0:",
				MultiplexID: &muxRec,
				IsActiveNow: true,
				Evidence:    EvidenceObserved,
			},
		},
		StreamPresence: StreamPresenceEmpty,
	}

	allocsActive := ExtractExternalAllocationsFromSnapshot(activeSnap, topo, nil)
	require.Len(t, allocsActive, 2)
	assert.Equal(t, "hdmi_live_tv", allocsActive[0].Source)
	assert.Equal(t, "local_timer_dvr", allocsActive[1].Source)

	// Case 2: In Standby -> HDMIPlayback is preserved in snapshot, but excluded from physical RF allocations
	standbySnap := activeSnap
	standbySnap.InStandby = true

	// Verify HDMIPlayback observation is STILL present in snapshot
	require.NotNil(t, standbySnap.HDMIPlayback)
	assert.Equal(t, "1:0:19:283D:3FB:1:C00000:0:0:0:", standbySnap.HDMIPlayback.ServiceRef)

	// Extract allocations for Standby: HDMI live TV is NOT consuming an RF frontend
	allocsStandby := ExtractExternalAllocationsFromSnapshot(standbySnap, topo, nil)
	require.Len(t, allocsStandby, 1, "expected only local DVR recording to occupy RF in standby")
	assert.Equal(t, "local_timer_dvr", allocsStandby[0].Source)
}

func TestExtractExternalAllocations_CorrelatesXG2GStreams(t *testing.T) {
	muxStream, _ := ParseServiceRef("1:0:19:283F:3FB:1:C00000:0:0:0:")
	topo := ReceiverTopology{
		Inputs: []PhysicalInput{{ID: "in-0", Label: "Input 1", DeliveryType: DeliveryLegacyUniversal}},
		Demodulators: []Demodulator{
			{ID: "tuner_a", InputID: "in-0"},
			{ID: "tuner_b", InputID: "in-0"},
		},
	}

	snap := ReceiverRuntimeSnapshot{
		StreamPresence: StreamPresenceActive,
		ReportedStreams: []ObservedStream{
			{
				RawRef:      "1:0:19:283F:3FB:1:C00000:0:0:0:",
				MultiplexID: &muxStream,
				TunerIndex:  0, // Tuner 0 -> demod "tuner_a"
				Evidence:    EvidenceObserved,
			},
			{
				RawRef:      "1:0:19:283F:3FB:1:C00000:0:0:0:",
				MultiplexID: &muxStream,
				TunerIndex:  1, // Tuner 1 -> demod "tuner_b"
				Evidence:    EvidenceObserved,
			},
		},
	}

	// Suppose tuner_a is owned by an active xg2g session, tuner_b is an external client (e.g. VLC)
	activeXG2G := map[DemodulatorID]bool{
		"tuner_a": true,
	}

	allocs := ExtractExternalAllocationsFromSnapshot(snap, topo, activeXG2G)
	require.Len(t, allocs, 1, "xg2g-owned tuner_a must be skipped from external allocations")
	assert.Equal(t, "external_stream_client", allocs[0].Source)
	require.NotNil(t, allocs[0].DemodID)
	assert.Equal(t, DemodulatorID("tuner_b"), *allocs[0].DemodID)
}

func TestCollectRuntimeSnapshot_StandbySkipsGetCurrent(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	var getCurrentCalled bool
	client := &mockPollerClient{
		statusInfoFunc: func(ctx context.Context) (*openwebif.StatusInfo, error) {
			return &openwebif.StatusInfo{
				InStandby: "true",
			}, nil
		},
		currentFunc: func(ctx context.Context) (*openwebif.CurrentInfo, error) {
			getCurrentCalled = true
			return &openwebif.CurrentInfo{}, nil
		},
	}

	snap := CollectRuntimeSnapshot(ctx, client, now)

	assert.True(t, snap.InStandby)
	assert.Equal(t, EvidenceObserved, snap.StandbyEvidence)
	assert.False(t, getCurrentCalled, "GetCurrent must NOT be called when receiver is in standby")
}

func TestExternalSyncPoller_SyncOnce_TopologyElevation(t *testing.T) {
	ctx := context.Background()

	var aboutCalls int
	client := &mockPollerClient{
		aboutFunc: func(ctx context.Context) (*openwebif.AboutInfo, error) {
			aboutCalls++
			var about openwebif.AboutInfo
			about.Info.Model = "Vu+ Uno 4K SE"
			about.Info.Tuners = []openwebif.AboutTuner{
				{Name: "Tuner A", Type: "DVB-S2"},
			}
			return &about, nil
		},
		statusInfoFunc: func(ctx context.Context) (*openwebif.StatusInfo, error) {
			return &openwebif.StatusInfo{InStandby: "true"}, nil
		},
	}

	svc, err := NewService(ReceiverTopology{Confidence: ConfidenceDefault}, EvaluationModeAuditOnly)
	require.NoError(t, err)

	poller := NewExternalSyncPoller(client, svc, 10*time.Second, zerolog.Nop())

	err = poller.SyncOnce(ctx)
	require.NoError(t, err)

	assert.Equal(t, ConfidenceObserved, svc.Topology().Confidence)
	assert.Equal(t, "Vu+ Uno 4K SE", svc.Topology().Model)
	assert.Equal(t, 1, aboutCalls, "About should only be called once during SyncOnce")
}
