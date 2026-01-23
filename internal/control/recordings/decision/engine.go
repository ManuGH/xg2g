package decision

import (
	"context"
)

// Decide is the pure decision engine entry point.
// Returns (httpStatus, decision, problem). Exactly one of decision/problem is non-nil.
// R5-A: Accept schemaType for server-side telemetry.
func Decide(ctx context.Context, input DecisionInput, schemaType string) (int, *Decision, *Problem) {
	// P8-1: Normalization (CTO-Grade Robustness)
	// (Input is already normalized by DecodeDecisionInput, but we keep it here for defense-in-depth)
	input = NormalizeInput(input)

	// Start Decision Span (Correction 2: Owned by Decide)
	ctx, span := StartDecisionSpan(ctx)
	defer span.End()

	// Phase 1: Input validation (fail-closed)
	if prob := validateInput(input); prob != nil {
		// Observability (Phase 6a: Input Failure)
		EmitDecisionObs(ctx, input, nil, prob, schemaType)
		return prob.Status, nil, prob
	}

	// Phase 2: Compute compatibility predicates
	pred := computePredicates(input.Source, input.Capabilities, input.Policy)

	// Phase 3: Decision table evaluation (first match wins)
	// (Returns Mode and ReasonCodes per ADR-P8)
	mode, reasons, rules := evaluateDecision(pred, input.Capabilities, input.Policy)

	// Phase 4: Build decision response
	decision := buildDecision(mode, pred, input, reasons, rules)

	// Phase 5: Output Invariants Enforcement (P8-3)
	// Stop-the-line: Normalize and validate to prevent semantic lies.
	normalizeDecision(decision)
	if err := validateOutputInvariants(decision, input); err != nil {
		prob := &Problem{
			Type:   "recordings/invariant-violation",
			Title:  "Invariant Violation",
			Status: 500,
			Code:   string(ProblemInvariantViolation),
			Detail: err.Error(),
		}
		// Observability (Phase 6b: Invariant Violation)
		EmitDecisionObs(ctx, input, nil, prob, schemaType)
		return 500, nil, prob
	}

	// Phase 6: Observability (Success) (P4 Observability)
	// Populate Trace with Hash and Rules
	// (Note: rule/why logic belongs in engine, but Hash is pure input)
	decision.Trace.InputHash = input.ComputeHash()

	// Add telemetry (R5-A Condition 3: Server-side only)
	EmitDecisionObs(ctx, input, decision, nil, schemaType)

	return 200, decision, nil
}

const (
	// Sentinel value for deny mode (ADR P4-2 requirement).
	sentinelNone = "none"
)

