package ports

import "testing"

// TestProcessFailureDetails_AreTheWireContract pins the exact text of every
// canonical detail with literals written out here rather than referencing the
// constants. That is the point: these strings reach clients as reason_detail and
// appear in the OpenAPI examples, so a rename must fail loudly here instead of
// silently changing the contract.
func TestProcessFailureDetails_AreTheWireContract(t *testing.T) {
	golden := map[ProcessFailureCause]string{
		CauseTranscodeStalled:       "transcode stalled - no progress detected",
		CauseBlackOutput:            "runtime path correctness failed - black output detected",
		CauseCopyOutputMissingCodec: "copy output missing codec parameters",
		CauseUpstreamEnded:          "upstream stream ended prematurely",
		CauseUpstreamOpenFailed:     "failed to open upstream input",
		CauseUpstreamIOError:        "upstream input/output error",
		CauseInvalidUpstreamInput:   "invalid upstream input data",
		CauseUpstreamScrambled:      "upstream stream is scrambled (encrypted, could not be descrambled)",
		CauseEncoderCrashed:         "encoder process crashed",
		CauseProcessExited:          "process exited unexpectedly",
	}
	for cause, want := range golden {
		if got := cause.Detail(); got != want {
			t.Errorf("%s: wire text changed\n  got:  %q\n  want: %q", cause, got, want)
		}
	}
	if len(golden) != len(processFailureTable) {
		t.Fatalf("the taxonomy has %d causes but %d are pinned here — pin the new one",
			len(processFailureTable), len(golden))
	}
}

// TestClassifyProcessFailure_RoundTrips is the invariant the restructure buys:
// every cause's own text classifies back to that cause. Before, each consumer
// carried its own copy of the prose and matched it independently, so a copy could
// (and did) drift out of agreement with the producer.
func TestClassifyProcessFailure_RoundTrips(t *testing.T) {
	for _, entry := range processFailureTable {
		if got := ClassifyProcessFailure(entry.Text); got != entry.Cause {
			t.Errorf("%q classified as %q, want %q", entry.Text, got, entry.Cause)
		}
	}
}

// TestClassifyProcessFailure_SeesThroughWrappers covers how details actually
// arrive: the layer that observed the failure prefixes its own context, e.g.
// "process died during startup: transcode stalled - no progress detected".
func TestClassifyProcessFailure_SeesThroughWrappers(t *testing.T) {
	cases := map[string]ProcessFailureCause{
		"process died during startup: transcode stalled - no progress detected": CauseTranscodeStalled,
		"encoder process crashed (segmentation fault)":                          CauseEncoderCrashed,
		"ENCODER PROCESS CRASHED (bus error)":                                   CauseEncoderCrashed,
		"":                                                                      CauseUnknown,
		"something nobody has classified yet":                                   CauseUnknown,
	}
	for detail, want := range cases {
		if got := ClassifyProcessFailure(detail); got != want {
			t.Errorf("ClassifyProcessFailure(%q) = %q, want %q", detail, got, want)
		}
	}
}

// TestProcessFailurePolicy_EncodeVsInput is the policy line that the incident and
// the crash investigation both turned on: a lighter profile is worth trying when
// the ENCODE path gave out, and never when the INPUT is unusable.
func TestProcessFailurePolicy_EncodeVsInput(t *testing.T) {
	encodePath := []ProcessFailureCause{CauseTranscodeStalled, CauseEncoderCrashed}
	for _, c := range encodePath {
		if !c.RecoverableByProfileChange() {
			t.Errorf("%s is an encode-path failure and should be worth a lighter profile", c)
		}
	}

	inputPath := []ProcessFailureCause{
		CauseUpstreamScrambled, CauseUpstreamEnded, CauseUpstreamOpenFailed,
		CauseInvalidUpstreamInput, CauseUpstreamIOError,
	}
	for _, c := range inputPath {
		if c.RecoverableByProfileChange() {
			t.Errorf("%s is an input failure — no profile change repairs it", c)
		}
	}

	// A scrambled upstream is the one that must never buy a retry either: it is
	// deterministic, and retrying it cost a real session 52 seconds.
	if CauseUpstreamScrambled.RetriableAsIs() {
		t.Error("a scrambled upstream is deterministic and must not be retried as-is")
	}
	if CauseEncoderCrashed.RetriableAsIs() {
		t.Error("repeating the identical profile after a fault would just fault again")
	}
}

// TestProcessFailurePriority_CrashOutranksEverything keeps the reported cause the
// actual cause: a fault takes the encode path down, so every symptom observed
// around it is downstream of the fault.
func TestProcessFailurePriority_CrashOutranksEverything(t *testing.T) {
	crash := CauseEncoderCrashed.Priority()
	for _, entry := range processFailureTable {
		if entry.Cause == CauseEncoderCrashed {
			continue
		}
		if entry.Cause.Priority() >= crash {
			t.Errorf("%s (%d) must not outrank a crash (%d)", entry.Cause, entry.Cause.Priority(), crash)
		}
	}
	if CauseUnknown.Priority() != 0 {
		t.Error("an unclassified failure must rank lowest")
	}
}
