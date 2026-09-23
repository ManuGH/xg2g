// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package timeline

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// rawIngestResult is a canonical result carrying exactly the records and events given.
func rawIngestResult(records []mediafacts.TimingRecord, events []mediafacts.Event) mediafacts.ParseResult {
	return mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Events:   events,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityCanonical,
			Records:   records,
		},
	}
}

// canonicalIngestResult is rawIngestResult plus the RAP timing records media-core
// publishes when a RAP is established in the packet that carries its PES header
// (an IDR or IRAP slice): one per RandomAccessPoint event whose PES timing point
// is in the same result. It is fixture shorthand for the common case; the tests
// that are about binding itself - a RAP established chunks after its header, a
// record without its event, two records for one RAP - build their results with
// rawIngestResult and state every record.
func canonicalIngestResult(records []mediafacts.TimingRecord, events []mediafacts.Event) mediafacts.ParseResult {
	var bindings []mediafacts.TimingRecord
	for _, ev := range events {
		if ev.Kind != mediafacts.EventRandomAccessPoint {
			continue
		}
		for _, rec := range records {
			if rec.Type == mediafacts.TimingRecordTypePES && rec.PES.SubjectAt == ev.Offset {
				bindings = append(bindings, mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypeRandomAccessPoint, RAP: rec.PES})
			}
		}
	}
	return rawIngestResult(append(append([]mediafacts.TimingRecord(nil), records...), bindings...), events)
}

func rapRecord(pt mediafacts.TimingPoint) mediafacts.TimingRecord {
	return mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypeRandomAccessPoint, RAP: pt}
}

func pesRecord(pt mediafacts.TimingPoint) mediafacts.TimingRecord {
	return mediafacts.TimingRecord{Type: mediafacts.TimingRecordTypePES, PES: pt}
}

// A RAP that is only established when its access unit ends - an all-intra H.264
// picture, an HEVC recovery point - arrives chunks after its PES header. It is
// bound by the RAP timing record published with it, not by a PES record the
// index saw earlier.
func TestRAPEstablishedInLaterChunkIsBoundByItsRAPRecord(t *testing.T) {
	idx := NewMediaIndex()
	start := mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true, EpochAfter: 3}
	header := mediafacts.TimingPoint{Epoch: 3, PID: 256, HasPTS: true, PTS90k: 900000, HasDTS: true, DTS90k: 896400, ObservedAt: 1000, SubjectAt: 1000}

	// Chunk 1: the PES header, no RAP yet.
	if err := idx.ApplyIngestResult(rawIngestResult([]mediafacts.TimingRecord{
		{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: start},
		pesRecord(header),
	}, nil)); err != nil {
		t.Fatalf("chunk 1: %v", err)
	}
	if _, ok := idx.FindPrecedingRAP(1000); ok {
		t.Fatalf("no RAP may exist before one was established")
	}

	// Chunk 2: the next PES starts at 90000, which ends the access unit and
	// establishes the RAP at 1000.
	established := header
	established.ObservedAt = 90000
	next := mediafacts.TimingPoint{Epoch: 3, PID: 256, HasPTS: true, PTS90k: 903600, ObservedAt: 90000, SubjectAt: 90000}
	if err := idx.ApplyIngestResult(rawIngestResult(
		[]mediafacts.TimingRecord{pesRecord(next), rapRecord(established)},
		[]mediafacts.Event{{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true}},
	)); err != nil {
		t.Fatalf("chunk 2: %v", err)
	}

	rap, ok := idx.FindRAPPrecedingPTS(3, 900000)
	if !ok || rap.Offset != 1000 {
		t.Fatalf("FindRAPPrecedingPTS(3, 900000) = (%+v, %v), want the RAP at 1000", rap, ok)
	}
	if !rap.HasTimingBinding || rap.Epoch != 3 || rap.PID != 256 || !rap.HasPTS || rap.PTS90k != 900000 || !rap.HasDTS || rap.DTS90k != 896400 {
		t.Errorf("RAP bound to the wrong timing: %+v", rap)
	}
}

