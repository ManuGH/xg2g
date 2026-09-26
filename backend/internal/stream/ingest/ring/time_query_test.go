// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// mockTimeCore provides deterministic canonical timing records and PSI sections for tests.
type mockTimeCore struct {
	mu           sync.Mutex
	epoch        mediafacts.TimelineEpoch
	hasPMT       bool
	videoPID     uint16
	audioPID     uint16
	genRAPEvents bool
	customResult func(startOffset int64, chunk []byte) mediafacts.ParseResult
}

func (m *mockTimeCore) Ingest(ctx context.Context, startOffset int64, chunk []byte) (mediafacts.ParseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.customResult != nil {
		return m.customResult(startOffset, chunk), nil
	}

	numPackets := len(chunk) / TSPacketSize
	var events []mediafacts.Event
	var records []mediafacts.TimingRecord

	for i := 0; i < numPackets; i++ {
		pktOffset := startOffset + int64(i*TSPacketSize)
		// Packet with PUSI (byte 1 has 0x40) represents a video RAP
		if chunk[i*TSPacketSize+1]&0x40 != 0 {
			events = append(events, mediafacts.Event{
				Kind:     mediafacts.EventRandomAccessPoint,
				Offset:   pktOffset,
				Joinable: chunk[i*TSPacketSize+2] != 0xEE, // 0xEE marks non-joinable (e.g. scrambled)
			})
			pts := pktOffset * 90
			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:      m.epoch,
					PID:        m.videoPID,
					HasPTS:     true,
					PTS90k:     pts,
					ObservedAt: pktOffset,
					SubjectAt:  pktOffset,
				},
			})
			records = append(records, mediafacts.TimingRecord{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:      m.epoch,
					PID:        m.videoPID,
					HasPTS:     true,
					PTS90k:     pts,
					ObservedAt: pktOffset,
					SubjectAt:  pktOffset,
				},
			})
		}
	}

	patSec := []byte{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}
	pmtSec := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}

	return mediafacts.ParseResult{
		Coverage:               mediafacts.ParseCoverageComplete,
		ProcessedThroughOffset: startOffset + int64(len(chunk)),
		Events:                 events,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records:   records,
		},
		Facts: mediafacts.Facts{
			HasPAT:     true,
			HasPMT:     m.hasPMT,
			PMTPID:     4096,
			VideoPID:   m.videoPID,
			VideoCodec: CodecH264,
		},
		PSI: mediafacts.ActivePSI{
			PATSections: [][]byte{patSec},
			PMTSections: [][]byte{pmtSec},
		},
	}, nil
}

func (m *mockTimeCore) SetTargetProgram(ctx context.Context, programNumber uint16) (mediafacts.ParseResult, error) {
	return mediafacts.ParseResult{Coverage: mediafacts.ParseCoverageComplete}, nil
}

func (m *mockTimeCore) Reset() {}

func makeTSPacket(pusi bool, nonJoinable bool, tag byte) []byte {
	pkt := make([]byte, TSPacketSize)
	pkt[0] = SyncByte
	if pusi {
		pkt[1] = 0x40
	}
	if nonJoinable {
		pkt[2] = 0xEE
	} else {
		pkt[2] = tag
	}
	return pkt
}

func rapRecord(pt mediafacts.TimingPoint) mediafacts.TimingRecord {
	return mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypeRandomAccessPoint, RAP: pt}
}

func pesRecord(pt mediafacts.TimingPoint) mediafacts.TimingRecord {
	return mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypePES, PES: pt}
}

func TestMasterRing_PresentationTimeline_ClampedToRingWindow(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	// 10 packets buffer capacity = 1880 bytes
	r := NewMasterRingWithCore(10*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest epoch 1 start discontinuity
	core.customResult = func(startOffset int64, chunk []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(chunk)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							ObservedAt:    0,
							HasEpochAfter: true,
							EpochAfter:    1,
						},
					},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}
	_, err := r.Push(ctx, makeTSPacket(false, false, 0))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	core.customResult = nil // revert to mock time core default

	// Push 4 packets: 2 RAPs (packet 0 at 188, packet 2 at 564)
	chunk1 := append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 2)...)
	chunk1 = append(chunk1, makeTSPacket(true, false, 3)...)
	chunk1 = append(chunk1, makeTSPacket(false, false, 4)...)
	_, err = r.Push(ctx, chunk1)
	if err != nil {
		t.Fatalf("Push chunk1 failed: %v", err)
	}

	tr := r.Timeline()
	pt, ok := tr.PresentationTimeline(1)
	if !ok {
		t.Fatalf("PresentationTimeline(1) ok=false")
	}

	if pt.Epoch != 1 {
		t.Errorf("Epoch = %d, want 1", pt.Epoch)
	}
	if pt.TotalRAPs != 2 || pt.JoinableRAPs != 2 {
		t.Errorf("TotalRAPs = %d, JoinableRAPs = %d, want 2/2", pt.TotalRAPs, pt.JoinableRAPs)
	}
	if len(pt.Tracks) != 1 || pt.Tracks[0].PID != 256 {
		t.Fatalf("Tracks = %+v, want 1 track PID 256", pt.Tracks)
	}
	if pt.Tracks[0].EarliestPTS90k != 188*90 || pt.Tracks[0].LatestPTS90k != 564*90 {
		t.Errorf("PTS range = %d..%d, want %d..%d", pt.Tracks[0].EarliestPTS90k, pt.Tracks[0].LatestPTS90k, 188*90, 564*90)
	}

	// Push 10 packets to cause tail eviction overtaking packet 0 (offset 188)
	overflow := make([]byte, 10*TSPacketSize)
	for i := 0; i < 10; i++ {
		copy(overflow[i*TSPacketSize:], makeTSPacket(i == 8, false, byte(i)))
	}
	_, err = r.Push(ctx, overflow)
	if err != nil {
		t.Fatalf("Push overflow failed: %v", err)
	}

	pt2, ok := tr.PresentationTimeline(1)
	if !ok {
		t.Fatalf("PresentationTimeline(1) after eviction ok=false")
	}

	tail := r.Tail()
	if pt2.StartOffset != tail {
		t.Errorf("Clamped StartOffset = %d, want tail %d", pt2.StartOffset, tail)
	}
	// Earlier RAP at 188 is pruned from [tail, head), so FirstRAPOffset must be >= tail
	if pt2.HasFirstRAP && pt2.FirstRAPOffset < tail {
		t.Errorf("FirstRAPOffset %d < tail %d", pt2.FirstRAPOffset, tail)
	}
}

