package manager

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/audiotopology"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionAudioTopologyMonitor_DebouncesStructuralChanges(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	now := time.Now()

	// Initial Topology: AC3 2.0 (PID 6122)
	pmt20 := []audiotopology.PMTTrackObservation{
		{PID: 6122, Codec: "ac3", Channels: 2, Language: "deu"},
	}
	topo20 := audiotopology.BuildTopology("sref1", pmt20, nil, nil, now)

	// Target Topology: AC3 5.1 (PID 6122)
	pmt51 := []audiotopology.PMTTrackObservation{
		{PID: 6122, Codec: "ac3", Channels: 6, Language: "deu"},
	}
	topo51 := audiotopology.BuildTopology("sref1", pmt51, nil, nil, now.Add(time.Minute))

	var currentProbe audiotopology.AudioTopology = topo20
	probeFn := func(ctx context.Context, sessionID, serviceRef, inputURL string) (audiotopology.AudioTopology, error) {
		return currentProbe, nil
	}

	cfg := AudioTopologyMonitorConfig{
		PollInterval:          100 * time.Millisecond,
		ProbeTimeout:          50 * time.Millisecond,
		RequiredConfirmations: 2,
	}

	monitor := NewSessionAudioTopologyMonitor("sess1", "sref1", "http://stream", topo20, cfg, probeFn, logger)

	var receivedChanges []audiotopology.TopologyChange
	monitor.SetChangeCallback(func(change audiotopology.TopologyChange, newTopo audiotopology.AudioTopology) {
		receivedChanges = append(receivedChanges, change)
	})

	ctx := context.Background()

	// Probe 1: Same topology -> No change
	monitor.PollOnce(ctx)
	assert.Equal(t, topo20.StructuralRevision, monitor.CurrentTopology().StructuralRevision)
	assert.Empty(t, receivedChanges)

	// Stream switches to 5.1
	currentProbe = topo51

	// Probe 2 (First 5.1 observation): Candidate observed, NOT yet adopted (requires 2 confirmations)
	monitor.PollOnce(ctx)
	assert.Equal(t, topo20.StructuralRevision, monitor.CurrentTopology().StructuralRevision, "should not adopt after only 1 probe")
	assert.Empty(t, receivedChanges)

	// Transient glitch: Probe 3 returns 2.0 again -> candidate reset
	currentProbe = topo20
	monitor.PollOnce(ctx)
	assert.Equal(t, topo20.StructuralRevision, monitor.CurrentTopology().StructuralRevision)
	assert.Empty(t, receivedChanges)

	// Stream stays 5.1 continuously
	currentProbe = topo51

	// Probe 4 (First 5.1 observation of second attempt): count = 1
	monitor.PollOnce(ctx)
	assert.Equal(t, topo20.StructuralRevision, monitor.CurrentTopology().StructuralRevision)
	assert.Empty(t, receivedChanges)

	// Probe 5 (Second 5.1 observation): count = 2 -> Stabilized & adopted!
	monitor.PollOnce(ctx)
	assert.Equal(t, topo51.StructuralRevision, monitor.CurrentTopology().StructuralRevision, "should adopt after 2 confirmed probes")
	require.Len(t, receivedChanges, 1)
	assert.True(t, receivedChanges[0].IsStructural)
	assert.Contains(t, receivedChanges[0].Summary, "channels 2 -> 6")
}

func TestSessionAudioTopologyMonitor_MetadataChangeAdoptedImmediately(t *testing.T) {
	logger := zerolog.New(os.Stderr)
	now := time.Now()

	pmt := []audiotopology.PMTTrackObservation{
		{PID: 6120, Codec: "mp2", Channels: 2, Language: "deu"},
	}
	topo1 := audiotopology.BuildTopology("sref1", pmt, nil, nil, now)

	e2 := []audiotopology.Enigma2TrackObservation{
		{PID: 6120, Description: "Stereo (Standard)"},
	}
	topo2 := audiotopology.BuildTopology("sref1", pmt, nil, e2, now.Add(time.Second))

	var currentProbe audiotopology.AudioTopology = topo1
	probeFn := func(ctx context.Context, sessionID, serviceRef, inputURL string) (audiotopology.AudioTopology, error) {
		return currentProbe, nil
	}

	cfg := AudioTopologyMonitorConfig{
		RequiredConfirmations: 2,
	}

	monitor := NewSessionAudioTopologyMonitor("sess1", "sref1", "http://stream", topo1, cfg, probeFn, logger)

	var receivedChanges []audiotopology.TopologyChange
	monitor.SetChangeCallback(func(change audiotopology.TopologyChange, newTopo audiotopology.AudioTopology) {
		receivedChanges = append(receivedChanges, change)
	})

	ctx := context.Background()
	currentProbe = topo2

	// Single probe with metadata change should be adopted immediately
	monitor.PollOnce(ctx)
	assert.Equal(t, topo2.MetadataRevision, monitor.CurrentTopology().MetadataRevision)
	assert.Equal(t, topo1.StructuralRevision, monitor.CurrentTopology().StructuralRevision)
	require.Len(t, receivedChanges, 1)
	assert.True(t, receivedChanges[0].IsMetadataOnly)
}