// The index never binds on its own. A RAP event whose result carries a PES
// record at the same offset but no RAP record is unbound: joining the two is
// media-core's decision, and it did not make it.
func TestRAPWithoutRAPRecordIsUnboundEvenBesideAMatchingPESRecord(t *testing.T) {
	idx := NewMediaIndex()
	header := mediafacts.TimingPoint{Epoch: 0, PID: 256, HasPTS: true, PTS90k: 90000, ObservedAt: 1000, SubjectAt: 1000}
	if err := idx.ApplyIngestResult(rawIngestResult(
		[]mediafacts.TimingRecord{
			{Type: mediafacts.TimingRecordTypeDiscontinuity, Discontinuity: mediafacts.DiscontinuityRecord{Scope: mediafacts.DiscontinuityScopeProgram, ObservedAt: 0, HasEpochAfter: true}},
			pesRecord(header),
		},
		[]mediafacts.Event{{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true}},
	)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rap, ok := idx.FindPrecedingRAP(1000)
	if !ok || rap.HasTimingBinding {
		t.Fatalf("RAP must exist and be unbound, got (%+v, %v)", rap, ok)
	}
	if _, ok := idx.FindRAPPrecedingPTS(0, 90000); ok {
		t.Errorf("an unbound RAP must not be found by PTS")
	}
}

func TestUnmatchedRAPTimingRecordFailsClosed(t *testing.T) {
	idx := NewMediaIndex()
	stray := mediafacts.TimingPoint{Epoch: 0, PID: 256, HasPTS: true, PTS90k: 90000, ObservedAt: 5000, SubjectAt: 1000}
	err := idx.ApplyIngestResult(rawIngestResult(
		[]mediafacts.TimingRecord{pesRecord(mediafacts.TimingPoint{Epoch: 0, PID: 256, ObservedAt: 5000, SubjectAt: 5000}), rapRecord(stray)},
		[]mediafacts.Event{{Kind: mediafacts.EventRandomAccessPoint, Offset: 5000, Joinable: true}},
	))
	if !errors.Is(err, ErrUnmatchedTimingBinding) {
		t.Fatalf("expected ErrUnmatchedTimingBinding, got: %v", err)
	}
	if _, ok := idx.FindFollowingRAP(0); ok {
		t.Errorf("index state mutated after unmatched binding")
	}
	if len(idx.TimingPoints()) != 0 {
		t.Errorf("timing points added after unmatched binding")
	}
}

func TestRAPPrecedingAndFollowing(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    180000,
					SubjectAt: 3000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    270000,
					SubjectAt: 5000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 3000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 5000, Joinable: false},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Preceding queries
	if _, ok := idx.FindPrecedingRAP(500); ok {
		t.Errorf("FindPrecedingRAP(500) should be false")
	}
	if rap, ok := idx.FindPrecedingRAP(1000); !ok || rap.Offset != 1000 {
		t.Errorf("FindPrecedingRAP(1000) = (%v, %v), want offset 1000", rap, ok)
	}
	if rap, ok := idx.FindPrecedingRAP(2500); !ok || rap.Offset != 1000 {
		t.Errorf("FindPrecedingRAP(2500) = (%v, %v), want offset 1000", rap, ok)
	}
	if rap, ok := idx.FindPrecedingRAP(3000); !ok || rap.Offset != 3000 {
		t.Errorf("FindPrecedingRAP(3000) = (%v, %v), want offset 3000", rap, ok)
	}
	if rap, ok := idx.FindPrecedingRAP(6000); !ok || rap.Offset != 5000 {
		t.Errorf("FindPrecedingRAP(6000) = (%v, %v), want offset 5000", rap, ok)
	}

	// Following queries
	if rap, ok := idx.FindFollowingRAP(500); !ok || rap.Offset != 1000 {
		t.Errorf("FindFollowingRAP(500) = (%v, %v), want offset 1000", rap, ok)
	}
	if rap, ok := idx.FindFollowingRAP(1000); !ok || rap.Offset != 1000 {
		t.Errorf("FindFollowingRAP(1000) = (%v, %v), want offset 1000", rap, ok)
	}
	if rap, ok := idx.FindFollowingRAP(2500); !ok || rap.Offset != 3000 {
		t.Errorf("FindFollowingRAP(2500) = (%v, %v), want offset 3000", rap, ok)
	}
	if rap, ok := idx.FindFollowingRAP(5000); !ok || rap.Offset != 5000 {
		t.Errorf("FindFollowingRAP(5000) = (%v, %v), want offset 5000", rap, ok)
	}
	if _, ok := idx.FindFollowingRAP(5001); ok {
		t.Errorf("FindFollowingRAP(5001) should be false")
	}

	// Range query RAPsBetween
	raps := idx.RAPsBetween(1000, 3000)
	if len(raps) != 2 || raps[0].Offset != 1000 || raps[1].Offset != 3000 {
		t.Errorf("RAPsBetween(1000, 3000) unexpected: %+v", raps)
	}
	if len(idx.RAPsBetween(3001, 3500)) != 0 {
		t.Errorf("RAPsBetween(3001, 3500) should be empty")
	}
}

func TestRAPInvalidation(t *testing.T) {
	idx := NewMediaIndex()

	res1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
		},
	)
	if err := idx.ApplyIngestResult(res1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rap, ok := idx.FindPrecedingRAP(1000)
	if !ok || rap.Offset != 1000 {
		t.Fatalf("expected RAP at 1000, got (%v, %v)", rap, ok)
	}
	if _, ok := idx.FindRAPPrecedingPTS(1, 90000); !ok {
		t.Fatalf("expected RAP in PTS index before invalidation")
	}

	// Invalidate RAP at 1000
	res2 := canonicalIngestResult(
		nil,
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPointInvalidated, Offset: 1000},
		},
	)
	if err := idx.ApplyIngestResult(res2); err != nil {
		t.Fatalf("unexpected error on invalidation: %v", err)
	}

	if _, ok := idx.FindPrecedingRAP(1000); ok {
		t.Errorf("FindPrecedingRAP(1000) should be false after invalidation")
	}
	if _, ok := idx.FindFollowingRAP(1000); ok {
		t.Errorf("FindFollowingRAP(1000) should be false after invalidation")
	}
	if _, ok := idx.FindRAPPrecedingPTS(1, 90000); ok {
		t.Errorf("FindRAPPrecedingPTS(1, 90000) should be false after invalidation")
	}
	if _, ok := idx.FindRAPNearestPTS(1, 90000); ok {
		t.Errorf("FindRAPNearestPTS(1, 90000) should be false after invalidation")
	}
}

func TestEpochScopedPTSLookup(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    93600,
					SubjectAt: 2000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    97200,
					SubjectAt: 3000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 3000, Joinable: true},
		},
	)
	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Preceding PTS in Epoch 1
	if _, ok := idx.FindRAPPrecedingPTS(1, 89999); ok {
		t.Errorf("FindRAPPrecedingPTS(1, 89999) should be false")
	}
	if rap, ok := idx.FindRAPPrecedingPTS(1, 90000); !ok || rap.PTS90k != 90000 {
		t.Errorf("FindRAPPrecedingPTS(1, 90000) = (%v, %v), want 90000", rap, ok)
	}
	if rap, ok := idx.FindRAPPrecedingPTS(1, 95000); !ok || rap.PTS90k != 93600 {
		t.Errorf("FindRAPPrecedingPTS(1, 95000) = (%v, %v), want 93600", rap, ok)
	}
	if rap, ok := idx.FindRAPPrecedingPTS(1, 100000); !ok || rap.PTS90k != 97200 {
		t.Errorf("FindRAPPrecedingPTS(1, 100000) = (%v, %v), want 97200", rap, ok)
	}

	// Nearest PTS in Epoch 1
	if rap, ok := idx.FindRAPNearestPTS(1, 80000); !ok || rap.PTS90k != 90000 {
		t.Errorf("FindRAPNearestPTS(1, 80000) = (%v, %v), want 90000", rap, ok)
	}
	// 95000 is closer to 93600 (diff 1400) than 97200 (diff 2200)
	if rap, ok := idx.FindRAPNearestPTS(1, 95000); !ok || rap.PTS90k != 93600 {
		t.Errorf("FindRAPNearestPTS(1, 95000) = (%v, %v), want 93600", rap, ok)
	}
	// 96000 is closer to 97200 (diff 1200) than 93600 (diff 2400)
	if rap, ok := idx.FindRAPNearestPTS(1, 96000); !ok || rap.PTS90k != 97200 {
		t.Errorf("FindRAPNearestPTS(1, 96000) = (%v, %v), want 97200", rap, ok)
	}
	// Equidistant tie-breaker (91800 is 1800 from 90000 and 1800 from 93600) -> returns preceding (90000)
	if rap, ok := idx.FindRAPNearestPTS(1, 91800); !ok || rap.PTS90k != 90000 {
		t.Errorf("FindRAPNearestPTS(1, 91800) = (%v, %v), want 90000 (preceding on tie)", rap, ok)
	}

	// Querying non-existent Epoch 2
	if _, ok := idx.FindRAPPrecedingPTS(2, 90000); ok {
		t.Errorf("FindRAPPrecedingPTS(2, 90000) should be false")
	}
	if _, ok := idx.FindRAPNearestPTS(2, 90000); ok {
		t.Errorf("FindRAPNearestPTS(2, 90000) should be false")
	}
}