// evaluateDecisionTable implements the normative decision table (Section 6.3).
// Evaluates in order D-1 through D-5; first match wins.
// evaluateDecision implements the strict logic from ADR-P8.
// Returns Mode and a list of normative ReasonCodes.
func evaluateDecision(pred Predicates, caps Capabilities, policy Policy) (Mode, []ReasonCode, []string) {
	var reasons []ReasonCode
	var rules []string

	// ADR-009.1 §3: Container mismatch blocks only DirectPlay, not DirectStream/Transcode.
	// Record reason for observability, but DO NOT return early.
	rules = append(rules, "rule_container")
	if !pred.CanContainer {
		reasons = append(reasons, ReasonContainerNotSupported)
		// Flow continues to DP/DS/Transcode checks.
	}

	rules = append(rules, "rule_video")
	rules = append(rules, "rule_audio")

	if !pred.CanVideo {
		reasons = append(reasons, ReasonVideoCodecNotSupported)
	}
	if !pred.CanAudio {
		reasons = append(reasons, ReasonAudioCodecNotSupported)
	}
	if !caps.SupportsHLS {
		reasons = append(reasons, ReasonHLSNotSupported)
	}

	// If any codec mismatch...
	if !pred.CanVideo || !pred.CanAudio {
		rules = append(rules, "rule_transcode") // Implicit checkout
		// Just logic:
		// Logic is: !Video -> Check Transcode.
		if pred.TranscodePossible {
			return ModeTranscode, reasons, append(rules, "rule_transcode_allowed")
		}
		if !policy.AllowTranscode {
			reasons = append(reasons, ReasonPolicyDeniesTranscode)
		}
		return ModeDeny, reasons, rules
	}

	// Step 4: DirectPlay
	rules = append(rules, "rule_directplay")
	if pred.DirectPlayPossible {
		return ModeDirectPlay, []ReasonCode{ReasonDirectPlayMatch}, rules
	}

	// Step 5: DirectStream
	rules = append(rules, "rule_directstream")
	if pred.DirectStreamPossible {
		return ModeDirectStream, []ReasonCode{ReasonDirectStreamMatch}, rules
	}

	// Step 6: Transcode
	rules = append(rules, "rule_transcode")
	if pred.TranscodeNeeded && pred.TranscodePossible {
		return ModeTranscode, reasons, rules
	}

	// Step 7: Deny
	if pred.TranscodeNeeded && !pred.TranscodePossible {
		if !policy.AllowTranscode {
			reasons = append(reasons, ReasonPolicyDeniesTranscode)
		}
		return ModeDeny, reasons, rules
	}

	// Fallback
	if len(reasons) == 0 {
		reasons = append(reasons, ReasonNoCompatiblePlaybackPath)
	}
	return ModeDeny, reasons, rules
}

func reasonsToRuleHits(r string) string { return r } // Dummy helper if needed

// buildDecision constructs the final Decision response.
func buildDecision(mode Mode, pred Predicates, input DecisionInput, reasons []ReasonCode, rules []string) *Decision {
	outputs := buildOutputs(mode, input.Source)

	var selURL, selKind string
	if len(outputs) > 0 {
		selURL = outputs[0].URL
		selKind = outputs[0].Kind
	}

	// Sort reasons by priority for deterministic ordering.
	sortReasonsByPriority(reasons)

	// Construct Trace details
	// Phase 1: Simple mapping of Reasons -> Trace.Why
	// In Phase 2, this will include specific constraints (want/got).
	why := make([]Reason, len(reasons))
	for i, r := range reasons {
		why[i] = Reason{Code: r}
	}

	decision := &Decision{
		Mode:        mode,
		Selected:    buildSelected(mode, input.Source),
		Outputs:     outputs,
		Constraints: []string{}, // Always empty array (no constraints in P4-2)
		Reasons:     reasons,
		Trace: Trace{
			RequestID: input.RequestID,
			RuleHits:  rules,
			Why:       why,
		},
		SelectedOutputURL:  selURL,
		SelectedOutputKind: selKind,
	}

	return decision
}

// buildSelected constructs the selected formats.
// For mode=deny, MUST use sentinel "none" (not null).
func buildSelected(mode Mode, source Source) SelectedFormats {
	if mode == ModeDeny {
		return SelectedFormats{
			Container:  sentinelNone,
			VideoCodec: sentinelNone,
			AudioCodec: sentinelNone,
		}
	}

	// For all other modes, use actual source formats
	return SelectedFormats{
		Container:  source.Container,
		VideoCodec: source.VideoCodec,
		AudioCodec: source.AudioCodec,
	}
}

// buildOutputs constructs the outputs array.
// For mode=deny, MUST be empty array.
func buildOutputs(mode Mode, source Source) []Output {
	if mode == ModeDeny {
		return []Output{} // Empty array for deny
	}

	// For P4-2, we return placeholder outputs
	// (actual URL construction is out of scope for pure engine)
	switch mode {
	case ModeDirectPlay:
		return []Output{
			{Kind: "file", URL: "placeholder://direct-play"},
		}
	case ModeDirectStream:
		return []Output{
			{Kind: "hls", URL: "placeholder://direct-stream.m3u8"},
		}
	case ModeTranscode:
		return []Output{
			{Kind: "hls", URL: "placeholder://transcode.m3u8"},
		}
	default:
		return []Output{}
	}
}
