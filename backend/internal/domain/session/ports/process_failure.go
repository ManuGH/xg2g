// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ports

import "strings"

// Canonical text for each classified process failure.
//
// These strings are API-visible: they reach clients as `reason_detail`, and the
// OpenAPI examples quote them. Changing one changes the contract.
//
// They live here, on the layer both the media adapters and the session domain
// already depend on, because they used to live in six places at once. Every
// consumer re-matched the prose with strings.Contains — the adapter that produced
// it, the priority table, the lifecycle classifier, the recovery policy, and two
// separate HTTP mappers. That is not a hypothetical hazard: the two HTTP copies
// had already drifted, and the one serving GET /sessions/{id} was missing the
// scrambled case entirely, so the REST detail for a scrambled upstream came back
// empty while the same failure carried its text everywhere else.
const (
	DetailTranscodeStalled          = "transcode stalled - no progress detected"
	DetailBlackOutput               = "runtime path correctness failed - black output detected"
	DetailCopyOutputMissingCodec    = "copy output missing codec parameters"
	DetailUpstreamEndedPrematurely  = "upstream stream ended prematurely"
	DetailUpstreamOpenFailed        = "failed to open upstream input"
	DetailUpstreamIOError           = "upstream input/output error"
	DetailInvalidUpstreamInput      = "invalid upstream input data"
	DetailUpstreamScrambled         = "upstream stream is scrambled (encrypted, could not be descrambled)"
	DetailProcessExitedUnexpectedly = "process exited unexpectedly"
	// DetailEncoderCrashed is a PREFIX: the signal name is appended, so matchers
	// must not compare it for equality.
	DetailEncoderCrashed = "encoder process crashed"
)

// ProcessFailureCause is the bounded taxonomy for how a media process failed.
// It exists so policy decisions — what may be retried, what a lighter profile
// could survive, which detail outranks which — are made on a typed value instead
// of on prose that any consumer was free to re-parse its own way.
type ProcessFailureCause string

const (
	// CauseUnknown means the detail did not match any known failure.
	CauseUnknown ProcessFailureCause = ""
	// CauseTranscodeStalled: the transcode produced no progress and was stopped.
	CauseTranscodeStalled ProcessFailureCause = "transcode_stalled"
	// CauseBlackOutput: the runtime path produced a black picture.
	CauseBlackOutput ProcessFailureCause = "black_output"
	// CauseCopyOutputMissingCodec: stream copy lacked usable codec parameters.
	CauseCopyOutputMissingCodec ProcessFailureCause = "copy_output_missing_codec"
	// CauseUpstreamEnded: the upstream stopped before the session was up.
	CauseUpstreamEnded ProcessFailureCause = "upstream_ended"
	// CauseUpstreamOpenFailed: the upstream could not be opened at all.
	CauseUpstreamOpenFailed ProcessFailureCause = "upstream_open_failed"
	// CauseUpstreamIOError: the upstream failed with an I/O error while reading.
	CauseUpstreamIOError ProcessFailureCause = "upstream_io_error"
	// CauseInvalidUpstreamInput: the upstream delivered undecodable data.
	CauseInvalidUpstreamInput ProcessFailureCause = "invalid_upstream_input"
	// CauseUpstreamScrambled: the upstream is encrypted and was not descrambled.
	CauseUpstreamScrambled ProcessFailureCause = "upstream_scrambled"
	// CauseEncoderCrashed: the media process died on a fault signal.
	CauseEncoderCrashed ProcessFailureCause = "encoder_crashed"
	// CauseProcessExited: the process ended without a more specific diagnosis.
	CauseProcessExited ProcessFailureCause = "process_exited"
)

// processFailureTable is the single mapping between causes and their canonical
// text, in match order: the longest and most specific detail first, so a detail
// that embeds another (a startup wrapper around a stall, say) still classifies as
// the inner failure.
var processFailureTable = []struct {
	Cause ProcessFailureCause
	Text  string
}{
	{CauseTranscodeStalled, DetailTranscodeStalled},
	{CauseBlackOutput, DetailBlackOutput},
	{CauseCopyOutputMissingCodec, DetailCopyOutputMissingCodec},
	{CauseUpstreamEnded, DetailUpstreamEndedPrematurely},
	{CauseUpstreamOpenFailed, DetailUpstreamOpenFailed},
	{CauseUpstreamIOError, DetailUpstreamIOError},
	{CauseInvalidUpstreamInput, DetailInvalidUpstreamInput},
	{CauseUpstreamScrambled, DetailUpstreamScrambled},
	{CauseEncoderCrashed, DetailEncoderCrashed},
	{CauseProcessExited, DetailProcessExitedUnexpectedly},
}

// ClassifyProcessFailure is the ONLY place that reads failure prose. Callers that
// need to make a decision about a failure classify once and switch on the cause.
//
// Matching is substring-based because details are routinely wrapped by the layer
// that observed them ("process died during startup: <detail>").
func ClassifyProcessFailure(detail string) ProcessFailureCause {
	lower := strings.ToLower(strings.TrimSpace(detail))
	if lower == "" {
		return CauseUnknown
	}
	for _, entry := range processFailureTable {
		if strings.Contains(lower, strings.ToLower(entry.Text)) {
			return entry.Cause
		}
	}
	return CauseUnknown
}

// Detail returns the canonical text for a cause. For CauseEncoderCrashed this is
// the prefix without a signal name.
func (c ProcessFailureCause) Detail() string {
	for _, entry := range processFailureTable {
		if entry.Cause == c {
			return entry.Text
		}
	}
	return ""
}

// Priority ranks how informative a cause is when several failures were observed
// for the same process and only one can be reported.
//
// A crash outranks everything: if the process faulted, every other symptom
// observed around it — the stall it stopped producing progress for, the playlist
// that never appeared — is downstream of the fault.
func (c ProcessFailureCause) Priority() int {
	switch c {
	case CauseEncoderCrashed:
		return 60
	case CauseBlackOutput:
		return 55
	case CauseTranscodeStalled:
		return 50
	case CauseCopyOutputMissingCodec:
		return 45
	case CauseUpstreamEnded:
		return 40
	case CauseUpstreamScrambled:
		return 35
	case CauseUpstreamOpenFailed, CauseUpstreamIOError:
		return 30
	case CauseInvalidUpstreamInput:
		return 20
	case CauseProcessExited:
		return 10
	default:
		return 0
	}
}

// RecoverableByProfileChange reports whether restarting on a lighter profile can
// plausibly succeed where this failure did not.
//
// True only for failures of the ENCODE path: a transcode that never progresses,
// and a process that faulted on the code path this profile selected. Input
// failures are deliberately excluded — no profile repairs a scrambled or
// truncated upstream, and pretending otherwise buys a full restart that ends the
// same way, which is how a scrambled channel once cost 52 seconds and reported
// the wrong reason.
func (c ProcessFailureCause) RecoverableByProfileChange() bool {
	switch c {
	case CauseTranscodeStalled, CauseEncoderCrashed:
		return true
	default:
		return false
	}
}

// RetriableAsIs reports whether repeating the SAME profile is worth one attempt.
//
// True for transient upstream conditions: a tuner that had not settled, a relay
// that dropped the first connection. The second attempt reads an already-tuned
// receiver, so it can legitimately succeed where the first did not.
func (c ProcessFailureCause) RetriableAsIs() bool {
	switch c {
	case CauseUpstreamEnded, CauseUpstreamOpenFailed, CauseInvalidUpstreamInput:
		return true
	default:
		return false
	}
}