func TestSamePTSInTwoEpochs(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          mediafacts.DiscontinuityScopeProgram,
					ObservedAt:     5000,
					HasEpochBefore: true,
					EpochBefore:    1,
					HasEpochAfter:  true,
					EpochAfter:     2,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     2,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000, // Identical PTS in Epoch 2
					SubjectAt: 5000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 5000, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Query Epoch 1
	rap1Pre, ok := idx.FindRAPPrecedingPTS(1, 90000)
	if !ok || rap1Pre.Offset != 1000 || rap1Pre.Epoch != 1 {
		t.Errorf("FindRAPPrecedingPTS(1, 90000) got (%v, %v), want offset 1000 in epoch 1", rap1Pre, ok)
	}
	rap1Near, ok := idx.FindRAPNearestPTS(1, 90000)
	if !ok || rap1Near.Offset != 1000 || rap1Near.Epoch != 1 {
		t.Errorf("FindRAPNearestPTS(1, 90000) got (%v, %v), want offset 1000 in epoch 1", rap1Near, ok)
	}

	// Query Epoch 2
	rap2Pre, ok := idx.FindRAPPrecedingPTS(2, 90000)
	if !ok || rap2Pre.Offset != 5000 || rap2Pre.Epoch != 2 {
		t.Errorf("FindRAPPrecedingPTS(2, 90000) got (%v, %v), want offset 5000 in epoch 2", rap2Pre, ok)
	}
	rap2Near, ok := idx.FindRAPNearestPTS(2, 90000)
	if !ok || rap2Near.Offset != 5000 || rap2Near.Epoch != 2 {
		t.Errorf("FindRAPNearestPTS(2, 90000) got (%v, %v), want offset 5000 in epoch 2", rap2Near, ok)
	}
}

func TestPTSIndexDoesNotAssumeOffsetMonotonicity(t *testing.T) {
	// Specific required test:
	// offset 100 -> PTS 93600
	// offset 200 -> PTS 86400
	// FindRAPPrecedingPTS(epoch, 90000) -> RAP with PTS 86400 (at offset 200)
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    93600,
					SubjectAt: 100,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    86400,
					SubjectAt: 200,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Offset index must be in offset order (100 then 200)
	rapPreOffset, ok := idx.FindPrecedingRAP(150)
	if !ok || rapPreOffset.Offset != 100 {
		t.Errorf("FindPrecedingRAP(150) = (%v, %v), want offset 100", rapPreOffset, ok)
	}

	// PTS query must find PTS 86400 even though it arrived at offset 200
	rapPrePTS, ok := idx.FindRAPPrecedingPTS(1, 90000)
	if !ok {
		t.Fatalf("FindRAPPrecedingPTS(1, 90000) failed to find RAP")
	}
	if rapPrePTS.PTS90k != 86400 || rapPrePTS.Offset != 200 {
		t.Errorf("FindRAPPrecedingPTS(1, 90000) got PTS=%d Offset=%d, want PTS 86400 at Offset 200",
			rapPrePTS.PTS90k, rapPrePTS.Offset)
	}
}

func TestPCREntries(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          1,
					PCRPID:         256,
					ObservedAt:     1000,
					ExtendedPCR27m: 27000000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          1,
					PCRPID:         256,
					ObservedAt:     2000,
					ExtendedPCR27m: 54000000,
				},
			},
		},
		nil,
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pcrs := idx.PCREntries()
	if len(pcrs) != 2 {
		t.Fatalf("expected 2 PCR entries, got %d", len(pcrs))
	}
	if pcrs[0].ExtendedPCR27m != 27000000 || pcrs[0].ObservedAt != 1000 {
		t.Errorf("unexpected PCR[0]: %+v", pcrs[0])
	}
	if pcrs[1].ExtendedPCR27m != 54000000 || pcrs[1].ObservedAt != 2000 {
		t.Errorf("unexpected PCR[1]: %+v", pcrs[1])
	}
}

func TestDiscontinuities(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					Reason:        mediafacts.DiscontinuityReasonProgramIdentityChanged,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:      mediafacts.DiscontinuityScopeTrack,
					TrackPID:   256,
					Reason:     mediafacts.DiscontinuityReasonTransportTimingLoss,
					ObservedAt: 2000,
				},
			},
		},
		nil,
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	all := idx.DiscontinuitiesBetween(0, 3000)
	if len(all) != 2 {
		t.Fatalf("expected 2 discontinuities, got %d", len(all))
	}

	prog := idx.DiscontinuitiesBetween(500, 1500)
	if len(prog) != 1 || prog[0].Scope != mediafacts.DiscontinuityScopeProgram {
		t.Errorf("DiscontinuitiesBetween(500, 1500) unexpected: %+v", prog)
	}

	track := idx.DiscontinuitiesBetween(1500, 2500)
	if len(track) != 1 || track[0].Scope != mediafacts.DiscontinuityScopeTrack {
		t.Errorf("DiscontinuitiesBetween(1500, 2500) unexpected: %+v", track)
	}
}

