// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/timeline"
)

// Real broadcast, through the real core, into the MediaIndex, at the chunk sizes
// production actually produces.
//
// In real H.264 broadcast a RAP is routinely established one PES after its own
// header: an all-intra picture made of non-IDR slices is only an entry point once
// the access unit has ended. Before media-core published the binding, the index
// joined a RAP to the PES timing record of the same result - which, at the
// pacer's ~108-packet chunks, left such RAPs unbound on every capture that
// carries them. Nothing in the authored fixtures had a RAP that arrived later
// than its header, so nothing noticed.
//
// Every capture the Step 5d manifest feeds is replayed with the target in effect
// where the manifest feeds it. The property held is the one that has to hold for
// any stream: where the chunk boundaries fall does not change which RAPs are
// bound, or to what. On these captures every RAP's PES carries a PTS, so every
// RAP must also be bound - that part is a fact about the captures, measured, not
// a rule for all input.
func TestVideoHardware_RAPTimingBindingIsChunkIndependent(t *testing.T) {
	bin := requireRealCore(t)
	dir := requireHardwareDir(t)
	captures, cases := loadHardwareManifest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()

	type subject struct {
		capture string
		target  uint16
	}
	var subjects []subject
	seen := make(map[subject]bool)
	for _, c := range cases {
		target := c.initial
		for _, step := range c.steps {
			switch {
			case step.isFeed:
				s := subject{capture: step.feed, target: target}
				if !seen[s] {
					seen[s] = true
					subjects = append(subjects, s)
				}
			case !step.newCore:
				target = step.setTarget
			}
		}
	}
	// In packets: one, the pacer's working size at ~8 Mbit/s and 20 ms, and a
	// chunk far larger than any access unit.
	sizes := []int{1, 108, 5576}

	totalRAPs := 0
	for n, subj := range subjects {
		c, ok := captures[subj.capture]
		if !ok {
			t.Fatalf("the manifest feeds %s but does not declare it", subj.capture)
		}
		data := readCapture(t, dir, c)

		var reference []timeline.RAPEntry
		for i, packets := range sizes {
			// Subjects are numbered in manifest order; the manifest says what each is.
			name := fmt.Sprintf("subject%d/%dpackets", n, packets)
			t.Run(name, func(t *testing.T) {
				raps := indexCaptureRAPs(ctx, t, bin, subj.target, data, packets*188)
				for _, rap := range raps {
					if !rap.HasTimingBinding || !rap.HasPTS {
						t.Fatalf("%s: RAP at %d is not bound to a PTS: %+v", name, rap.Offset, rap)
					}
				}
				if i == 0 {
					reference = raps
					totalRAPs += len(raps)
					t.Logf("%s: %d RAPs, all bound - the reference", name, len(raps))
					return
				}
				if fmt.Sprint(raps) != fmt.Sprint(reference) {
					t.Fatalf("%s indexed other RAPs than the first chunking:\n  got  %+v\n  want %+v", name, raps, reference)
				}
				t.Logf("%s: %d RAPs, identical to the reference", name, len(raps))
			})
		}
	}
	if totalRAPs == 0 {
		t.Fatal("no capture produced a RAP; the replay proved nothing")
	}
}

// indexCaptureRAPs feeds data to a fresh real core in chunkBytes slices and
// every result into a fresh MediaIndex, the way MasterRing hands results on.
func indexCaptureRAPs(ctx context.Context, t *testing.T, bin string, target uint16, data []byte, chunkBytes int) []timeline.RAPEntry {
	t.Helper()
	remote, err := Start(ctx, bin, target)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := remote.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	idx := timeline.NewMediaIndex()
	events := 0
	for start := 0; start < len(data); start += chunkBytes {
		end := min(start+chunkBytes, len(data))
		res, err := remote.Ingest(ctx, int64(start), data[start:end])
		if err != nil {
			t.Fatalf("offset %d: the real core failed: %v", start, err)
		}
		for _, ev := range res.Events {
			if ev.Kind == mediafacts.EventRandomAccessPoint {
				events++
			}
		}
		if err := idx.ApplyIngestResult(res); err != nil {
			t.Fatalf("offset %d: the index refused the core's result: %v", start, err)
		}
	}
	raps := idx.RAPsBetween(0, int64(len(data)))
	if len(raps) != events {
		t.Fatalf("%d RAP events, %d RAPs indexed", events, len(raps))
	}
	return raps
}
