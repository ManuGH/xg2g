// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

//go:build integration_fast || integration

package test

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/ManuGH/xg2g/internal/stream/ingest/livesource"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
	"github.com/ManuGH/xg2g/internal/stream/ingest/pipeline"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTopologyService simulates hardware tuner admission governance for integration testing.
type stubTopologyService struct {
	mu           sync.Mutex
	maxTuners    int
	allocations  map[string]string // sessionID -> transponderKey
	transponders map[string]int    // transponderKey -> count
}

func newStubTopologyService(maxTuners int) *stubTopologyService {
	return &stubTopologyService{
		maxTuners:    maxTuners,
		allocations:  make(map[string]string),
		transponders: make(map[string]int),
	}
}

func (s *stubTopologyService) ReserveStreamLeaseAtomic(serviceRef string, sessionID string, priority receivertopology.Priority, ttl time.Duration) (*receivertopology.Lease, receivertopology.AllocationDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tpKey := openwebif.ParseTransponderKey(serviceRef)

	// If session already holds a lease on this transponder
	if curTP, ok := s.allocations[sessionID]; ok && curTP == tpKey {
		return &receivertopology.Lease{SessionID: sessionID}, receivertopology.AllocationDecision{Allowed: true}, nil
	}

	// If transponder already has an active tuner assigned
	if count, ok := s.transponders[tpKey]; ok && count > 0 {
		s.allocations[sessionID] = tpKey
		s.transponders[tpKey]++
		return &receivertopology.Lease{SessionID: sessionID}, receivertopology.AllocationDecision{Allowed: true}, nil
	}

	// New transponder requires an unoccupied physical tuner
	if len(s.transponders) >= s.maxTuners {
		return nil, receivertopology.AllocationDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("physical tuner limit (%d) reached: all tuners occupied", s.maxTuners),
		}, nil
	}

	s.allocations[sessionID] = tpKey
	s.transponders[tpKey] = 1
	return &receivertopology.Lease{SessionID: sessionID}, receivertopology.AllocationDecision{Allowed: true}, nil
}

func (s *stubTopologyService) ReleaseStream(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tpKey, ok := s.allocations[sessionID]
	if !ok {
		return false
	}
	delete(s.allocations, sessionID)
	s.transponders[tpKey]--
	if s.transponders[tpKey] <= 0 {
		delete(s.transponders, tpKey)
	}
	return true
}

func setupTwinPipeline(t *testing.T, twin *openwebif.DigitalTwin, topSvc pipeline.TopologyService, warmHold time.Duration) (*session.Manager, *livesource.Provider) {
	t.Helper()

	u, err := url.Parse(twin.URL())
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	connectorCfg := pipeline.DefaultTestConnectorConfig(twin.URL(), port)
	connectorCfg.TopologyService = topSvc
	if topSvc != nil {
		connectorCfg.RequireTopology = true
	}

	connector := pipeline.NewLivePipelineConnector(connectorCfg)
	mgrCfg := session.ManagerConfig{
		WarmHoldDuration: warmHold,
		ConnectTimeout:   5 * time.Second,
	}
	mgr := session.NewManager(mgrCfg, connector)
	t.Cleanup(func() { _ = mgr.Close() })

	provider := livesource.NewProvider(mgr, u.Hostname(), port)
	return mgr, provider
}