func TestProgramDiscontinuityBuildsHalfOpenEpochSpans(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          mediafacts.DiscontinuityScopeProgram,
					ObservedAt:     5000,
					HasEpochBefore: true,
					EpochBefore:    1,
					HasEpochAfter:  true,
					EpochAfter:     2,
				},
			},
		},
		nil,
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := idx.EpochSpans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}

	// Span 1: [1000, 5000), closed
	if spans[0].Epoch != 1 || spans[0].StartOffset != 1000 || spans[0].EndOffset != 5000 || !spans[0].Closed {
		t.Errorf("unexpected Span 1: %+v", spans[0])
	}

	// Span 2: [5000, ∞), open
	if spans[1].Epoch != 2 || spans[1].StartOffset != 5000 || spans[1].Closed {
		t.Errorf("unexpected Span 2: %+v", spans[1])
	}

	// EpochForOffset
	if _, ok := idx.EpochForOffset(500); ok {
		t.Errorf("EpochForOffset(500) should be false")
	}
	if span, ok := idx.EpochForOffset(1000); !ok || span.Epoch != 1 {
		t.Errorf("EpochForOffset(1000) = (%v, %v), want Epoch 1", span, ok)
	}
	if span, ok := idx.EpochForOffset(4999); !ok || span.Epoch != 1 {
		t.Errorf("EpochForOffset(4999) = (%v, %v), want Epoch 1", span, ok)
	}
	if span, ok := idx.EpochForOffset(5000); !ok || span.Epoch != 2 {
		t.Errorf("EpochForOffset(5000) = (%v, %v), want Epoch 2", span, ok)
	}
	if span, ok := idx.EpochForOffset(100000); !ok || span.Epoch != 2 {
		t.Errorf("EpochForOffset(100000) = (%v, %v), want Epoch 2", span, ok)
	}
}

func TestTrackDiscontinuityDoesNotSplitEpoch(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:      mediafacts.DiscontinuityScopeTrack,
					TrackPID:   256,
					ObservedAt: 2000,
				},
			},
		},
		nil,
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spans := idx.EpochSpans()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 span, got %d", len(spans))
	}
	if spans[0].Epoch != 1 || spans[0].Closed {
		t.Errorf("track discontinuity altered epoch span: %+v", spans[0])
	}
}

func TestActiveEpochDoesNotInventBoundary(t *testing.T) {
	idx := NewMediaIndex()

	res := mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority:      mediafacts.TimingAuthorityCanonical,
			HasActiveEpoch: true,
			ActiveEpoch:    42, // Active epoch reported without byte-positioned discontinuity
			Records:        nil,
		},
	}

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(idx.EpochSpans()) != 0 {
		t.Errorf("ActiveEpoch without discontinuity invented epoch spans: %+v", idx.EpochSpans())
	}
	if _, ok := idx.EpochForOffset(1000); ok {
		t.Errorf("EpochForOffset should be false without byte-positioned boundary")
	}
}

func TestAmbiguousRAPTimingBindingFailsClosed(t *testing.T) {
	idx := NewMediaIndex()

	res := rawIngestResult(
		[]mediafacts.TimingRecord{
			rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 256, HasPTS: true, PTS90k: 90000, SubjectAt: 1000}),
			// Conflict: a second binding for the same RAP.
			rapRecord(mediafacts.TimingPoint{Epoch: 1, PID: 257, HasPTS: true, PTS90k: 90040, SubjectAt: 1000}),
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
		},
	)

	err := idx.ApplyIngestResult(res)
	if !errors.Is(err, ErrAmbiguousTimingBinding) {
		t.Fatalf("expected ErrAmbiguousTimingBinding, got: %v", err)
	}

	// Zero mutation check
	if _, ok := idx.FindPrecedingRAP(1000); ok {
		t.Errorf("index state mutated after ambiguous error")
	}
	if len(idx.TimingPoints()) != 0 {
		t.Errorf("timing points added after ambiguous error")
	}
}

func TestNonCanonicalCommitReturnsErrorWithoutMutation(t *testing.T) {
	idx := NewMediaIndex()

	// Initial valid state
	validRes := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
		},
	)
	if err := idx.ApplyIngestResult(validRes); err != nil {
		t.Fatalf("unexpected error on initial ingest: %v", err)
	}

	// Ingest with TimingAuthorityNone
	nonCanonicalRes := mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityNone,
		},
		Events: []mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	}

	err := idx.ApplyIngestResult(nonCanonicalRes)
	if !errors.Is(err, ErrNonCanonicalTiming) {
		t.Fatalf("expected ErrNonCanonicalTiming for TimingAuthorityNone, got: %v", err)
	}

	// Verify offset 2000 was not added
	if _, ok := idx.FindFollowingRAP(2000); ok {
		t.Errorf("non-canonical commit mutated the index")
	}

	// Ingest with TimingAuthorityUnknown
	unknownRes := mediafacts.ParseResult{
		Coverage: mediafacts.ParseCoverageComplete,
		Timing: mediafacts.TimingResult{
			Authority: mediafacts.TimingAuthorityUnknown,
		},
	}
	errUnknown := idx.ApplyIngestResult(unknownRes)
	if !errors.Is(errUnknown, ErrNonCanonicalTiming) {
		t.Fatalf("expected ErrNonCanonicalTiming for TimingAuthorityUnknown, got: %v", errUnknown)
	}
}

func TestNegativeExtendedPTS(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    -90000,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    -45000,
					SubjectAt: 2000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rapPre, ok := idx.FindRAPPrecedingPTS(1, -50000)
	if !ok || rapPre.PTS90k != -90000 {
		t.Errorf("FindRAPPrecedingPTS(1, -50000) = (%v, %v), want -90000", rapPre, ok)
	}

	rapNear, ok := idx.FindRAPNearestPTS(1, -40000)
	if !ok || rapNear.PTS90k != -45000 {
		t.Errorf("FindRAPNearestPTS(1, -40000) = (%v, %v), want -45000", rapNear, ok)
	}
}

