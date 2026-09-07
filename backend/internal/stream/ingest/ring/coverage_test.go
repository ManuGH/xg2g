// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ring

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// narrowCore answers correctly about part of the stream.
//
// Not a broken core and not a hostile one: everything it says is true, the
// offset is right, the PSI is real. It simply was not asked about the rest, and
// the fields it did not fill hold the zero values that a complete core uses to
// say "no entry point", "nothing scrambled", "no parameter sets".
//
// This is the shape the Rust core has during the media-core migration, which is
// why the ring has to refuse it rather than rely on nobody wiring it up.
type narrowCore struct {
	mediafacts.Core
	coverage mediafacts.ParseCoverage
	calls    int
}

func (c *narrowCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	c.calls++
	res, err := c.Core.Ingest(ctx, startOffset, data)
	res.Coverage = c.coverage
	return res, err
}

func (c *narrowCore) SetTargetProgram(ctx context.Context, programNumber uint16) (mediafacts.ParseResult, error) {
	c.calls++
	res, err := c.Core.SetTargetProgram(ctx, programNumber)
	res.Coverage = c.coverage
	return res, err
}

// TestCoverage_ARingCommitsNothingFromAnIncompleteResult is the gate that keeps
// a differential from becoming a cutover.
//
// A core answering about PSI alone is a real thing during the migration, and it
// reaches the ring through the same interface a complete one does. What stops it
// being committed has to be the ring itself, not the absence of configuration:
// configuration is a decision somebody can change without noticing what it was
// protecting.
func TestCoverage_ARingCommitsNothingFromAnIncompleteResult(t *testing.T) {
	for _, tc := range []struct {
		name     string
		coverage mediafacts.ParseCoverage
	}{
		{"psi only", mediafacts.ParseCoveragePSIOnly},
		// The zero value, which is what a core that has never heard of coverage
		// returns. It must fail closed rather than read as complete.
		{"unstated", mediafacts.ParseCoverageUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMasterRing(400 * TSPacketSize)
			defer r.Close()
			core := &narrowCore{Core: r.core, coverage: tc.coverage}
			r.core = core

			headBefore, genBefore := r.Head(), r.Generation()
			factsBefore := r.facts
			preambleBefore := r.PATPMTPreamble()

			n, err := r.Push(context.Background(), onePacket())
			if !errors.Is(err, ErrCoreIncompleteResult) {
				t.Fatalf("Push returned (%d, %v), want ErrCoreIncompleteResult", n, err)
			}
			if n != 0 {
				t.Errorf("Push reported %d bytes written", n)
			}

			// Nothing moved: not the bytes, not the epoch, not the facts, not
			// the tables a subscriber would be handed.
			if got := r.Head(); got != headBefore {
				t.Errorf("head moved from %d to %d", headBefore, got)
			}
			if got := r.Generation(); got != genBefore {
				t.Errorf("generation moved from %d to %d", genBefore, got)
			}
			if got := r.facts; !reflect.DeepEqual(got, factsBefore) {
				t.Errorf("facts changed to %+v", got)
			}
			if got := r.PATPMTPreamble(); len(got) != len(preambleBefore) {
				t.Errorf("preamble changed from %d to %d bytes", len(preambleBefore), len(got))
			}

			// And the core is finished. A ring that asked again would be giving
			// a second chance to something that answered about the wrong thing.
			callsAfterFirst := core.calls
			if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
				t.Errorf("second Push returned %v, want ErrCoreUnusable", err)
			}
			if core.calls != callsAfterFirst {
				t.Errorf("the core was asked again after answering incompletely")
			}
		})
	}
}

// TestCoverage_ATargetChangeIsRefusedTheSameWay covers the other call that
// commits a core result. Both paths publish facts, so both have to gate.
func TestCoverage_ATargetChangeIsRefusedTheSameWay(t *testing.T) {
	r := NewMasterRing(400 * TSPacketSize)
	defer r.Close()
	core := &narrowCore{Core: r.core, coverage: mediafacts.ParseCoveragePSIOnly}
	r.core = core

	factsBefore := r.facts
	if err := r.SetTargetProgram(context.Background(), 7); !errors.Is(err, ErrCoreIncompleteResult) {
		t.Fatalf("SetTargetProgram returned %v, want ErrCoreIncompleteResult", err)
	}
	if got := r.facts; !reflect.DeepEqual(got, factsBefore) {
		t.Errorf("facts changed to %+v", got)
	}
	if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
		t.Errorf("the core stayed usable after answering incompletely: %v", err)
	}
}

// TestCoverage_TheInProcessCoreCoversEverything is the other half of the gate.
// A check that refuses everything would pass the tests above and break the
// product, so the reference core is held to the coverage it claims.
func TestCoverage_TheInProcessCoreCoversEverything(t *testing.T) {
	c := mediafacts.NewGoCore(1)
	ctx := context.Background()

	res, err := c.Ingest(ctx, 0, onePacket())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !res.Covers(mediafacts.ParseCoverageComplete) {
		t.Errorf("GoCore.Ingest reported coverage %s", res.Coverage)
	}

	res, err = c.SetTargetProgram(ctx, 2)
	if err != nil {
		t.Fatalf("set target: %v", err)
	}
	if !res.Covers(mediafacts.ParseCoverageComplete) {
		t.Errorf("GoCore.SetTargetProgram reported coverage %s", res.Coverage)
	}
}

// throughCore answers correctly about everything except how much it read.
type throughCore struct {
	mediafacts.Core
	delta int64
}

func (c throughCore) Ingest(ctx context.Context, startOffset int64, data []byte) (mediafacts.ParseResult, error) {
	res, err := c.Core.Ingest(ctx, startOffset, data)
	res.ProcessedThroughOffset += c.delta
	return res, err
}

// TestCoverage_AnOffsetThatIsNotTheWholeChunkCommitsNothing is the atomicity
// gate, checked from both sides of the exact value.
//
// Short is a core that stopped early and left the ring with bytes it has no
// meaning for - that case has been covered since the seam tests. Long is the
// one a remote core makes reachable: a peer claiming to have read past what it
// was given is describing bytes that do not exist yet, and it costs a failing
// process nothing to say so. Neither may commit, and both finish the core.
func TestCoverage_AnOffsetThatIsNotTheWholeChunkCommitsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		delta int64
	}{
		{"one byte short", -1},
		{"one byte long", 1},
		{"a whole packet short", -TSPacketSize},
		{"a whole packet long", TSPacketSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMasterRing(400 * TSPacketSize)
			defer r.Close()
			r.core = throughCore{Core: r.core, delta: tc.delta}

			headBefore, genBefore := r.Head(), r.Generation()
			n, err := r.Push(context.Background(), onePacket())
			if err == nil {
				t.Fatalf("Push returned (%d, nil) for an offset that was %+d", n, tc.delta)
			}
			if !errors.Is(err, ErrCoreIncomplete) {
				t.Errorf("Push returned %v, want ErrCoreIncomplete", err)
			}
			if n != 0 {
				t.Errorf("Push reported %d bytes written", n)
			}
			if got := r.Head(); got != headBefore {
				t.Errorf("head moved from %d to %d", headBefore, got)
			}
			if got := r.Generation(); got != genBefore {
				t.Errorf("generation moved from %d to %d", genBefore, got)
			}
			if _, err := r.Push(context.Background(), onePacket()); !errors.Is(err, ErrCoreUnusable) {
				t.Errorf("the core stayed usable: %v", err)
			}
		})
	}
}
