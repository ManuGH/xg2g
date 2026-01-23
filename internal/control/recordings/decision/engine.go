package decision

import "sort"

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

	// Collect incompatibility reasons (Step 1-3)
	// These are accumulated to explain why higher modes failed.
	if !pred.CanContainer {
		reasons = append(reasons, ReasonContainerNotSupported)
		// Rule-Container evaluated (Hit means exclusion or check)
		// ADR implies "Rule Hits" is the path taken.
		// Use standard names: "rule_container", "rule_video", "rule_audio"
	} else {
		// Passed
	}

	// Simplify: Always record rules evaluated in order?
	// Or only "Hit" rules that triggered specific logic?
	// User said: "Ordered list of rules evaluated/hit".
	// Let's log *evaluations* that had normative impact.

	rules = append(rules, "rule_container")
	if !pred.CanContainer {
		return ModeDeny, []ReasonCode{ReasonContainerNotSupported}, rules
	}

	rules = append(rules, "rule_video")
	rules = append(rules, "rule_audio")

	if !pred.CanVideo {
		reasons = append(reasons, ReasonVideoCodecNotSupported)
	}
	if !pred.CanAudio {
		reasons = append(reasons, ReasonAudioCodecNotSupported)
	}

	// If any codec mismatch...
	if !pred.CanVideo || !pred.CanAudio {
		rules = append(rules, "rule_transcode") // Implicit checkout
		// Just logic:
		// Logic is: !Video -> Check Transcode.
		if policy.AllowTranscode {
			return ModeTranscode, reasons, append(rules, "rule_transcode_allowed")
		}
		// If here, either !AllowTranscode OR (logic fallthrough).
		// Wait, structure in my previous rewrite was:
		// Collect reasons -> Check DP -> Check DS -> Check Transcode -> Deny.
		// Trace should reflect *that* structure.
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
		reasons = append(reasons, ReasonPolicyDeniesTranscode)
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

	// Sort reasons for determinism (ADR-009)
	sort.Sort(ReasonCodeSlice(reasons))

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
