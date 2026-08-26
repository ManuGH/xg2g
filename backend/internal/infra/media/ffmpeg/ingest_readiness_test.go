// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// joinable describes a stream a decoder could be started on, so each test below
// can break exactly one thing.
func joinableFacts() ports.LiveSourceFacts {
	return ports.LiveSourceFacts{
		Generation:        3,
		HasPAT:            true,
		HasPMT:            true,
		VideoPID:          256,
		VideoCodec:        "h264",
		ParameterSetsSeen: true,
		CleanEntryPoints:  2,
		CleanAccessUnits:  40,
		ClearVideoPackets: 5000,
	}
}

func TestPreflightResultFromFacts_Reasons(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*ports.LiveSourceFacts)
		attachErr  error
		wantReason ports.PreflightReason
		wantDetail string
	}{
		{
			name: "receiver is not descrambling the service",
			mutate: func(f *ports.LiveSourceFacts) {
				f.ScrambledVideoPackets = 4000
				f.CleanAccessUnits = 0
				f.CleanEntryPoints = 0
			},
			wantReason: ports.PreflightReasonScrambled,
			wantDetail: "no clean access unit",
		},
		{
			name:       "the stream has not said what it contains",
			mutate:     func(f *ports.LiveSourceFacts) { f.HasPMT = false },
			wantReason: ports.PreflightReasonInvalidTS,
			wantDetail: detailTopologyNotReady,
		},
		{
			name:       "pmt names no video stream",
			mutate:     func(f *ports.LiveSourceFacts) { f.VideoPID = 0 },
			wantReason: ports.PreflightReasonNoVideo,
			wantDetail: "no video elementary stream",
		},
		{
			name:       "decoder configuration has not arrived",
			mutate:     func(f *ports.LiveSourceFacts) { f.ParameterSetsSeen = false },
			wantReason: ports.PreflightReasonInvalidTS,
			wantDetail: "no parameter sets",
		},
		{
			name:       "no entry point came through in the clear",
			mutate:     func(f *ports.LiveSourceFacts) { f.CleanEntryPoints = 0 },
			wantReason: ports.PreflightReasonInvalidTS,
			wantDetail: "no clean entry point",
		},
		{
			name:       "joinable stream, attach ran out of time",
			mutate:     func(*ports.LiveSourceFacts) {},
			attachErr:  context.DeadlineExceeded,
			wantReason: ports.PreflightReasonTimeout,
			wantDetail: detailAttachTimeout,
		},
		{
			name:       "joinable stream, upstream simply not there",
			mutate:     func(*ports.LiveSourceFacts) {},
			attachErr:  errors.New("acquire shared ingest: no upstream"),
			wantReason: ports.PreflightReasonUnreachable,
			wantDetail: detailSharedIngestUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			facts := joinableFacts()
			tc.mutate(&facts)

			got := preflightResultFromFacts(facts, tc.attachErr)

			if got.OK {
				t.Error("OK = true on a failed attach")
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q (detail %q)", got.Reason, tc.wantReason, got.Detail)
			}
			if !strings.Contains(got.Detail, tc.wantDetail) {
				t.Errorf("Detail = %q, want it to contain %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

// Scrambling outranks joinability. A stream the receiver is not descrambling is
// also not joinable, and reporting "invalid_ts" there would send someone looking
// at the transport stream instead of at the softcam.
func TestPreflightResultFromFacts_ScramblingOutranksJoinability(t *testing.T) {
	facts := joinableFacts()
	facts.ScrambledVideoPackets = 9000
	facts.CleanAccessUnits = 0
	facts.CleanEntryPoints = 0
	facts.ParameterSetsSeen = false

	if got := preflightResultFromFacts(facts, nil); got.Reason != ports.PreflightReasonScrambled {
		t.Errorf("Reason = %q, want %q", got.Reason, ports.PreflightReasonScrambled)
	}
}

// Scrambled packets alone are not a verdict: a stream descrambles a moment after
// it starts, and the clean access units are what say it did.
func TestPreflightResultFromFacts_ScrambledPacketsWithCleanPicturesIsNotScrambled(t *testing.T) {
	facts := joinableFacts()
	facts.ScrambledVideoPackets = 300 // the lock-in before the control word arrived
	facts.CleanAccessUnits = 25

	if got := preflightResultFromFacts(facts, errors.New("boom")); got.Reason == ports.PreflightReasonScrambled {
		t.Errorf("Reason = %q; clean access units prove the receiver is descrambling", got.Reason)
	}
}

// The attach error has to survive into the detail, or a trace says which criterion
// failed without saying what the caller actually saw.
func TestPreflightResultFromFacts_CarriesTheAttachError(t *testing.T) {
	const upstream = "upstream refused the connection"
	got := preflightResultFromFacts(joinableFacts(), errors.New(upstream))

	if !strings.Contains(got.Detail, upstream) {
		t.Errorf("Detail = %q, want it to carry %q", got.Detail, upstream)
	}
}

// The orchestrator recognises a structured preflight failure by ErrNoValidTS, and
// maps its result into PlaybackTrace.PreflightReason. That path has to keep
// working, or the reason silently stops being published.
func TestPreflightErrorFromFacts_ReachesTheStructuredFailurePath(t *testing.T) {
	facts := joinableFacts()
	facts.HasPMT = false

	err := preflightErrorFromFacts(facts, errors.New("attach failed"))

	if !errors.Is(err, ports.ErrNoValidTS) {
		t.Fatalf("error %v is not recognised as a structured preflight failure", err)
	}

	var pErr *ports.PreflightError
	if !errors.As(err, &pErr) {
		t.Fatal("error does not unwrap to a PreflightError")
	}
	result := pErr.StructuredResult()
	if result.Reason != ports.PreflightReasonInvalidTS {
		t.Errorf("Reason = %q, want %q", result.Reason, ports.PreflightReasonInvalidTS)
	}
	if result.OK {
		t.Error("StructuredResult reports OK on a failure")
	}
}