func TestMasterRing_TimingPoints_ObservedAtDistinctFromSubjectAt(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	// 5 packets buffer = 940 bytes
	r := NewMasterRingWithCore(5*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest epoch 1 with SubjectAt=0, ObservedAt=500
	core.customResult = func(startOffset int64, chunk []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(chunk)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							ObservedAt:    0,
							HasEpochAfter: true,
							EpochAfter:    1,
						},
					},
					{
						Type: mediafacts.TimingRecordTypePES,
						PES: mediafacts.TimingPoint{
							Epoch:      1,
							PID:        256,
							HasPTS:     true,
							PTS90k:     1000,
							ObservedAt: 500, // observed at 500
							SubjectAt:  0,   // packet was at offset 0
						},
					},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, make([]byte, 3*TSPacketSize))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Tail is 0, so SubjectAt 0 is in [tail, head) -> timing point visible
	pt, ok := r.Timeline().PresentationTimeline(1)
	if !ok || len(pt.Tracks) != 1 || !pt.Tracks[0].HasPTS {
		t.Fatalf("Expected visible timing point, got ok:%v pt:%+v", ok, pt)
	}

	// Now push 4 packets to advance tail past 0 (tail becomes 376), but tail < ObservedAt (500)
	core.customResult = nil
	_, err = r.Push(ctx, make([]byte, 4*TSPacketSize))
	if err != nil {
		t.Fatalf("Push 4 packets failed: %v", err)
	}

	tail := r.Tail()
	if tail <= 0 {
		t.Fatalf("Expected tail > 0, got %d", tail)
	}

	// At this point: SubjectAt (0) < tail (188) <= ObservedAt (500).
	// Query must filter on SubjectAt: the timing point must NOT be included!
	pt2, ok2 := r.Timeline().PresentationTimeline(1)
	if ok2 {
		for _, trk := range pt2.Tracks {
			if trk.PID == 256 && trk.HasPTS && trk.EarliestPTS90k == 1000 {
				t.Fatalf("Timing point with SubjectAt=0 was falsely included after tail advanced to %d", tail)
			}
		}
	}
}

func TestMasterRing_SeekToTime_EnforcesJoinableAndActiveTopology(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest epoch 1 start
	core.customResult = func(startOffset int64, chunk []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(chunk)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							ObservedAt:    0,
							HasEpochAfter: true,
							EpochAfter:    1,
						},
					},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}
	_, _ = r.Push(ctx, makeTSPacket(false, false, 0))
	core.customResult = nil

	// Ingest 3 RAPs:
	// RAP 1: offset 188, Joinable=true, PTS=10000
	// RAP 2: offset 376, Joinable=false (non-joinable), PTS=20000
	// RAP 3: offset 564, Joinable=true, PTS=30000
	chunk := append(makeTSPacket(true, false, 1), makeTSPacket(true, true, 2)...) // packet 1 joinable, packet 2 non-joinable
	chunk = append(chunk, makeTSPacket(true, false, 3)...)                        // packet 3 joinable

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset + TSPacketSize, Joinable: false}, // non-joinable!
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset + 2*TSPacketSize, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: startOffset, ObservedAt: startOffset}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: startOffset + TSPacketSize, ObservedAt: startOffset + TSPacketSize}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 30000, SubjectAt: startOffset + 2*TSPacketSize, ObservedAt: startOffset + 2*TSPacketSize}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}
	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push chunk failed: %v", err)
	}

	// 1. SeekToTime target 20000 with Preceding mode:
	// MUST skip the non-joinable RAP 2 at 20000 and return joinable RAP 1 at 10000!
	res, err := r.SeekToTime(1, 20000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("SeekToTime failed: %v", err)
	}
	if !res.RAP.Joinable {
		t.Fatalf("SeekToTime returned non-joinable RAP!")
	}
	if res.RAP.PTS90k != 10000 {
		t.Errorf("Expected Joinable RAP at PTS 10000, got PTS %d", res.RAP.PTS90k)
	}

	// 2. NewPrimedSubscriberAtTime must also enforce joinability
	attach, sub, err := r.NewPrimedSubscriberAtTime(1, 20000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("NewPrimedSubscriberAtTime failed: %v", err)
	}
	if !attach.HasKeyframe || attach.KeyframeOffset != res.Offset {
		t.Errorf("Attach KeyframeOffset = %d, want %d", attach.KeyframeOffset, res.Offset)
	}
	_ = sub.Close()

	// 3. Topology unresolved check: when HasPMT is false
	core.hasPMT = false
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: false},
		}
	}
	_, err = r.Push(ctx, makeTSPacket(false, false, 0))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	_, err = r.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != ErrTopologyUnresolved {
		t.Errorf("Expected ErrTopologyUnresolved when HasPMT is false, got %v", err)
	}
}

func TestMasterRing_SetTargetProgram_WithoutIngest_FailsClosedOnOldProgram(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest program 1 RAP
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 5000, SubjectAt: startOffset, ObservedAt: startOffset}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}
	_, err := r.Push(ctx, makeTSPacket(true, false, 1))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Verify seek works in current generation
	_, err = r.SeekToTime(1, 5000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("Initial SeekToTime failed: %v", err)
	}

	// Trigger SetTargetProgram without ingesting new bytes
	// (Simulate user zapping to program 2)
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage: mediafacts.ParseCoverageComplete,
			Events:   []mediafacts.Event{{Kind: mediafacts.EventProgramIdentityChanged, Offset: r.Head()}},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: false},
		}
	}
	r.SetTargetProgram(ctx, 2)

	// Ingest 1 packet of program 2 at new head to establish new generation floor
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventProgramIdentityChanged, Offset: startOffset},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 5000, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0x11, 0x22, 0x33, 0x44}},
			},
		}
	}
	_, err = r.Push(ctx, makeTSPacket(false, false, 2))
	if err != nil {
		t.Fatalf("Push program 2 packet failed: %v", err)
	}

	// Seeking back into program 1 (offset < resumeFloor) must fail closed with ErrHistoricalProgramSeekUnsupported!
	_, err = r.SeekToTime(1, 5000, timeline.SeekModePreceding)
	if err != ErrHistoricalProgramSeekUnsupported {
		t.Errorf("Expected ErrHistoricalProgramSeekUnsupported seeking across program change, got %v", err)
	}
}