func TestLargeExtendedTimestamps(t *testing.T) {
	idx := NewMediaIndex()

	hugePTS1 := int64(math.MaxInt64 - 200000)
	hugePTS2 := int64(math.MaxInt64 - 100000)

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    hugePTS1,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    hugePTS2,
					SubjectAt: 2000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rapNear, ok := idx.FindRAPNearestPTS(1, hugePTS2-10)
	if !ok || rapNear.PTS90k != hugePTS2 {
		t.Errorf("FindRAPNearestPTS huge timestamps failed: got (%v, %v)", rapNear, ok)
	}

	// Extreme difference between negative min and positive max does not overflow or panic
	rapExtreme, ok := idx.FindRAPNearestPTS(1, math.MinInt64)
	if !ok || rapExtreme.PTS90k != hugePTS1 {
		t.Errorf("FindRAPNearestPTS extreme negative pts failed: got (%v, %v)", rapExtreme, ok)
	}
}

func TestDeterministicPruning(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          1,
					PCRPID:         256,
					ObservedAt:     1000,
					ExtendedPCR27m: 100,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          1,
					PCRPID:         256,
					ObservedAt:     2000,
					ExtendedPCR27m: 200,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    180000,
					SubjectAt: 2000,
				},
			},
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          mediafacts.DiscontinuityScopeProgram,
					ObservedAt:     5000,
					HasEpochBefore: true,
					EpochBefore:    1,
					HasEpochAfter:  true,
					EpochAfter:     2,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          2,
					PCRPID:         256,
					ObservedAt:     6000,
					ExtendedPCR27m: 600,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     2,
					PID:       256,
					HasPTS:    true,
					PTS90k:    270000,
					SubjectAt: 6000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 6000, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prune before 2000:
	// - RAP 1000 removed, RAP 2000 and 6000 kept
	// - PCR 1000 removed, PCR 2000 and 6000 kept
	// - Epoch 1 [1000, 5000) must be KEPT because EndOffset (5000) > tailOffset (2000)
	idx.PruneBefore(2000)

	if _, ok := idx.FindPrecedingRAP(1000); ok {
		t.Errorf("RAP at 1000 should be pruned")
	}
	if rap, ok := idx.FindPrecedingRAP(2000); !ok || rap.Offset != 2000 {
		t.Errorf("RAP at 2000 should be kept, got (%v, %v)", rap, ok)
	}
	if len(idx.PCREntries()) != 2 {
		t.Errorf("expected 2 PCR entries, got %d", len(idx.PCREntries()))
	}

	// Verify Epoch 1 is still resolvable for remaining entries
	span1, ok := idx.EpochForOffset(2000)
	if !ok || span1.Epoch != 1 {
		t.Errorf("EpochForOffset(2000) should resolve to Epoch 1 even after PruneBefore(2000): got (%v, %v)", span1, ok)
	}

	// Prune before 6000:
	// - Epoch 1 [1000, 5000) now pruned because EndOffset <= 6000
	// - Epoch 2 [5000, ∞) kept because it is open
	idx.PruneBefore(6000)

	if _, ok := idx.EpochForOffset(2000); ok {
		t.Errorf("EpochForOffset(2000) should be false after PruneBefore(6000)")
	}
	span2, ok := idx.EpochForOffset(6000)
	if !ok || span2.Epoch != 2 {
		t.Errorf("EpochForOffset(6000) should resolve to Epoch 2: got (%v, %v)", span2, ok)
	}
	if len(idx.PCREntries()) != 1 {
		t.Errorf("expected 1 PCR entry after PruneBefore(6000), got %d", len(idx.PCREntries()))
	}
}

func TestConcurrentReaders(t *testing.T) {
	idx := NewMediaIndex()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		var offset int64 = 1000
		var pts int64 = 90000
		var epoch mediafacts.TimelineEpoch = 1

		for {
			select {
			case <-stop:
				return
			default:
				res := canonicalIngestResult(
					[]mediafacts.TimingRecord{
						{
							Type: mediafacts.TimingRecordTypePES,
							PES: mediafacts.TimingPoint{
								Epoch:     epoch,
								PID:       256,
								HasPTS:    true,
								PTS90k:    pts,
								SubjectAt: offset,
							},
						},
						{
							Type: mediafacts.TimingRecordTypePCR,
							PCR: mediafacts.PCRPoint{
								Epoch:          epoch,
								PCRPID:         256,
								ObservedAt:     offset,
								ExtendedPCR27m: pts * 300,
							},
						},
					},
					[]mediafacts.Event{
						{Kind: mediafacts.EventRandomAccessPoint, Offset: offset, Joinable: true},
					},
				)
				_ = idx.ApplyIngestResult(res)

				if offset%5000 == 0 && offset > 20000 {
					idx.PruneBefore(offset - 10000)
				}

				offset += 1880
				pts += 3600
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()

	// 5 Concurrent reader goroutines
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = idx.FindPrecedingRAP(10000)
					_, _ = idx.FindFollowingRAP(5000)
					_, _ = idx.FindRAPPrecedingPTS(1, 100000)
					_, _ = idx.FindRAPNearestPTS(1, 100000)
					_ = idx.RAPsBetween(5000, 15000)
					_ = idx.PCREntries()
					_ = idx.TimingPoints()
					_ = idx.EpochSpans()
				}
			}
		}(r)
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestUnboundRAPIsNotMistakenForEpochZero(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    500,
					HasEpochAfter: true,
					EpochAfter:    0, // Real Epoch 0
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        256,
					HasPTS:     false,
					HasDTS:     false,
					ObservedAt: 2000,
					SubjectAt:  2000,
				},
			},
		},
		[]mediafacts.Event{
			// RAP at 1000: Unbound (no PES record matching SubjectAt 1000)
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			// RAP at 2000: Bound to Epoch 0, but no PTS/DTS
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rapUnbound, ok := idx.FindPrecedingRAP(1000)
	if !ok || rapUnbound.Offset != 1000 {
		t.Fatalf("failed to find unbound RAP at 1000: (%v, %v)", rapUnbound, ok)
	}
	if rapUnbound.HasTimingBinding {
		t.Errorf("unbound RAP must have HasTimingBinding == false, got true")
	}

	rapBound, ok := idx.FindPrecedingRAP(2000)
	if !ok || rapBound.Offset != 2000 {
		t.Fatalf("failed to find bound RAP at 2000: (%v, %v)", rapBound, ok)
	}
	if !rapBound.HasTimingBinding {
		t.Errorf("bound RAP at 2000 must have HasTimingBinding == true")
	}
	if rapBound.Epoch != 0 || rapBound.PID != 256 {
		t.Errorf("bound RAP at 2000 unexpected: Epoch=%d PID=%d", rapBound.Epoch, rapBound.PID)
	}
	if rapBound.HasPTS || rapBound.HasDTS {
		t.Errorf("bound RAP at 2000 without timestamps should have HasPTS=false and HasDTS=false")
	}

	// Prove they are distinct
	if rapUnbound.HasTimingBinding == rapBound.HasTimingBinding {
		t.Errorf("unbound and bound RAPs must not have equal HasTimingBinding")
	}
}

