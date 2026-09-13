// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package mediafacts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPESOffset_EveryRandomAccessPointSitsOnAVideoPESStart writes the reference's
// own PES-start coordinates from an archived capture.
//
// currentPESOffset is a compatibility contract for the Rust migration, and the
// migration has to be able to check it rather than assume it. The core keeps no
// public list of PES starts, but it does emit a random access point at the
// offset of the PES packet the access unit began in - and that offset *is*
// currentPESOffset, assigned at the video PES start and read back when the
// access unit finishes.
//
// So every offset written here is a coordinate the reference itself produced at
// a video PES start. The Rust side computes video PES starts independently from
// the same bytes, and the two sets are compared outside this test: every offset
// here must be one Rust also calls a video PES start. It is a subset rather than
// an equality, because only the PES packets that turned out to carry an IDR
// raise the event - which is why the comparison is a containment and is stated
// as one instead of being dressed up as more.
//
// Skipped without the archive, which is deliberately not in the repository.
func TestPESOffset_EveryRandomAccessPointSitsOnAVideoPESStart(t *testing.T) {
	dir := os.Getenv("XG2G_PSI_HARDWARE_DIR")
	out := os.Getenv("XG2G_PES_OFFSETS_OUT")
	if dir == "" || out == "" {
		t.Skip("XG2G_PSI_HARDWARE_DIR and XG2G_PES_OFFSETS_OUT must both be set")
	}

	subjects := []struct {
		name    string
		program uint16
	}{
		{"A_orf1hd", 4911},
		{"B1_puls4", 20007},
		{"I_post", 4911},
	}

	result := map[string][]int64{}
	for _, s := range subjects {
		data, err := os.ReadFile(filepath.Join(dir, s.name+".ts")) //nolint:gosec // G304: archive path from the environment
		if err != nil {
			t.Logf("%s: not in the archive; skipping", s.name)
			continue
		}

		core := NewGoCore(s.program)
		ctx := context.Background()
		var offsets []int64

		// One packet per call so the events of a chunk cannot be attributed to
		// the wrong packet. The offsets themselves come from the core, not from
		// this loop; the loop only decides where each packet starts.
		offset := int64(0)
		for at := 0; at+TSPacketSize <= len(data); at += TSPacketSize {
			res, err := core.Ingest(ctx, offset, data[at:at+TSPacketSize])
			if err != nil {
				t.Fatalf("%s at %d: %v", s.name, offset, err)
			}
			for _, ev := range res.Events {
				if ev.Kind == EventRandomAccessPoint {
					offsets = append(offsets, ev.Offset)
				}
			}
			offset += TSPacketSize
		}
		t.Logf("%s: %d random access points", s.name, len(offsets))
		result[s.name] = offsets
	}

	total := 0
	for _, v := range result {
		total += len(v)
	}
	if total == 0 {
		t.Fatal("no random access points; a containment check over nothing is not evidence")
	}

	blob, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(out, blob, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %d reference PES-start coordinates to %s", total, out)
}