func TestMasterRing_ExtractWindow_PacketAlignedAndBoundaryRules(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest epoch 1 with 3 RAPs (188-byte aligned):
	// RAP 1: offset 0, PTS 10000
	// RAP 2: offset 376 (2 packets later), PTS 20000
	// RAP 3: offset 752 (2 packets later), PTS 30000
	chunk := make([]byte, 6*TSPacketSize)
	for i := 0; i < 6; i++ {
		p := chunk[i*TSPacketSize : (i+1)*TSPacketSize]
		p[0] = SyncByte
		p[1] = byte(i)
	}

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 752, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 376, ObservedAt: 376}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 30000, SubjectAt: 752, ObservedAt: 752}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Extract window [10000, 25000]:
	// StartRAP should be RAP 1 (offset 0, PTS 10000)
	// EndRAP should be RAP 3 (offset 752, PTS 30000 >= 25000)
	// Byte slice length: 752 - 0 = 752 bytes = 4 TS packets exactly
	slice, err := r.ExtractWindow(WindowRequest{
		Epoch:           1,
		StartPTS:        10000,
		EndPTS:          25000,
		IncludePreamble: true,
	})
	if err != nil {
		t.Fatalf("ExtractWindow failed: %v", err)
	}

	if slice.StartOffset != 0 || slice.EndOffset != 752 {
		t.Errorf("Window offsets = %d..%d, want 0..752", slice.StartOffset, slice.EndOffset)
	}
	if len(slice.Data)%TSPacketSize != 0 {
		t.Errorf("Data len %d not packet aligned", len(slice.Data))
	}
	if slice.PacketCount != 4 {
		t.Errorf("PacketCount = %d, want 4", slice.PacketCount)
	}
	if slice.Data[0] != SyncByte {
		t.Errorf("Data sync byte = 0x%X, want 0x47", slice.Data[0])
	}
	if len(slice.Preamble) == 0 {
		t.Errorf("Preamble was requested but returned empty")
	}
	if slice.ActualStartPTS90k != 10000 || slice.ActualEndPTS90k != 30000 {
		t.Errorf("Actual PTS = %d..%d, want 10000..30000", slice.ActualStartPTS90k, slice.ActualEndPTS90k)
	}
}

func TestMasterRing_ExtractWindow_AudioPESSpanningVideoRAPBoundary(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
		audioPID: 257,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Interleaved stream:
	// Packet 0: Video RAP 1 (offset 0, PTS 10000)
	// Packet 1: Audio PES start (offset 188, PTS 10000)
	// Packet 2: Video RAP 2 (offset 376, PTS 20000)
	// Packet 3: Audio PES continuation (offset 564, no PUSI)
	chunk := make([]byte, 4*TSPacketSize)
	for i := 0; i < 4; i++ {
		chunk[i*TSPacketSize] = SyncByte
	}

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					pesRecord(mediafacts.TimingPoint{Epoch: 1, PID: 257, HasPTS: true, PTS90k: 10000, SubjectAt: 188, ObservedAt: 188}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Extract window [10000, 20000]:
	// Should cut cleanly at EndRAP.Offset = 376 (packet-aligned 376 bytes, exactly 2 packets: 0 and 1)
	// Even though Audio PES continues into packet 3, raw TS packet alignment is strictly preserved!
	slice, err := r.ExtractWindow(WindowRequest{
		Epoch:    1,
		StartPTS: 10000,
		EndPTS:   20000,
	})
	if err != nil {
		t.Fatalf("ExtractWindow failed: %v", err)
	}

	if slice.StartOffset != 0 || slice.EndOffset != 376 {
		t.Errorf("Window range = %d..%d, want 0..376", slice.StartOffset, slice.EndOffset)
	}
	if len(slice.Data) != 376 {
		t.Errorf("len(Data) = %d, want 376", len(slice.Data))
	}
}

func TestMasterRing_ExtractWindow_MissingCanonicalEndRAP_FailsClosed(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest only 1 RAP at offset 0, no closing RAP
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, makeTSPacket(true, false, 1))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Attempt to extract window [10000, 20000] when no subsequent RAP exists
	// Must fail closed with ErrNoCanonicalEndBoundary!
	_, err = r.ExtractWindow(WindowRequest{
		Epoch:    1,
		StartPTS: 10000,
		EndPTS:   20000,
	})
	if err != ErrNoCanonicalEndBoundary {
		t.Errorf("Expected ErrNoCanonicalEndBoundary at live edge without closing RAP, got %v", err)
	}
}

func TestMasterRing_ExtractWindow_MaxWindowBytesGuard(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	// Capacity large enough
	r := NewMasterRingWithCore(20*1024*1024, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest two RAPs that are > 16 MiB apart (188-byte packet aligned)
	offset1 := int64(0)
	offset2 := int64(17*1024*1024/TSPacketSize) * TSPacketSize

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: offset1, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: offset2, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: offset1, ObservedAt: offset1}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 50000, SubjectAt: offset2, ObservedAt: offset2}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	chunk := make([]byte, offset2+TSPacketSize)
	for i := 0; i < len(chunk); i += TSPacketSize {
		chunk[i] = SyncByte
	}
	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	_, err = r.ExtractWindow(WindowRequest{
		Epoch:    1,
		StartPTS: 10000,
		EndPTS:   50000,
	})
	if err != ErrWindowTooLarge {
		t.Errorf("Expected ErrWindowTooLarge for 17 MiB window, got %v", err)
	}
}

func TestMasterRing_SubscriberReader_SeekToTime(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Push 4 packets: RAP at 0 (PTS 10000), packet 1, RAP at 376 (PTS 20000), packet 3
	chunk := make([]byte, 4*TSPacketSize)
	for i := 0; i < 4; i++ {
		chunk[i*TSPacketSize] = SyncByte
		chunk[i*TSPacketSize+1] = byte(i)
	}

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Create subscriber reader at head (offset 752)
	reader := r.NewSubscriberReader(r.Head())
	if reader.Offset() != 752 {
		t.Errorf("Reader initial offset = %d, want 752", reader.Offset())
	}

	// Seek backward to PTS 10000 (RAP at offset 0)
	seekRes, err := reader.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("reader.SeekToTime failed: %v", err)
	}
	if seekRes.Offset != 0 {
		t.Errorf("Seek offset = %d, want 0", seekRes.Offset)
	}
	if reader.Offset() != 0 {
		t.Errorf("Reader offset after seek = %d, want 0", reader.Offset())
	}

	// Next Read() MUST return the preamble packets first
	readBuf := make([]byte, 10*TSPacketSize)
	n, err := reader.Read(readBuf)
	if err != nil {
		t.Fatalf("reader.Read failed: %v", err)
	}
	if n < TSPacketSize {
		t.Fatalf("reader.Read returned %d bytes, want at least 1 packet", n)
	}
	if readBuf[0] != SyncByte {
		t.Errorf("First byte = 0x%X, want sync byte 0x47", readBuf[0])
	}
}