func TestBoundEpochZeroWithoutPTSIsDistinguishable(t *testing.T) {
	idx := NewMediaIndex()

	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    100,
					HasEpochAfter: true,
					EpochAfter:    0,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        300,
					HasPTS:     false,
					ObservedAt: 500,
					SubjectAt:  500,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 500, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rapA, _ := idx.FindPrecedingRAP(200)
	rapB, _ := idx.FindPrecedingRAP(500)

	// RAP A has no binding
	if rapA.HasTimingBinding {
		t.Errorf("RAP A unexpectedly has timing binding")
	}

	// RAP B has explicit binding to Epoch 0
	if !rapB.HasTimingBinding {
		t.Errorf("RAP B missing timing binding")
	}
	if rapB.Epoch != 0 || rapB.PID != 300 {
		t.Errorf("RAP B incorrect Epoch/PID: %d / %d", rapB.Epoch, rapB.PID)
	}
	if rapB.HasPTS {
		t.Errorf("RAP B should not have PTS")
	}
}

func TestInconsistentEpochTransitionFailsWithoutMutation(t *testing.T) {
	idx := NewMediaIndex()

	// 1. Initially activate Epoch 1
	initRes := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    1000,
					HasEpochAfter: true,
					EpochAfter:    1,
				},
			},
		},
		nil,
	)
	if err := idx.ApplyIngestResult(initRes); err != nil {
		t.Fatalf("failed initial ingest: %v", err)
	}

	spansBefore := idx.EpochSpans()
	if len(spansBefore) != 1 || spansBefore[0].Epoch != 1 {
		t.Fatalf("expected active Epoch 1 span, got: %+v", spansBefore)
	}

	// 2. Inconsistent transition: active is Epoch 1, but discontinuity says before=2, after=3
	badRes1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          mediafacts.DiscontinuityScopeProgram,
					ObservedAt:     2000,
					HasEpochBefore: true,
					EpochBefore:    2, // Mismatch!
					HasEpochAfter:  true,
					EpochAfter:     3,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          3,
					ObservedAt:     2000,
					ExtendedPCR27m: 12345,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	)

	err1 := idx.ApplyIngestResult(badRes1)
	if !errors.Is(err1, ErrInconsistentEpochTransition) {
		t.Fatalf("expected ErrInconsistentEpochTransition, got: %v", err1)
	}

	// Verify ZERO MUTATION: Epoch 1 is still open, no new span, no PCR, no RAP
	spansAfter1 := idx.EpochSpans()
	if len(spansAfter1) != 1 || spansAfter1[0].Epoch != 1 || spansAfter1[0].Closed {
		t.Errorf("spans mutated after badRes1: %+v", spansAfter1)
	}
	if len(idx.PCREntries()) != 0 {
		t.Errorf("PCR added despite inconsistent epoch error")
	}
	if _, ok := idx.FindPrecedingRAP(2000); ok {
		t.Errorf("RAP added despite inconsistent epoch error")
	}

	// 3. Inconsistent transition: active is Epoch 1, but discontinuity says before=None, after=2
	badRes2 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:         mediafacts.DiscontinuityScopeProgram,
					ObservedAt:    3000,
					HasEpochAfter: true,
					EpochAfter:    2, // Cannot start without closing active Epoch 1
				},
			},
		},
		nil,
	)

	err2 := idx.ApplyIngestResult(badRes2)
	if !errors.Is(err2, ErrInconsistentEpochTransition) {
		t.Fatalf("expected ErrInconsistentEpochTransition for badRes2, got: %v", err2)
	}

	// 4. Inconsistent transition: active is Epoch 1, but closing says before=2, after=None
	badRes3 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:          mediafacts.DiscontinuityScopeProgram,
					ObservedAt:     4000,
					HasEpochBefore: true,
					EpochBefore:    2, // Mismatch with active Epoch 1
				},
			},
		},
		nil,
	)

	err3 := idx.ApplyIngestResult(badRes3)
	if !errors.Is(err3, ErrInconsistentEpochTransition) {
		t.Fatalf("expected ErrInconsistentEpochTransition for badRes3, got: %v", err3)
	}
}

func TestIncompleteCoverageFailsClosed(t *testing.T) {
	idx := NewMediaIndex()

	coverages := []mediafacts.ParseCoverage{
		mediafacts.ParseCoverageUnknown,
		mediafacts.ParseCoveragePSIOnly,
		mediafacts.ParseCoveragePSIVideo,
	}

	for _, cov := range coverages {
		res := mediafacts.ParseResult{
			Coverage: cov,
			Timing: mediafacts.TimingResult{
				Authority: mediafacts.TimingAuthorityCanonical,
			},
			Events: []mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
			},
		}

		err := idx.ApplyIngestResult(res)
		if !errors.Is(err, ErrIncompleteCoverage) {
			t.Errorf("coverage %v: expected ErrIncompleteCoverage, got: %v", cov, err)
		}

		// Ensure zero mutation
		if _, ok := idx.FindPrecedingRAP(1000); ok {
			t.Errorf("coverage %v: index mutated despite error", cov)
		}
	}
}

