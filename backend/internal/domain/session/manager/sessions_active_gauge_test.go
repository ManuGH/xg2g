// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package manager

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestSessionsActiveGaugeTracksActiveMap verifies that the xg2g_v3_sessions_active
// gauge follows the orchestrator's live o.active map. registerActive/unregisterActive
// publish the absolute len(o.active), so assertions are independent of prior state.
func TestSessionsActiveGaugeTracksActiveMap(t *testing.T) {
	o := &Orchestrator{}
	noop := context.CancelFunc(func() {})

	o.registerActive("s1", noop)
	o.registerActive("s2", noop)
	if got := testutil.ToFloat64(sessionsActive); got != 2 {
		t.Fatalf("after 2 registers: gauge = %v, want 2", got)
	}

	// Idempotent re-register of the same id must not double-count.
	o.registerActive("s2", noop)
	if got := testutil.ToFloat64(sessionsActive); got != 2 {
		t.Fatalf("after re-registering s2: gauge = %v, want 2", got)
	}

	o.unregisterActive("s1")
	if got := testutil.ToFloat64(sessionsActive); got != 1 {
		t.Fatalf("after 1 unregister: gauge = %v, want 1", got)
	}

	o.unregisterActive("s2")
	if got := testutil.ToFloat64(sessionsActive); got != 0 {
		t.Fatalf("after all unregister: gauge = %v, want 0", got)
	}
}