func TestMasterRing_Reader_SubsequentProgramChange(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest Program 1 with RAP at offset 0
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, makeTSPacket(true, false, 1))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Attach primed subscriber at time
	attach, reader, err := r.NewPrimedSubscriberAtTime(1, 10000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("NewPrimedSubscriberAtTime failed: %v", err)
	}
	if !attach.HasKeyframe || attach.KeyframeOffset != 0 {
		t.Errorf("Attach KeyframeOffset = %d, want 0", attach.KeyframeOffset)
	}

	// 1. Verify attach.Preamble carries the initial PMT PID 4096 (matching NewPrimedSubscriber contract)
	hasInitialPMT := false
	for i := 0; i+TSPacketSize <= len(attach.Preamble); i += TSPacketSize {
		pkt := attach.Preamble[i : i+TSPacketSize]
		pid := uint16(pkt[1]&0x1F)<<8 | uint16(pkt[2])
		if pid == 4096 {
			hasInitialPMT = true
		}
	}
	if !hasInitialPMT {
		t.Fatalf("expected initial preamble to carry PMT PID 4096")
	}

	// Reader reads stream bytes starting at keyframe offset 0 (tag 1)
	pkt0Buf := make([]byte, TSPacketSize)
	nPkt0, err := reader.Read(pkt0Buf)
	if err != nil || nPkt0 != TSPacketSize {
		t.Fatalf("reader.Read packet 0 failed: n=%d, err=%v", nPkt0, err)
	}
	if pkt0Buf[2] != 1 {
		t.Fatalf("expected packet 0 with tag 1, got tag %d", pkt0Buf[2])
	}

	// 2. Push Program 2 transition chunk (establishing identity change at offset 188).
	// Head becomes 376; resumeFloor becomes 376!
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventProgramIdentityChanged, Offset: startOffset},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:          mediafacts.DiscontinuityScopeProgram,
							ObservedAt:     startOffset,
							HasEpochBefore: true,
							EpochBefore:    1,
							HasEpochAfter:  true,
							EpochAfter:     2,
						},
					},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 5000, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0x11, 0x22, 0x33, 0x44}},
			},
		}
	}

	_, err = r.Push(ctx, makeTSPacket(false, false, 99))
	if err != nil {
		t.Fatalf("Push program 2 transition chunk failed: %v", err)
	}

	// 3. Reader attempts to read: it must remain blocked waiting for a keyframe at or after floor (376).
	readDone := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 10*TSPacketSize)
		n, rErr := reader.Read(buf)
		if rErr != nil {
			readErr <- rErr
			return
		}
		readDone <- append([]byte(nil), buf[:n]...)
	}()

	select {
	case <-readDone:
		t.Fatalf("reader unblocked before a keyframe at or after floor (376) was pushed")
	case <-time.After(50 * time.Millisecond):
		// Expected: reader is correctly waiting!
	}

	// 4. Push Program 2 keyframe chunk at offset 376 (>= resumeFloor) in Epoch 2.
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					rapRecord(mediafacts.TimingPoint{Epoch: 2, PID: 256, HasPTS: true, PTS90k: 50000, SubjectAt: startOffset, ObservedAt: startOffset}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 5000, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0x11, 0x22, 0x33, 0x44}},
			},
		}
	}

	_, err = r.Push(ctx, makeTSPacket(true, false, 2))
	if err != nil {
		t.Fatalf("Push program 2 keyframe chunk failed: %v", err)
	}

	// 5. Reader must now unblock, deliver new PMT (PID 5000), and never deliver old PMT or old payload.
	var afterBuf []byte
	select {
	case err := <-readErr:
		t.Fatalf("reader.Read failed after keyframe pushed: %v", err)
	case afterBuf = <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("reader timed out waiting for recovery")
	}

	hasNewPMT := false
	for i := 0; i+TSPacketSize <= len(afterBuf); i += TSPacketSize {
		pkt := afterBuf[i : i+TSPacketSize]
		pid := uint16(pkt[1]&0x1F)<<8 | uint16(pkt[2])
		if pid == 4096 {
			t.Errorf("Reader delivered stale PMT (PID 4096) after program change")
		}
		if pid == 5000 {
			hasNewPMT = true
		}
	}
	if !hasNewPMT {
		t.Errorf("Reader did not deliver new PMT (PID 5000) after program change")
	}

	// Read next packet: must be Program 2's packet (tag 2), NOT old packet
	pkt2Buf := make([]byte, TSPacketSize)
	n4, err := reader.Read(pkt2Buf)
	if err != nil {
		t.Fatalf("reader.Read after preamble failed: %v", err)
	}
	if n4 != TSPacketSize || pkt2Buf[2] != 2 {
		t.Fatalf("expected packet 2 with tag 2, got %d bytes, tag %d", n4, pkt2Buf[2])
	}

	// 6. Verify TimelineEpoch and Program Change interplay:
	pt1, ok1 := r.Timeline().PresentationTimeline(1)
	if !ok1 {
		t.Fatalf("PresentationTimeline(1) failed")
	}
	if !pt1.Closed || pt1.EndOffset != 188 {
		t.Errorf("Epoch 1 PresentationTimeline: Closed=%v EndOffset=%d, want Closed=true EndOffset=188", pt1.Closed, pt1.EndOffset)
	}

	pt2, ok2 := r.Timeline().PresentationTimeline(2)
	if !ok2 {
		t.Fatalf("PresentationTimeline(2) failed")
	}
	if pt2.Closed || pt2.StartOffset != 188 {
		t.Errorf("Epoch 2 PresentationTimeline: Closed=%v StartOffset=%d, want Closed=false StartOffset=188", pt2.Closed, pt2.StartOffset)
	}

	// SeekToTime on historical Epoch 1 (before resumeFloor 376) fails closed with ErrHistoricalProgramSeekUnsupported:
	_, err = r.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != ErrHistoricalProgramSeekUnsupported {
		t.Errorf("SeekToTime on historical Epoch 1: got %v, want ErrHistoricalProgramSeekUnsupported", err)
	}

	// SeekToTime on active Epoch 2 (at offset 376 >= resumeFloor) succeeds with new preamble:
	seekRes, err := r.SeekToTime(2, 50000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("SeekToTime on active Epoch 2 failed: %v", err)
	}
	if seekRes.Offset != 376 {
		t.Errorf("SeekToTime Offset = %d, want 376", seekRes.Offset)
	}

	// ExtractWindow on historical Epoch 1 with IncludePreamble fails closed with ErrHistoricalProgramSeekUnsupported:
	_, err = r.ExtractWindow(WindowRequest{
		Epoch:           1,
		StartPTS:        10000,
		EndPTS:          10000,
		IncludePreamble: true,
	})
	if err != ErrHistoricalProgramSeekUnsupported {
		t.Errorf("ExtractWindow on historical Epoch 1 with preamble: got %v, want ErrHistoricalProgramSeekUnsupported", err)
	}
}

