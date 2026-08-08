package model

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// TestReasonDetailCode_ScrambledHasText is the bug this table was collapsed to
// fix. Two HTTP surfaces each carried their own copy of the code-to-text mapping,
// and the copy serving GET /sessions/{id} had no case for DUpstreamScrambled — so
// that endpoint answered with an empty reason_detail for a scrambled upstream,
// the exact failure mode seen in production, while every other surface carried
// the text.
func TestReasonDetailCode_ScrambledHasText(t *testing.T) {
	got := DUpstreamScrambled.Text()
	if got == "" {
		t.Fatal("DUpstreamScrambled must render public text")
	}
	if got != ports.DetailUpstreamScrambled {
		t.Fatalf("scrambled text must come from the shared taxonomy\n  got:  %q\n  want: %q", got, ports.DetailUpstreamScrambled)
	}
}

// TestReasonDetailCode_TextIsTotal keeps the table honest: every code except the
// explicit "no detail" one must render something, so adding a code without wiring
// its text cannot slip through and produce a silently empty reason_detail.
func TestReasonDetailCode_TextIsTotal(t *testing.T) {
	all := []ReasonDetailCode{
		DContextCanceled, DDeadlineExceeded, DRecordingComplete, DSweeperForcedStopStuck,
		DInternalInvariantBreach, DProcessEndedStartup, DProcessExitedUnexpectedly,
		DTranscodeStalled, DUpstreamEndedPrematurely, DUpstreamInputOpenFailed,
		DInvalidUpstreamInput, DUpstreamScrambled, DCopyOutputMissingCodec,
		DEncoderCrashed, DBlackOutput,
	}
	for _, code := range all {
		if code.Text() == "" {
			t.Errorf("%s renders no public text", code)
		}
	}
	if DNone.Text() != "" {
		t.Errorf("DNone must render empty, got %q", DNone.Text())
	}
}

// TestReasonDetailCode_ProcessFailuresQuoteTheTaxonomy pins that the renderer and
// the adapters that produce these failures use the same string, so a session's
// reason_detail always matches what the media layer actually reported.
func TestReasonDetailCode_ProcessFailuresQuoteTheTaxonomy(t *testing.T) {
	pairs := map[ReasonDetailCode]ports.ProcessFailureCause{
		DTranscodeStalled:          ports.CauseTranscodeStalled,
		DUpstreamEndedPrematurely:  ports.CauseUpstreamEnded,
		DUpstreamInputOpenFailed:   ports.CauseUpstreamOpenFailed,
		DInvalidUpstreamInput:      ports.CauseInvalidUpstreamInput,
		DCopyOutputMissingCodec:    ports.CauseCopyOutputMissingCodec,
		DUpstreamScrambled:         ports.CauseUpstreamScrambled,
		DProcessExitedUnexpectedly: ports.CauseProcessExited,
	}
	for code, cause := range pairs {
		if code.Text() != cause.Detail() {
			t.Errorf("%s renders %q but the taxonomy says %q", code, code.Text(), cause.Detail())
		}
		// And the round trip: the rendered text classifies back to the same cause.
		if got := ports.ClassifyProcessFailure(code.Text()); got != cause {
			t.Errorf("%s text classifies as %q, want %q", code, got, cause)
		}
	}
}
