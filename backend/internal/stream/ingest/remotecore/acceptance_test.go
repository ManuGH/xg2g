// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package remotecore

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
	"github.com/ManuGH/xg2g/internal/stream/ingest/normalizer"
)

// What a full-sized chunk actually costs to send, receive and answer.
//
// DefaultIngestDeadline was chosen before there was anything on the other end of
// a socket to measure. This is the measurement: the largest chunk the ring
// produces by default, over a real Unix socket, to a real core.
//
// It fails on max rather than on a percentile. A chunk that misses the deadline
// is a chunk that retires the core - there is no such thing as a tolerable
// fraction of those - so the number that has to fit is the worst one, not the
// median. Reported all the same, because a median close to the limit means the
// limit is about to be reached even while this passes.
//
// Skipped without a core binary. Linux is where the answer counts; a number from
// the development machine says nothing about the host that runs this.
func TestAcceptance_AFullChunkRoundTripsInsideTheDeadline(t *testing.T) {
	bin := os.Getenv("XG2G_MEDIA_CORE_BIN")
	if bin == "" {
		t.Skip("XG2G_MEDIA_CORE_BIN not set; this is the acceptance run, not a unit test")
	}
	if testing.Short() {
		t.Skip("measurement, not a smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	core, err := Start(ctx, bin, 1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = core.Close() }()

	chunk := make([]byte, normalizer.DefaultConfig().StagingBufferCapacity)
	const rounds = 200

	// Not measured: the first exchange also pays for whatever the core does once.
	if _, err := core.Ingest(ctx, 0, chunk); err != nil {
		t.Fatalf("warmup ingest: %v", err)
	}

	took := make([]time.Duration, 0, rounds)
	offset := int64(len(chunk))
	for i := 0; i < rounds; i++ {
		began := time.Now()
		if _, err := core.Ingest(ctx, offset, chunk); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
		took = append(took, time.Since(began))
		offset += int64(len(chunk))
	}

	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	p := func(q float64) time.Duration { return took[int(float64(len(took)-1)*q)] }
	p50, p99, max := p(0.50), p(0.99), took[len(took)-1]

	t.Logf("%d round trips of %d bytes: p50=%v p99=%v max=%v (deadline %v)",
		rounds, len(chunk), p50, p99, max, mediafacts.DefaultIngestDeadline)

	if max > mediafacts.DefaultIngestDeadline {
		t.Fatalf("slowest round trip %v exceeds DefaultIngestDeadline %v", max, mediafacts.DefaultIngestDeadline)
	}
	// A median already at a fifth of the budget means the budget is not what it
	// looks like. Loud, and separate from the failure above.
	if p50 > mediafacts.DefaultIngestDeadline/5 {
		t.Errorf("median round trip %v is more than a fifth of the %v budget; "+
			"the deadline is closer than it appears", p50, mediafacts.DefaultIngestDeadline)
	}
}