func TestMasterRing_ExtractWindow_HistoricalProgramWithPreamble_FailsClosed(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest Program 1 with RAP 1 at offset 0 (PTS 10000) and RAP 2 at offset 376 (PTS 20000)
	chunk := make([]byte, 4*TSPacketSize)
	for i := 0; i < 4; i++ {
		chunk[i*TSPacketSize] = SyncByte
	}

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Trigger program identity change (zap to Program 2)
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventProgramIdentityChanged, Offset: startOffset},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: startOffset, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 50000, SubjectAt: startOffset, ObservedAt: startOffset}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 5000, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0x11, 0x22, 0x33, 0x44}},
			},
		}
	}
	_, err = r.Push(ctx, makeTSPacket(true, false, 2))
	if err != nil {
		t.Fatalf("Push program 2 failed: %v", err)
	}

	// Extracting historical Program 1 window WITH preamble must fail closed with ErrHistoricalProgramSeekUnsupported!
	_, err = r.ExtractWindow(WindowRequest{
		Epoch:           1,
		StartPTS:        10000,
		EndPTS:          20000,
		IncludePreamble: true,
	})
	if err != ErrHistoricalProgramSeekUnsupported {
		t.Errorf("ExtractWindow with preamble on historical program: got %v, want ErrHistoricalProgramSeekUnsupported", err)
	}

	// Extracting historical Program 1 window WITHOUT preamble (raw TS) succeeds
	slice, err := r.ExtractWindow(WindowRequest{
		Epoch:           1,
		StartPTS:        10000,
		EndPTS:          20000,
		IncludePreamble: false,
	})
	if err != nil {
		t.Errorf("ExtractWindow without preamble on historical program failed: %v", err)
	}
	if slice.StartOffset != 0 || slice.EndOffset != 376 {
		t.Errorf("Slice range = %d..%d, want 0..376", slice.StartOffset, slice.EndOffset)
	}
	if len(slice.Preamble) != 0 {
		t.Errorf("Slice has preamble length %d, want 0", len(slice.Preamble))
	}
}

func TestMasterRing_Push_EndRAPWithoutPublishedBytes_FailsClosedBeforeCommit(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Push 2 packets (offsets 0 and 188). Total head would be 376.
	// But mock says EndRAP is at offset 376 (which is == want, so 0 bytes written for EndRAP!)
	chunk := append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 1)...)

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err == nil {
		t.Fatalf("Push succeeded unexpectedly for RAP without published bytes")
	}
	if !errors.Is(err, ErrEventBeyondProcessedBytes) {
		t.Fatalf("Push error = %v, want ErrEventBeyondProcessedBytes", err)
	}

	// Invariant: "No truth without corresponding bytes"
	// Verify ring bytes are completely unchanged:
	if r.Head() != 0 {
		t.Errorf("Head = %d, want 0", r.Head())
	}
	if r.BufferedBytes() != 0 {
		t.Errorf("BufferedBytes = %d, want 0", r.BufferedBytes())
	}

	// Verify timeline index is completely unchanged:
	timelines := r.Timeline().PresentationTimelines()
	if len(timelines) != 0 {
		t.Errorf("PresentationTimelines count = %d, want 0", len(timelines))
	}
	raps := r.timelineIndex.RAPsBetween(0, 1000)
	if len(raps) != 0 {
		t.Errorf("RAPs in index = %d, want 0", len(raps))
	}

	// Verify attach index is completely unchanged:
	if len(r.KeyframeOffsets()) != 0 {
		t.Errorf("KeyframeOffsets = %v, want empty", r.KeyframeOffsets())
	}
}

func TestMasterRing_Push_PESTimingBeyondProcessedBytes_FailsClosedBeforeCommit(t *testing.T) {
	t.Run("SubjectAtBeyond", func(t *testing.T) {
		core := &mockTimeCore{
			epoch:    1,
			hasPMT:   true,
			videoPID: 256,
		}

		r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
		defer r.Close()

		ctx := context.Background()

		// Push 2 packets (offsets 0 and 188). Total head would be 376.
		// PES timing record specifies SubjectAt: 200 (200 + 188 = 388 > 376)
		chunk := append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 1)...)

		core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(c)),
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				},
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records: []mediafacts.TimingRecord{
						{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
						rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
						{
							Type: mediafacts.TimingRecordTypePES,
							PES: mediafacts.TimingPoint{
								Epoch:      1,
								PID:        256,
								HasPTS:     true,
								PTS90k:     20000,
								SubjectAt:  200, // 200 + 188 = 388 > 376
								ObservedAt: 300,
							},
						},
					},
				},
				Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
				PSI: mediafacts.ActivePSI{
					PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
					PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
				},
			}
		}

		_, err := r.Push(ctx, chunk)
		if err == nil {
			t.Fatalf("Push succeeded unexpectedly for PES SubjectAt beyond published bytes")
		}
		if !errors.Is(err, ErrEventBeyondProcessedBytes) {
			t.Fatalf("Push error = %v, want ErrEventBeyondProcessedBytes", err)
		}

		// Invariant: "No truth without corresponding bytes"
		if r.Head() != 0 {
			t.Errorf("Head = %d, want 0", r.Head())
		}
		if r.BufferedBytes() != 0 {
			t.Errorf("BufferedBytes = %d, want 0", r.BufferedBytes())
		}
		timelines := r.Timeline().PresentationTimelines()
		if len(timelines) != 0 {
			t.Errorf("PresentationTimelines count = %d, want 0", len(timelines))
		}
	})

	t.Run("ObservedAtBeyond", func(t *testing.T) {
		core := &mockTimeCore{
			epoch:    1,
			hasPMT:   true,
			videoPID: 256,
		}

		r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
		defer r.Close()

		ctx := context.Background()

		// Push 2 packets (offsets 0 and 188). Total head would be 376.
		// PES timing record specifies ObservedAt: 400 (> 376)
		chunk := append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 1)...)

		core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
			return mediafacts.ParseResult{
				Coverage:               mediafacts.ParseCoverageComplete,
				ProcessedThroughOffset: startOffset + int64(len(c)),
				Events: []mediafacts.Event{
					{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				},
				Timing: mediafacts.TimingResult{
					Authority: mediafacts.TimingAuthorityCanonical,
					Records: []mediafacts.TimingRecord{
						{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
						rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
						{
							Type: mediafacts.TimingRecordTypePES,
							PES: mediafacts.TimingPoint{
								Epoch:      1,
								PID:        256,
								HasPTS:     true,
								PTS90k:     20000,
								SubjectAt:  0,
								ObservedAt: 400, // 400 > 376
							},
						},
					},
				},
				Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
				PSI: mediafacts.ActivePSI{
					PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
					PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
				},
			}
		}

		_, err := r.Push(ctx, chunk)
		if err == nil {
			t.Fatalf("Push succeeded unexpectedly for PES ObservedAt beyond published bytes")
		}
		if !errors.Is(err, ErrEventBeyondProcessedBytes) {
			t.Fatalf("Push error = %v, want ErrEventBeyondProcessedBytes", err)
		}

		// Invariant: "No truth without corresponding bytes"
		if r.Head() != 0 {
			t.Errorf("Head = %d, want 0", r.Head())
		}
		if r.BufferedBytes() != 0 {
			t.Errorf("BufferedBytes = %d, want 0", r.BufferedBytes())
		}
		timelines := r.Timeline().PresentationTimelines()
		if len(timelines) != 0 {
			t.Errorf("PresentationTimelines count = %d, want 0", len(timelines))
		}
	})
}

