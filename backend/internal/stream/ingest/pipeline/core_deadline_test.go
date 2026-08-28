// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/stream/ingest/mediafacts"
)

// The core's deadline and the viewer's patience are set in different packages for
// different reasons, and neither knows about the other. This is where they are
// held together.
//
// A zap fails when transport readiness is not reached within ReadyTimeout, and
// reaching readiness takes many chunks. A core that stops answering costs that
// budget exactly one deadline - the ring retires it and every later chunk fails
// at once - so what matters is not that the deadline is small, but that it stays
// a fraction of the budget it is spent from. If someone raises the deadline to
// something comfortable for a slow socket, a hung core stops being a failed chunk
// and becomes a failed zap, and nothing else in the tree would notice.
func TestIngestDeadlineStaysWellInsideTheZapBudget(t *testing.T) {
	ready := DefaultPreparationConfig().ReadyTimeout
	deadline := mediafacts.DefaultIngestDeadline

	if ready <= 0 {
		t.Fatalf("ReadyTimeout = %v; the budget this is measured against is gone", ready)
	}

	// Four is not a magic number, it is the smallest ratio at which a hung core
	// still leaves the majority of a zap for the work that follows it.
	const minRatio = 4
	if deadline*minRatio > ready {
		t.Errorf("ingest deadline %v is more than a quarter of the zap budget %v; a hung core would take most of a viewer's wait with it",
			deadline, ready)
	}
	t.Logf("ingest deadline %v against a zap budget of %v (ratio %.1f)", deadline, ready, float64(ready)/float64(deadline))
}