func TestDuplicatePTSDeterministicTieBreak(t *testing.T) {
	idx := NewMediaIndex()

	// Ingest two RAPs in Epoch 1 with identical PTS = 90000 at offsets 100 and 200,
	// and two RAPs with identical PTS = 93600 at offsets 300 and 400.
	res := canonicalIngestResult(
		[]mediafacts.TimingRecord{
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
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 100,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 200,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    93600,
					SubjectAt: 300,
				},
			},
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       256,
					HasPTS:    true,
					PTS90k:    93600,
					SubjectAt: 400,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 100, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 200, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 300, Joinable: true},
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 400, Joinable: true},
		},
	)

	if err := idx.ApplyIngestResult(res); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1. FindRAPPrecedingPTS for target 90000:
	// Both offset 100 and 200 have PTS 90000 <= 90000.
	// Tie break must deterministically select the greatest transport Offset (200).
	rapPre1, ok := idx.FindRAPPrecedingPTS(1, 90000)
	if !ok || rapPre1.PTS90k != 90000 || rapPre1.Offset != 200 {
		t.Errorf("FindRAPPrecedingPTS(1, 90000) = (%+v, %v), want PTS 90000 at Offset 200", rapPre1, ok)
	}

	// 2. FindRAPNearestPTS for target 90000:
	// Exact match. Must select greatest transport Offset (200).
	rapNear1, ok := idx.FindRAPNearestPTS(1, 90000)
	if !ok || rapNear1.PTS90k != 90000 || rapNear1.Offset != 200 {
		t.Errorf("FindRAPNearestPTS(1, 90000) = (%+v, %v), want PTS 90000 at Offset 200", rapNear1, ok)
	}

	// 3. FindRAPNearestPTS for target 91800:
	// Exactly equidistant between PTS 90000 (diff 1800) and PTS 93600 (diff 1800).
	// Tie-break rule 1: preceding PTS wins (90000).
	// Tie-break rule 2: greatest transport Offset for 90000 wins (200).
	rapNearTie, ok := idx.FindRAPNearestPTS(1, 91800)
	if !ok || rapNearTie.PTS90k != 90000 || rapNearTie.Offset != 200 {
		t.Errorf("FindRAPNearestPTS(1, 91800) = (%+v, %v), want PTS 90000 at Offset 200", rapNearTie, ok)
	}

	// 4. FindRAPNearestPTS for target 93600:
	// Exact match. Must select greatest transport Offset (400).
	rapNear2, ok := idx.FindRAPNearestPTS(1, 93600)
	if !ok || rapNear2.PTS90k != 93600 || rapNear2.Offset != 400 {
		t.Errorf("FindRAPNearestPTS(1, 93600) = (%+v, %v), want PTS 93600 at Offset 400", rapNear2, ok)
	}
}

func BenchmarkFindPrecedingRAP(b *testing.B) {
	idx := NewMediaIndex()
	for i := 0; i < 10000; i++ {
		offset := int64(i * 1000)
		_ = idx.ApplyIngestResult(canonicalIngestResult(
			[]mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypePES,
					PES: mediafacts.TimingPoint{
						Epoch:     1,
						PID:       256,
						HasPTS:    true,
						PTS90k:    int64(i * 3600),
						SubjectAt: offset,
					},
				},
			},
			[]mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: offset, Joinable: true},
			},
		))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.FindPrecedingRAP(int64((i % 10000) * 1000))
	}
}

func BenchmarkFindRAPByPTS(b *testing.B) {
	idx := NewMediaIndex()
	for i := 0; i < 10000; i++ {
		offset := int64(i * 1000)
		_ = idx.ApplyIngestResult(canonicalIngestResult(
			[]mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypePES,
					PES: mediafacts.TimingPoint{
						Epoch:     1,
						PID:       256,
						HasPTS:    true,
						PTS90k:    int64(i * 3600),
						SubjectAt: offset,
					},
				},
			},
			[]mediafacts.Event{
				{Kind: mediafacts.EventRandomAccessPoint, Offset: offset, Joinable: true},
			},
		))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.FindRAPPrecedingPTS(1, int64((i%10000)*3600))
	}
}

func BenchmarkEpochForOffset(b *testing.B) {
	idx := NewMediaIndex()
	for e := 1; e <= 50; e++ {
		start := int64((e - 1) * 100000)
		_ = idx.ApplyIngestResult(canonicalIngestResult(
			[]mediafacts.TimingRecord{
				{
					Type: mediafacts.TimingRecordTypeDiscontinuity,
					Discontinuity: mediafacts.DiscontinuityRecord{
						Scope:          mediafacts.DiscontinuityScopeProgram,
						ObservedAt:     start,
						HasEpochBefore: e > 1,
						EpochBefore:    mediafacts.TimelineEpoch(e - 1),
						HasEpochAfter:  true,
						EpochAfter:     mediafacts.TimelineEpoch(e),
					},
				},
			},
			nil,
		))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.EpochForOffset(int64((i % 50) * 100000))
	}
}

var _ TimelineReader = (*MediaIndex)(nil)

func TestTimelineReader_DirectInterfaceContract(t *testing.T) {
	idx := NewMediaIndex()
	var reader TimelineReader = idx

	activeEpoch, hasActive := reader.ActiveEpoch()
	if hasActive || activeEpoch != 0 {
		t.Fatalf("expected no active epoch initially, got %v (%d)", hasActive, activeEpoch)
	}

	stats := reader.Stats()
	if stats.TotalRAPs != 0 || stats.BoundRAPs != 0 || stats.BoundRAPRatio() != 1.0 {
		t.Fatalf("unexpected empty stats: %+v", stats)
	}
}