func TestMasterRing_SeekToTime_ExtractWindow_IncompletePreamble_FailsClosed(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// 1. PAT is present, but PMT sections are empty even though HasPMT == true
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 188, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 188, ObservedAt: 188}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: nil, // Missing PMT sections!
			},
		}
	}

	_, err := r.Push(ctx, append(makeTSPacket(true, false, 1), makeTSPacket(true, false, 2)...))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// SeekToTime must fail closed with ErrTopologyUnresolved
	_, err = r.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != ErrTopologyUnresolved {
		t.Errorf("SeekToTime with missing PMT sections: got %v, want ErrTopologyUnresolved", err)
	}

	// ExtractWindow with IncludePreamble: true must fail closed with ErrTopologyUnresolved
	_, err = r.ExtractWindow(WindowRequest{
		Epoch:           1,
		StartPTS:        10000,
		EndPTS:          20000,
		IncludePreamble: true,
	})
	if err != ErrTopologyUnresolved {
		t.Errorf("ExtractWindow with missing PMT sections: got %v, want ErrTopologyUnresolved", err)
	}

	// 2. PMT PID is 0 (patPID): emit would deliver PMT as PAT, so it must fail closed
	r.facts.PMTPID = 0
	r.activePSI.PMTSections = [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}}
	_, err = r.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != ErrTopologyUnresolved {
		t.Errorf("SeekToTime with PMTPID == 0: got %v, want ErrTopologyUnresolved", err)
	}

	// 3. PAT sections are empty
	r.facts.PMTPID = 4096
	r.activePSI.PATSections = nil
	_, err = r.SeekToTime(1, 10000, timeline.SeekModePreceding)
	if err != ErrTopologyUnresolved {
		t.Errorf("SeekToTime with empty PAT sections: got %v, want ErrTopologyUnresolved", err)
	}
}

func BenchmarkMasterRing_ExtractWindowLockHoldDuration(b *testing.B) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	// 20 MiB buffer capacity
	r := NewMasterRingWithCore(20*1024*1024, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest 4 MiB of packets between two RAPs
	numPackets := (4 * 1024 * 1024) / TSPacketSize
	payload := make([]byte, numPackets*TSPacketSize)
	for i := 0; i < numPackets; i++ {
		payload[i*TSPacketSize] = SyncByte
	}

	endRAPOffset := int64(len(payload)) - TSPacketSize

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: endRAPOffset, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 50000, SubjectAt: endRAPOffset, ObservedAt: endRAPOffset}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, payload)
	if err != nil {
		b.Fatalf("Push failed: %v", err)
	}

	req := WindowRequest{
		Epoch:    1,
		StartPTS: 10000,
		EndPTS:   50000,
	}

	b.ResetTimer()
	var totalDuration time.Duration
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := r.ExtractWindow(req)
		elapsed := time.Since(start)
		if err != nil {
			b.Fatalf("ExtractWindow failed: %v", err)
		}
		totalDuration += elapsed
	}
	if b.N > 0 {
		avgTime := totalDuration / time.Duration(b.N)
		b.Logf("Measured Average ExtractWindow execution time for 4 MiB payload: %v", avgTime)
	}
}

func TestMasterRing_ConcurrentRace_TimeQueriesAndCommits(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	// 20 packets capacity
	r := NewMasterRingWithCore(20*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ingest epoch 1
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}
	_, _ = r.Push(ctx, makeTSPacket(false, false, 0))
	core.customResult = nil

	var wg sync.WaitGroup
	const iterations = 200

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			chunk := append(makeTSPacket(true, false, byte(i)), makeTSPacket(false, false, byte(i))...)
			_, pushErr := r.Push(ctx, chunk)
			if pushErr != nil && pushErr != ErrRingClosed {
				return
			}
			time.Sleep(100 * time.Microsecond)
		}
	}()

	// Reader 1: SeekToTime
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			pts := int64(i * 188 * 90)
			_, _ = r.SeekToTime(1, pts, timeline.SeekModePreceding)
		}
	}()

	// Reader 2: PresentationTimeline
	wg.Add(1)
	go func() {
		defer wg.Done()
		tr := r.Timeline()
		for i := 0; i < iterations; i++ {
			_, _ = tr.PresentationTimeline(1)
		}
	}()

	// Reader 3: ExtractWindow
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			pts := int64(i * 188 * 90)
			_, _ = r.ExtractWindow(WindowRequest{
				Epoch:    1,
				StartPTS: pts,
				EndPTS:   pts + 10000,
			})
		}
	}()

	wg.Wait()
}

