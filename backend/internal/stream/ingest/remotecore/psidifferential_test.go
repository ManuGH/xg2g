// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The PSI-scoped part of a result, rendered the way the shared corpus renders
// it.
//
// Comparing rendered lines rather than structs is what makes a disagreement
// readable and what keeps this comparison the same one the corpus already makes.
// It also keeps the omission honest: the fields the Rust core does not cover -
// random access, scrambling, parameter sets - are absent from these lines
// because they are absent from its answer, not because a struct comparison
// happened to skip them.
type psiScope struct {
	facts  string
	tracks []string
	events string
	psi    string
}

func scopeOf(res mediafacts.ParseResult) psiScope {
	pids := make([]string, 0, len(res.Facts.AudioPIDs))
	for _, p := range res.Facts.AudioPIDs {
		pids = append(pids, fmt.Sprintf("%d", p))
	}
	s := psiScope{
		facts: fmt.Sprintf(
			"through=%d hasPAT=%d hasPMT=%d pmtVersion=%d programNumber=%d pmtPID=%d videoPID=%d videoCodec=%s audioPIDs=%s",
			res.ProcessedThroughOffset, bit(res.Facts.HasPAT), bit(res.Facts.HasPMT),
			res.Facts.PMTVersion, res.Facts.ProgramNumber, res.Facts.PMTPID,
			res.Facts.VideoPID, res.Facts.VideoCodec, strings.Join(pids, ",")),
		psi: fmt.Sprintf("pat=%s pmt=%s",
			sectionsHex(res.PSI.PATSections), sectionsHex(res.PSI.PMTSections)),
	}
	for _, tr := range res.Facts.AudioTracks {
		s.tracks = append(s.tracks, fmt.Sprintf(
			"pid=%d streamType=%d codec=%s lang=%s channels=%d multichannel=%d componentType=%d hasComponentType=%d",
			tr.PID, tr.StreamType, tr.Codec, tr.Language,
			tr.Declared.Channels, bit(tr.Declared.Multichannel),
			tr.Declared.ComponentType, bit(tr.Declared.HasComponentType)))
	}
	names := make([]string, 0, len(res.Events))
	for _, ev := range res.Events {
		switch ev.Kind {
		case mediafacts.EventProgramIdentityChanged:
			names = append(names, "identity")
		case mediafacts.EventRandomAccessPoint:
			names = append(names, fmt.Sprintf("rap@%d", ev.Offset))
		default:
			names = append(names, fmt.Sprintf("unknown(%d)", ev.Kind))
		}
	}
	s.events = strings.Join(names, " ")
	return s
}

func bit(b bool) int {
	if b {
		return 1
	}
	return 0
}

func sectionsHex(sections [][]byte) string {
	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		parts = append(parts, hex.EncodeToString(s))
	}
	return strings.Join(parts, ",")
}

// diff reports every field two scopes disagree about rather than the first, so a
// semantic difference shows its whole shape at once.
func (s psiScope) diff(other psiScope, mine, theirs string) []string {
	var bad []string
	if s.facts != other.facts {
		bad = append(bad, fmt.Sprintf("  facts %s  %s\n  facts %s  %s", mine, s.facts, theirs, other.facts))
	}
	if len(s.tracks) != len(other.tracks) {
		bad = append(bad, fmt.Sprintf("  tracks %s %d, %s %d", mine, len(s.tracks), theirs, len(other.tracks)))
	} else {
		for i := range s.tracks {
			if s.tracks[i] != other.tracks[i] {
				bad = append(bad, fmt.Sprintf("  track %d %s  %s\n  track %d %s  %s",
					i, mine, s.tracks[i], i, theirs, other.tracks[i]))
			}
		}
	}
	if s.events != other.events {
		bad = append(bad, fmt.Sprintf("  events %s  [%s]\n  events %s  [%s]", mine, s.events, theirs, other.events))
	}
	if s.psi != other.psi {
		bad = append(bad, fmt.Sprintf("  psi %s  %s\n  psi %s  %s", mine, s.psi, theirs, other.psi))
	}
	return bad
}

// TestPSIDifferential_TheRealRustCoreAgreesCallByCall is the proof Step 5c
// exists for.
//
// The same raw transport bytes reach two implementations - one in this process,
// one behind a Unix socket in a Rust process - and every call is compared before
// the next is made. Comparing only at the end would let a stream that diverged
// and re-converged pass.
//
// Three-way, not two. The authored corpus expectation is checked as well as the
// two implementations against each other, because R6, R7 and R8 each found a
// place where the two agreed and were both wrong. Agreement is not correctness;
// it is only evidence when the thing agreed with was written down first.
func TestPSIDifferential_TheRealRustCoreAgreesCallByCall(t *testing.T) {
	bin := requireRealCore(t)
	cases := loadPSICorpus(t)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	compared := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			remote, err := Start(ctx, bin, c.initial)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer func() {
				if err := remote.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			}()

			local := mediafacts.NewGoCore(c.initial)
			offset := int64(0)

			for i, call := range c.calls {
				var goRes, rustRes mediafacts.ParseResult
				var goErr, rustErr error

				if call.isChunk {
					goRes, goErr = local.Ingest(ctx, offset, call.chunk)
					rustRes, rustErr = remote.Ingest(ctx, offset, call.chunk)
					offset += int64(len(call.chunk))
				} else {
					goRes, goErr = local.SetTargetProgram(ctx, call.target)
					rustRes, rustErr = remote.SetTargetProgram(ctx, call.target)
				}
				if goErr != nil {
					t.Fatalf("call %d (%s): the reference failed: %v", i+1, call, goErr)
				}
				if rustErr != nil {
					t.Fatalf("call %d (%s): the real core failed: %v", i+1, call, rustErr)
				}

				// The one place the two are allowed to differ, and it is said
				// out loud rather than inferred: the reference covers the whole
				// stream, the Rust core covers PSI.
				if !goRes.Covers(mediafacts.ParseCoverageComplete) {
					t.Fatalf("call %d: the reference reported coverage %s", i+1, goRes.Coverage)
				}
				if !rustRes.Covers(mediafacts.ParseCoveragePSIOnly) {
					t.Fatalf("call %d: the real core reported coverage %s", i+1, rustRes.Coverage)
				}

				goScope, rustScope := scopeOf(goRes), scopeOf(rustRes)
				want := psiScope{
					facts:  call.wantFacts,
					tracks: call.wantTracks,
					events: call.wantEvents,
					psi:    call.wantPSI,
				}

				var bad []string
				bad = append(bad, goScope.diff(rustScope, "go  ", "rust")...)
				// Against the file as well, so neither implementation can drag
				// the other somewhere the contract never said.
				bad = append(bad, goScope.diff(want, "go  ", "want")...)
				bad = append(bad, rustScope.diff(want, "rust", "want")...)
				if len(bad) > 0 {
					t.Fatalf("call %d of %d (%s):\n%s", i+1, len(c.calls), call, strings.Join(bad, "\n"))
				}
				compared++
			}
		})
	}
	t.Logf("real-process differential: %d cases, %d calls, all exact", len(cases), compared)
	if compared != corpusCalls(cases) {
		t.Errorf("compared %d calls, the corpus has %d", compared, corpusCalls(cases))
	}
}