func TestMediaIndex_ErrNonMonotonicObservedAt(t *testing.T) {
	idx := NewMediaIndex()

	// Initial valid commit: PCR at offset 1000
	res1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          0,
					PCRPID:         256,
					ObservedAt:     1000,
					ExtendedPCR27m: 27_000_000,
				},
			},
		},
		nil,
	)
	if err := idx.ApplyIngestResult(res1); err != nil {
		t.Fatalf("initial ingest failed: %v", err)
	}

	// Regressive PCR at offset 500 (< 1000)
	res2 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePCR,
				PCR: mediafacts.PCRPoint{
					Epoch:          0,
					PCRPID:         256,
					ObservedAt:     500,
					ExtendedPCR27m: 27_000_000,
				},
			},
		},
		nil,
	)
	err := idx.ApplyIngestResult(res2)
	if !errors.Is(err, ErrNonMonotonicObservedAt) {
		t.Fatalf("expected ErrNonMonotonicObservedAt, got: %v", err)
	}
	if len(idx.PCREntries()) != 1 || idx.PCREntries()[0].ObservedAt != 1000 {
		t.Fatalf("index mutated on non-monotonic error: %+v", idx.PCREntries())
	}

	// Regressive PES timing point
	resPES1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        257,
					ObservedAt: 2000,
					SubjectAt:  2000,
				},
			},
		},
		nil,
	)
	if err := idx.ApplyIngestResult(resPES1); err != nil {
		t.Fatalf("pes1 ingest failed: %v", err)
	}
	resPES2 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypePES,
				PES: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        257,
					ObservedAt: 1500,
					SubjectAt:  1500,
				},
			},
		},
		nil,
	)
	err = idx.ApplyIngestResult(resPES2)
	if !errors.Is(err, ErrNonMonotonicObservedAt) {
		t.Fatalf("expected ErrNonMonotonicObservedAt on PES, got: %v", err)
	}

	// Regressive Discontinuity
	resDisc1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:      mediafacts.DiscontinuityScopeProgram,
					ObservedAt: 3000,
				},
			},
		},
		nil,
	)
	if err := idx.ApplyIngestResult(resDisc1); err != nil {
		t.Fatalf("disc1 ingest failed: %v", err)
	}
	resDisc2 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeDiscontinuity,
				Discontinuity: mediafacts.DiscontinuityRecord{
					Scope:      mediafacts.DiscontinuityScopeProgram,
					ObservedAt: 2500,
				},
			},
		},
		nil,
	)
	err = idx.ApplyIngestResult(resDisc2)
	if !errors.Is(err, ErrNonMonotonicObservedAt) {
		t.Fatalf("expected ErrNonMonotonicObservedAt on Discontinuity, got: %v", err)
	}

	// Regressive RAP event offset
	resRAP1 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        257,
					SubjectAt:  4000,
					ObservedAt: 4000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 4000, Joinable: true},
		},
	)
	if err := idx.ApplyIngestResult(resRAP1); err != nil {
		t.Fatalf("rap1 ingest failed: %v", err)
	}
	resRAP2 := canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:      0,
					PID:        257,
					SubjectAt:  3500,
					ObservedAt: 3500,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 3500, Joinable: true},
		},
	)
	err = idx.ApplyIngestResult(resRAP2)
	if !errors.Is(err, ErrNonMonotonicObservedAt) {
		t.Fatalf("expected ErrNonMonotonicObservedAt on RAP, got: %v", err)
	}
}

func TestMediaIndex_EmptyEpochKeyPruning(t *testing.T) {
	idx := NewMediaIndex()

	// Ingest RAP in epoch 0 at offset 1000
	_ = idx.ApplyIngestResult(canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:     0,
					PID:       257,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
		},
	))

	// Ingest RAP in epoch 1 at offset 2000
	_ = idx.ApplyIngestResult(canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:     1,
					PID:       257,
					HasPTS:    true,
					PTS90k:    180000,
					SubjectAt: 2000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	))

	stats := idx.Stats()
	if stats.EpochKeys != 2 {
		t.Fatalf("expected 2 epoch keys, got %d", stats.EpochKeys)
	}

	// Prune before 1500: should remove epoch 0 RAP and delete map key for epoch 0
	idx.PruneBefore(1500)

	statsAfter := idx.Stats()
	if statsAfter.EpochKeys != 1 {
		t.Fatalf("expected 1 epoch key after pruning, got %d", statsAfter.EpochKeys)
	}
	if statsAfter.TotalRAPs != 1 || statsAfter.BoundRAPs != 1 {
		t.Fatalf("unexpected stats after prune: %+v", statsAfter)
	}
}

func TestMediaIndex_StatsAndBoundRAPRatio(t *testing.T) {
	idx := NewMediaIndex()

	// Bound RAP
	_ = idx.ApplyIngestResult(canonicalIngestResult(
		[]mediafacts.TimingRecord{
			{
				Type: mediafacts.TimingRecordTypeRandomAccessPoint,
				RAP: mediafacts.TimingPoint{
					Epoch:     0,
					PID:       257,
					HasPTS:    true,
					PTS90k:    90000,
					SubjectAt: 1000,
				},
			},
		},
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 1000, Joinable: true},
		},
	))

	// Unbound RAP (no RAP timing record)
	_ = idx.ApplyIngestResult(canonicalIngestResult(
		nil,
		[]mediafacts.Event{
			{Kind: mediafacts.EventRandomAccessPoint, Offset: 2000, Joinable: true},
		},
	))

	stats := idx.Stats()
	if stats.TotalRAPs != 2 {
		t.Errorf("TotalRAPs = %d, want 2", stats.TotalRAPs)
	}
	if stats.BoundRAPs != 1 {
		t.Errorf("BoundRAPs = %d, want 1", stats.BoundRAPs)
	}
	if ratio := stats.BoundRAPRatio(); ratio < 0.49 || ratio > 0.51 {
		t.Errorf("BoundRAPRatio = %f, want 0.5", ratio)
	}
}