func TestMasterRing_SeekToTime_ErrorContractAndModes(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest Epoch 1 with 3 packets:
	// - Packet 0 (offset 0): RAP 1 with PTS 10000, Joinable: true
	// - Packet 1 (offset 188): PES with PTS 20000, no RAP
	// - Packet 2 (offset 376): RAP 2 with PTS 30000, Joinable: true
	// Retained presentation range for Epoch 1: [10000, 30000]
	chunk := append(append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 2)...), makeTSPacket(true, false, 3)...)

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					{
						Type: mediafacts.TimingRecordTypePES,
						PES:  mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 20000, SubjectAt: 188, ObservedAt: 188},
					},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 30000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Ingest Epoch 2 with 1 packet, but NO timing points with PTS (Epoch without timing)
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: startOffset, HasEpochBefore: true, EpochBefore: 1, HasEpochAfter: true, EpochAfter: 2}},
					{
						Type: mediafacts.TimingRecordTypePES,
						PES:  mediafacts.TimingPoint{Epoch: 2, PID: 256, HasPTS: false, SubjectAt: startOffset, ObservedAt: startOffset},
					},
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err = r.Push(ctx, makeTSPacket(false, false, 4))
	if err != nil {
		t.Fatalf("Push epoch 2 failed: %v", err)
	}

	t.Run("ErrEpochNotFound", func(t *testing.T) {
		modes := []timeline.SeekMode{timeline.SeekModePreceding, timeline.SeekModeFollowing, timeline.SeekModeNearest}
		for _, m := range modes {
			_, err := r.SeekToTime(999, 20000, m)
			if !errors.Is(err, ErrEpochNotFound) {
				t.Errorf("mode %v: got %v, want ErrEpochNotFound", m, err)
			}
		}
	})

	t.Run("ErrEpochNoTiming", func(t *testing.T) {
		modes := []timeline.SeekMode{timeline.SeekModePreceding, timeline.SeekModeFollowing, timeline.SeekModeNearest}
		for _, m := range modes {
			_, err := r.SeekToTime(2, 20000, m)
			if !errors.Is(err, ErrEpochNoTiming) {
				t.Errorf("mode %v: got %v, want ErrEpochNoTiming", m, err)
			}
		}
	})

	t.Run("SeekModePreceding_Bounds", func(t *testing.T) {
		// pts < earliestPTS (5000 < 10000) -> ErrPTSOutOfRange
		_, err := r.SeekToTime(1, 5000, timeline.SeekModePreceding)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 5000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts > latestPTS (35000 > 30000) -> ErrPTSOutOfRange
		_, err = r.SeekToTime(1, 35000, timeline.SeekModePreceding)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 35000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts == earliestPTS (10000) -> RAP 1 (offset 0)
		res, err := r.SeekToTime(1, 10000, timeline.SeekModePreceding)
		if err != nil || res.Offset != 0 {
			t.Errorf("pts 10000: got (%+v, %v), want offset 0", res, err)
		}

		// pts == latestPTS (30000) -> RAP 2 (offset 376)
		res, err = r.SeekToTime(1, 30000, timeline.SeekModePreceding)
		if err != nil || res.Offset != 376 {
			t.Errorf("pts 30000: got (%+v, %v), want offset 376", res, err)
		}

		// pts == 20000 (between RAPs) -> RAP 1 (offset 0)
		res, err = r.SeekToTime(1, 20000, timeline.SeekModePreceding)
		if err != nil || res.Offset != 0 {
			t.Errorf("pts 20000: got (%+v, %v), want offset 0", res, err)
		}
	})

	t.Run("SeekModeFollowing_Bounds", func(t *testing.T) {
		// pts < earliestPTS (5000 < 10000) -> ErrPTSOutOfRange
		_, err := r.SeekToTime(1, 5000, timeline.SeekModeFollowing)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 5000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts > latestPTS (35000 > 30000) -> ErrPTSOutOfRange
		_, err = r.SeekToTime(1, 35000, timeline.SeekModeFollowing)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 35000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts == earliestPTS (10000) -> RAP 1 (offset 0)
		res, err := r.SeekToTime(1, 10000, timeline.SeekModeFollowing)
		if err != nil || res.Offset != 0 {
			t.Errorf("pts 10000: got (%+v, %v), want offset 0", res, err)
		}

		// pts == latestPTS (30000) -> RAP 2 (offset 376)
		res, err = r.SeekToTime(1, 30000, timeline.SeekModeFollowing)
		if err != nil || res.Offset != 376 {
			t.Errorf("pts 30000: got (%+v, %v), want offset 376", res, err)
		}

		// pts == 20000 (between RAPs) -> RAP 2 (offset 376)
		res, err = r.SeekToTime(1, 20000, timeline.SeekModeFollowing)
		if err != nil || res.Offset != 376 {
			t.Errorf("pts 20000: got (%+v, %v), want offset 376", res, err)
		}
	})

	t.Run("SeekModeNearest_Bounds", func(t *testing.T) {
		// pts < earliestPTS (5000 < 10000) -> ErrPTSOutOfRange
		_, err := r.SeekToTime(1, 5000, timeline.SeekModeNearest)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 5000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts > latestPTS (35000 > 30000) -> ErrPTSOutOfRange
		_, err = r.SeekToTime(1, 35000, timeline.SeekModeNearest)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("pts 35000: got %v, want ErrPTSOutOfRange", err)
		}

		// pts == earliestPTS (10000) -> RAP 1 (offset 0)
		res, err := r.SeekToTime(1, 10000, timeline.SeekModeNearest)
		if err != nil || res.Offset != 0 {
			t.Errorf("pts 10000: got (%+v, %v), want offset 0", res, err)
		}

		// pts == latestPTS (30000) -> RAP 2 (offset 376)
		res, err = r.SeekToTime(1, 30000, timeline.SeekModeNearest)
		if err != nil || res.Offset != 376 {
			t.Errorf("pts 30000: got (%+v, %v), want offset 376", res, err)
		}

		// pts == 14000 (closer to 10000) -> RAP 1 (offset 0)
		res, err = r.SeekToTime(1, 14000, timeline.SeekModeNearest)
		if err != nil || res.Offset != 0 {
			t.Errorf("pts 14000: got (%+v, %v), want offset 0", res, err)
		}

		// pts == 26000 (closer to 30000) -> RAP 2 (offset 376)
		res, err = r.SeekToTime(1, 26000, timeline.SeekModeNearest)
		if err != nil || res.Offset != 376 {
			t.Errorf("pts 26000: got (%+v, %v), want offset 376", res, err)
		}
	})
}

func TestMasterRing_ExtractWindow_TrackDiscontinuities(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest 3 packets in Epoch 1:
	// - Packet 0 (offset 0): RAP 1 with PTS 10000, Joinable: true
	// - Packet 1 (offset 188): Audio track discontinuity on PID 257
	// - Packet 2 (offset 376): RAP 2 with PTS 30000, Joinable: true
	chunk := append(append(makeTSPacket(true, false, 1), makeTSPacket(false, false, 2)...), makeTSPacket(true, false, 3)...)

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 1}},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:      mediafacts.DiscontinuityScopeTrack,
							TrackPID:   257,
							ObservedAt: 188,
						},
					},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 30000, SubjectAt: 376, ObservedAt: 376}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	t.Run("AllowTrackDiscontinuities_ExposesInSlice", func(t *testing.T) {
		slice, err := r.ExtractWindow(WindowRequest{
			Epoch:                      1,
			StartPTS:                   10000,
			EndPTS:                     30000,
			RejectTrackDiscontinuities: false,
		})
		if err != nil {
			t.Fatalf("ExtractWindow failed: %v", err)
		}
		if !slice.HasTrackDiscontinuity {
			t.Errorf("slice.HasTrackDiscontinuity = false, want true")
		}
		if len(slice.TrackDiscontinuities) != 1 {
			t.Fatalf("len(TrackDiscontinuities) = %d, want 1", len(slice.TrackDiscontinuities))
		}
		disc := slice.TrackDiscontinuities[0]
		if disc.Scope != mediafacts.DiscontinuityScopeTrack || disc.TrackPID != 257 || disc.ObservedAt != 188 {
			t.Errorf("TrackDiscontinuity entry mismatch: %+v", disc)
		}
	})

	t.Run("RejectTrackDiscontinuities_FailsClosed", func(t *testing.T) {
		_, err := r.ExtractWindow(WindowRequest{
			Epoch:                      1,
			StartPTS:                   10000,
			EndPTS:                     30000,
			RejectTrackDiscontinuities: true,
		})
		if !errors.Is(err, ErrWindowHasTrackDiscontinuity) {
			t.Errorf("got %v, want ErrWindowHasTrackDiscontinuity", err)
		}
	})
}