// TestDigitalTwin_E2E_StreamPipeline_IngestAndReadiness verifies end-to-end streaming from
// the Digital Twin into ingest.Pipeline / LiveSource, ensuring PAT/PMT extraction, H.264
// parameter set discovery, clean entry points, and joinability.
func TestDigitalTwin_E2E_StreamPipeline_IngestAndReadiness(t *testing.T) {
	twin := openwebif.NewVuUno4KTwin(t)
	_, provider := setupTwinPipeline(t, twin, nil, 0)

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	src, err := provider.AcquireLiveSource(ctx, sRef)
	require.NoError(t, err, "Acquiring live source from Digital Twin must succeed")
	defer src.Release()

	preamble, reader, err := src.Attach(ctx, 3*time.Second)
	require.NoError(t, err, "Attaching to primed live source must succeed")
	require.NotNil(t, reader)
	defer reader.Close()

	// Verify preamble packets
	require.NotEmpty(t, preamble, "Preamble must contain PAT/PMT packets")
	require.Equal(t, 0, len(preamble)%188, "Preamble must consist of exact 188-byte TS packets")
	require.Equal(t, byte(0x47), preamble[0], "Preamble must start with sync byte 0x47")

	// Read TS packets from live subscriber reader
	buf := make([]byte, 10*188)
	n, err := io.ReadFull(reader, buf)
	require.NoError(t, err, "Reading from live subscriber reader must succeed")
	require.Equal(t, 10*188, n)

	for i := 0; i < n; i += 188 {
		assert.Equal(t, byte(0x47), buf[i], "Packet at byte offset %d must start with 0x47 sync byte", i)
	}

	// Verify readiness facts emitted by media parser
	facts := src.Facts()
	assert.True(t, facts.HasPAT, "PAT must be detected")
	assert.True(t, facts.HasPMT, "PMT must be detected")
	assert.Equal(t, uint16(0x0100), facts.VideoPID, "Video PID must be 0x0100")
	assert.Equal(t, "h264", facts.VideoCodec, "Video codec must be h264")
	assert.True(t, facts.ParameterSetsSeen, "Parameter sets (SPS/PPS) must be detected")
	assert.Greater(t, facts.CleanEntryPoints, uint64(0), "Clean entry points must be > 0")
	assert.Greater(t, facts.CleanAccessUnits, uint64(0), "Clean access units must be > 0")
	assert.True(t, facts.Joinable(), "Live stream must be joinable for clients")
	assert.True(t, facts.Descrambled(), "Live stream must be verified as clear/descrambled")

	twin.AssertNoAssertionFailure(t)
}

// TestDigitalTwin_E2E_StreamPipeline_PCRProgression verifies that the TS stream emitted by
// the Digital Twin exhibits strictly monotonic PCR timestamps across successive batches.
func TestDigitalTwin_E2E_StreamPipeline_PCRProgression(t *testing.T) {
	twin := openwebif.NewVuUno4KTwin(t)
	_, provider := setupTwinPipeline(t, twin, nil, 0)

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	src, err := provider.AcquireLiveSource(ctx, sRef)
	require.NoError(t, err)
	defer src.Release()

	_, reader, err := src.Attach(ctx, 3*time.Second)
	require.NoError(t, err)
	defer reader.Close()

	// Read packets and track PCR progression
	pcrValues := make([]uint64, 0, 10)
	pkt := make([]byte, 188)
	for i := 0; i < 40; i++ {
		_, err := io.ReadFull(reader, pkt)
		require.NoError(t, err)
		h, ok := normalizer.ParseTSPacket(pkt)
		if ok && h.HasPCR && h.PID == 0x0100 {
			pcrValues = append(pcrValues, h.PCR)
		}
	}

	require.GreaterOrEqual(t, len(pcrValues), 3, "Must observe at least 3 PCR samples")
	for i := 1; i < len(pcrValues); i++ {
		assert.Greater(t, pcrValues[i], pcrValues[i-1], "PCR timestamp at index %d must strictly exceed previous", i)
	}

	twin.AssertNoAssertionFailure(t)
}

