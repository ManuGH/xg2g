// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package manager

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

func scrambledPreflightError(scope ports.ScrambleScope) *ports.PreflightError {
	result := ports.PreflightResult{
		Reason:       ports.PreflightReasonScrambled,
		Detail:       "scrambled",
		ResolvedPort: 8001,
		Scramble: ports.ScrambleEvidence{
			Verdict:    ports.ScrambleVerdictScrambled,
			Fraction:   0.86,
			Classified: 2048,
			Window:     "post_lock",
			Scope:      scope,
		},
	}
	return ports.NewPreflightError(result)
}

// TestPreflightScrambleScope_SeparatesReceiverFromService pins the distinction all
// the way to what a session reports. Both cases are genuinely scrambled, so the
// reason stays the same; what differs is what the operator should do about it.
//
// One service scrambled while others work is a subscription question. Nothing
// descrambling at all is a receiver fault, and on 2026-08-08 that cost a morning
// of failed sessions that looked, in every log line, exactly like the first case.
func TestPreflightScrambleScope_SeparatesReceiverFromService(t *testing.T) {
	t.Parallel()

	t.Run("receiver-wide", func(t *testing.T) {
		_, handled, err := preflightStartReasonError(scrambledPreflightError(ports.ScrambleScopeReceiver))
		if !handled || err == nil {
			t.Fatal("a scrambled preflight must produce a reason error")
		}
		reason, detailCode, detail := classifyReason(err)
		if reason != model.RDescramblerDown {
			t.Fatalf("a receiver-wide fault gets its own reason so the player can translate it, got %q", reason)
		}
		if detailCode != model.DDescramblerDown {
			t.Fatalf("a receiver-wide fault must carry its own detail code, got %q", detailCode)
		}
		if !strings.Contains(strings.ToLower(detail), "receiver is not descrambling") {
			t.Fatalf("the detail must say what is actually wrong, got %q", detail)
		}
		if got := model.DDescramblerDown.Text(); got != ports.DetailDescramblerDown {
			t.Fatalf("public text must come from the shared taxonomy, got %q", got)
		}
		// Upstream of us, so it must not be charged to the server.
		if got := model.TraceStopClassFromReason(model.RDescramblerDown); got != model.PlaybackStopClassInput {
			t.Fatalf("a receiver fault is an input stop, got %q", got)
		}
	})

	t.Run("single service", func(t *testing.T) {
		_, handled, err := preflightStartReasonError(scrambledPreflightError(ports.ScrambleScopeService))
		if !handled || err == nil {
			t.Fatal("a scrambled preflight must produce a reason error")
		}
		reason, detailCode, detail := classifyReason(err)
		if reason != model.RUpstreamScrambled {
			t.Fatalf("expected the scrambled reason, got %q", reason)
		}
		if got := model.TraceStopClassFromReason(model.RUpstreamScrambled); got != model.PlaybackStopClassInput {
			t.Fatalf("a scrambled upstream is an input stop, not a server fault; got %q", got)
		}
		if detailCode != model.DUpstreamScrambled {
			t.Fatalf("a single scrambled service keeps the per-service detail, got %q", detailCode)
		}
		if strings.Contains(strings.ToLower(detail), "receiver is not descrambling") {
			t.Fatalf("must not blame the receiver on this evidence, got %q", detail)
		}
	})

	t.Run("unattributed falls back to the service", func(t *testing.T) {
		_, _, err := preflightStartReasonError(scrambledPreflightError(ports.ScrambleScopeUnknown))
		_, detailCode, _ := classifyReason(err)
		if detailCode != model.DUpstreamScrambled {
			t.Fatalf("without attribution the safe answer is the service, got %q", detailCode)
		}
	})

	// Keep the compile-time dependency on lifecycle explicit: classifyReason maps
	// through it, and this test is meaningless if that path changes shape.
	var _ = lifecycle.PhaseFromState
}