func TestMasterRing_SeekToTime_VideoPIDScoped_AudioDoesNotWidenRange(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Ingest 4 packets (4 * 188 bytes = 752 bytes):
	// Packet 0: Video RAP 1 (offset 0, PTS 10000, PID 256)
	// Packet 1: Audio PES 1 (offset 188, PTS 5000, PID 257) - leading audio PTS
	// Packet 2: Video RAP 2 (offset 376, PTS 30000, PID 256)
	// Packet 3: Audio PES 2 (offset 564, PTS 45000, PID 257) - trailing audio PTS
	chunk := make([]byte, 4*TSPacketSize)
	copy(chunk[0:TSPacketSize], makeTSPacket(true, false, 0))
	copy(chunk[TSPacketSize:2*TSPacketSize], makeTSPacket(false, false, 1))
	copy(chunk[2*TSPacketSize:3*TSPacketSize], makeTSPacket(true, false, 2))
	copy(chunk[3*TSPacketSize:4*TSPacketSize], makeTSPacket(false, false, 3))

	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 376, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							ObservedAt:    0,
							HasEpochAfter: true,
							EpochAfter:    1,
						},
					},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
					pesRecord(mediafacts.TimingPoint{Epoch: 1, PID: 257, HasPTS: true, PTS90k: 5000, SubjectAt: 188, ObservedAt: 188}),
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 30000, SubjectAt: 376, ObservedAt: 376}),
					pesRecord(mediafacts.TimingPoint{Epoch: 1, PID: 257, HasPTS: true, PTS90k: 45000, SubjectAt: 564, ObservedAt: 564}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, chunk)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// 1. Audio leading PTS is 5000; Video range is [10000, 30000].
	// Seeking at PTS 6000 or 8000 must fail with ErrPTSOutOfRange, because video presentation starts at 10000.
	for _, mode := range []timeline.SeekMode{timeline.SeekModePreceding, timeline.SeekModeFollowing, timeline.SeekModeNearest} {
		_, err := r.SeekToTime(1, 6000, mode)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("SeekToTime(1, 6000, %v) error = %v, want ErrPTSOutOfRange", mode, err)
		}
	}

	// 2. Audio trailing PTS is 45000; Video range is [10000, 30000].
	// Seeking at PTS 35000 or 40000 must fail with ErrPTSOutOfRange, because video presentation ends at 30000.
	for _, mode := range []timeline.SeekMode{timeline.SeekModePreceding, timeline.SeekModeFollowing, timeline.SeekModeNearest} {
		_, err := r.SeekToTime(1, 35000, mode)
		if !errors.Is(err, ErrPTSOutOfRange) {
			t.Errorf("SeekToTime(1, 35000, %v) error = %v, want ErrPTSOutOfRange", mode, err)
		}
	}

	// 3. Seeking within [10000, 30000] succeeds on Video RAPs
	res, err := r.SeekToTime(1, 15000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("SeekToTime(1, 15000, Preceding) failed: %v", err)
	}
	if res.Offset != 0 || res.RAP.PTS90k != 10000 {
		t.Errorf("res mismatch: offset=%d, pts=%d", res.Offset, res.RAP.PTS90k)
	}

	res2, err := r.SeekToTime(1, 20000, timeline.SeekModeFollowing)
	if err != nil {
		t.Fatalf("SeekToTime(1, 20000, Following) failed: %v", err)
	}
	if res2.Offset != 376 || res2.RAP.PTS90k != 30000 {
		t.Errorf("res2 mismatch: offset=%d, pts=%d", res2.Offset, res2.RAP.PTS90k)
	}
}

func TestMasterRing_NewPrimedSubscriberAtTime_NoPreambleDoubleDelivery(t *testing.T) {
	core := &mockTimeCore{
		epoch:    1,
		hasPMT:   true,
		videoPID: 256,
	}

	r := NewMasterRingWithCore(100*TSPacketSize, core, WithCanonicalTimeline())
	defer r.Close()

	ctx := context.Background()

	// Push keyframe at offset 0 (tag 42)
	core.customResult = func(startOffset int64, c []byte) mediafacts.ParseResult {
		return mediafacts.ParseResult{
			Coverage:               mediafacts.ParseCoverageComplete,
			ProcessedThroughOffset: startOffset + int64(len(c)),
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 0, Joinable: true},
			},
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
				Records: []mediafacts.TimingRecord{
					{
						Type: mediafacts.TimingRecordTypeDiscontinuity,
						Discontinuity: mediafacts.DiscontinuityRecord{
							Scope:         mediafacts.DiscontinuityScopeProgram,
							ObservedAt:    0,
							HasEpochAfter: true,
							EpochAfter:    1,
						},
					},
					rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 10000, SubjectAt: 0, ObservedAt: 0}),
				},
			},
			Facts: mediafacts.Facts{HasPAT: true, HasPMT: true, PMTPID: 4096, VideoPID: 256},
			PSI: mediafacts.ActivePSI{
				PATSections: [][]byte{{0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE1, 0x00, 0xE8, 0xF9, 0x5E, 0x7D}},
				PMTSections: [][]byte{{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE1, 0x00, 0xF0, 0x00, 0x1B, 0xE1, 0x00, 0xF0, 0x00, 0xAA, 0xBB, 0xCC, 0xDD}},
			},
		}
	}

	_, err := r.Push(ctx, makeTSPacket(true, false, 42))
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	attach, reader, err := r.NewPrimedSubscriberAtTime(1, 10000, timeline.SeekModePreceding)
	if err != nil {
		t.Fatalf("NewPrimedSubscriberAtTime failed: %v", err)
	}
	defer func() { _ = reader.Close() }()

	// Preamble must be present in attach point for caller to deliver
	if len(attach.Preamble) == 0 {
		t.Fatalf("attach.Preamble is empty")
	}

	// Reader itself must NOT deliver the preamble in Read(); it must directly yield the ring packet at offset 0
	buf := make([]byte, TSPacketSize)
	n, err := reader.Read(buf)
	if err != nil || n != TSPacketSize {
		t.Fatalf("reader.Read failed: n=%d, err=%v", n, err)
	}
	if buf[2] != 42 {
		t.Fatalf("reader.Read yielded unexpected packet tag %d, want 42 (preamble was erroneously double-queued into reader)", buf[2])
	}
}