// TestDigitalTwin_E2E_StreamPipeline_ReceiverCrashGracefulHandling verifies that an unmitigated
// receiver tuner crash (e.g. OpenATV dvb/dvb.cpp:1452 on over-allocation) fails closed gracefully,
// leaving active streams unaffected and allowing self-healing when a tuner becomes free.
func TestDigitalTwin_E2E_StreamPipeline_ReceiverCrashGracefulHandling(t *testing.T) {
	twin := openwebif.NewVuUno4KTwin(t,
		openwebif.WithPhysicalTuners(2),
		openwebif.WithCrashOnTunerExhaustion(true),
	)
	_, provider := setupTwinPipeline(t, twin, nil, 0)

	ctx := context.Background()

	// Stream 1 on Transponder 1: Das Erste HD (occupies physical tuner 0)
	src1, err := provider.AcquireLiveSource(ctx, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	require.NoError(t, err)

	_, r1, err := src1.Attach(ctx, 3*time.Second)
	require.NoError(t, err)

	// Stream 2 on Transponder 2: RTL Television (occupies physical tuner 1)
	src2, err := provider.AcquireLiveSource(ctx, "1:0:1:6DCA:44D:1:C00000:0:0:0:")
	require.NoError(t, err)
	defer src2.Release()

	_, r2, err := src2.Attach(ctx, 3*time.Second)
	require.NoError(t, err)
	defer r2.Close()

	assert.Equal(t, 2, twin.ActiveTunersCount(), "Both physical tuners must be active")

	// Attempt Stream 3 on Transponder 3: ProSieben (exhausts 2 tuners -> OpenATV assertion crash!)
	src3, err := provider.AcquireLiveSource(ctx, "1:0:1:6DCB:453:1:C00000:0:0:0:")
	require.Error(t, err, "Stream 3 must be refused because receiver crashed on tuner exhaustion")
	assert.Nil(t, src3)
	assert.Contains(t, err.Error(), "503", "Error must report upstream 503")

	// Invariant: The daemon is resilient. Streams 1 and 2 remain alive and healthy.
	buf := make([]byte, 188)
	_, err = io.ReadFull(r1, buf)
	require.NoError(t, err, "Stream 1 must continue reading without error")
	_, err = io.ReadFull(r2, buf)
	require.NoError(t, err, "Stream 2 must continue reading without error")

	// Invariant: Receiver registered the intentional assertion crash
	twin.AssertTunerCrashOccurred(t)

	// Now release Stream 1 to free physical tuner 0
	_ = r1.Close()
	src1.Release()

	// Wait for tuner 0 to be released on the twin
	require.Eventually(t, func() bool {
		return twin.ActiveTunersCount() == 1
	}, 1*time.Second, 10*time.Millisecond, "Tuner must be freed after src1 release")

	// Retry Stream 3 on Transponder 3: must now succeed since a physical tuner is available
	src3Retry, err := provider.AcquireLiveSource(ctx, "1:0:1:6DCB:453:1:C00000:0:0:0:")
	require.NoError(t, err, "Stream 3 must succeed after a physical tuner was freed")
	require.NotNil(t, src3Retry)
	defer src3Retry.Release()

	_, r3, err := src3Retry.Attach(ctx, 3*time.Second)
	require.NoError(t, err)
	defer r3.Close()

	_, err = io.ReadFull(r3, buf)
	require.NoError(t, err, "Stream 3 reader must deliver valid TS packets")
}

// TestDigitalTwin_E2E_StreamPipeline_TopologyGuardPreventsReceiverCrash verifies that
// upstream admission governance via TopologyService intercepts requests BEFORE they dial
// the receiver, completely eliminating the risk of OpenATV tuner assertion crashes.
func TestDigitalTwin_E2E_StreamPipeline_TopologyGuardPreventsReceiverCrash(t *testing.T) {
	twin := openwebif.NewVuUno4KTwin(t,
		openwebif.WithPhysicalTuners(2),
		openwebif.WithCrashOnTunerExhaustion(true),
	)
	topSvc := newStubTopologyService(2)
	_, provider := setupTwinPipeline(t, twin, topSvc, 0)

	ctx := context.Background()

	// Stream 1 on Transponder 1: Allowed
	src1, err := provider.AcquireLiveSource(ctx, "1:0:19:283D:3FB:1:C00000:0:0:0:")
	require.NoError(t, err)
	defer src1.Release()

	// Stream 2 on Transponder 2: Allowed
	src2, err := provider.AcquireLiveSource(ctx, "1:0:1:6DCA:44D:1:C00000:0:0:0:")
	require.NoError(t, err)
	defer src2.Release()

	// Attempt Stream 3 on Transponder 3: Topology governance MUST reject admission
	src3, err := provider.AcquireLiveSource(ctx, "1:0:1:6DCB:453:1:C00000:0:0:0:")
	require.Error(t, err, "Topology service must reject 3rd transponder")
	assert.Nil(t, src3)
	assert.ErrorIs(t, err, pipeline.ErrAdmissionDenied, "Admission must fail with ErrAdmissionDenied")

	// CRITICAL GOVERNANCE INVARIANT:
	// Because topology governance intercepted the call, the receiver was NEVER dialed
	// for the third transponder, and NO OpenATV assertion crash occurred!
	twin.AssertNoAssertionFailure(t)
}
