package playbackplanner

import "errors"

// ErrRuleNotImplemented is returned when the planner has no valid rules for the evidence.
var ErrRuleNotImplemented = errors.New("no planner rules implemented for this scenario")

// PlanningResult encapsulates the outcome of the planning process.
type PlanningResult struct {
	Plan  PlaybackPlan
	Trace PlanTrace
}

// Plan takes PlaybackEvidence and produces a PlanningResult.
// It is a pure, side-effect-free function. Deny is a valid plan, not an error.
// Errors are only returned if the evidence itself is invalid/malformed or if no rules apply.
func Plan(ev PlaybackEvidence) (PlanningResult, error) {
	if !hasValidEvidence(ev) {
		return PlanningResult{}, errors.New("invalid or incomplete evidence")
	}

	trace := PlanTrace{
		PlannerVersion: "v4", // or whatever we use
		Log:            []RuleHit{},
	}

	plan := PlaybackPlan{
		Outcome: "deny",
		Mode:    "none", // Sentinel
	}

	// Helper for structured logging
	logHit := func(rule, result, reason string) {
		trace.Log = append(trace.Log, RuleHit{Rule: rule, Result: result, Reason: reason})
	}

	if !isSourceTruthFresh(ev) {
		logHit("freshness_gate", "fail", "stale_or_partial_truth")
		// Based on legacy characterization, stale live truth drops to remux/transcode or denied if strict.
		// For now, if we don't have fresh evidence, we can't definitively direct_play.
		// Let's mark it and continue, but flag that direct_play is blocked.
	} else {
		logHit("freshness_gate", "pass", "truth_is_fresh")
	}

	// 1. Direct Play (Copy + Direct engine)
	canDirectPlay := true
	if ev.Scope != "recording" && ev.Scope != "media" {
		logHit("direct_play_gate", "fail", "scope_not_seekable")
		canDirectPlay = false
	} else if !supportsRange(ev) {
		logHit("direct_play_gate", "fail", "client_lacks_range_support")
		canDirectPlay = false
	} else if !isContainerCompatible(ev) {
		logHit("direct_play_gate", "fail", "container_incompatible")
		canDirectPlay = false
	} else if !isVideoCodecCompatible(ev) || !isAudioCodecCompatible(ev) {
		logHit("direct_play_gate", "fail", "codec_incompatible")
		canDirectPlay = false
	} else if requiresInterlaceRepair(ev) {
		logHit("direct_play_gate", "fail", "interlace_repair_required")
		canDirectPlay = false
	} else if exceedsMaxVideoLimits(ev) {
		logHit("direct_play_gate", "fail", "exceeds_client_limits")
		canDirectPlay = false
	}

	if canDirectPlay {
		logHit("mode_decision", "allow", "direct_play_selected")
		plan.Outcome = "allow"
		plan.Mode = "copy"
		plan.DeliveryEngine = "direct"
		resolveMediaTargets(&plan, ev)
		return PlanningResult{Plan: plan, Trace: trace}, nil
	}

	// 2. Direct Stream / Remux
	canRemux := true
	if !supportsHLS(ev) {
		logHit("remux_gate", "fail", "client_lacks_hls_support")
		canRemux = false
	} else if !isVideoCodecCompatible(ev) || !isAudioCodecCompatible(ev) {
		logHit("remux_gate", "fail", "codec_incompatible")
		canRemux = false
	} else if requiresInterlaceRepair(ev) {
		logHit("remux_gate", "fail", "interlace_repair_required")
		canRemux = false
	} else if exceedsMaxVideoLimits(ev) {
		logHit("remux_gate", "fail", "exceeds_client_limits")
		canRemux = false
	}

	if canRemux {
		logHit("mode_decision", "allow", "remux_selected")
		plan.Outcome = "allow"
		plan.Mode = "remux"
		plan.DeliveryEngine = "hls"
		resolveMediaTargets(&plan, ev)
		return PlanningResult{Plan: plan, Trace: trace}, nil
	}

	// 3. Transcode
	canTranscode := true
	if ev.OperatorPolicy.DisableTranscoding {
		logHit("transcode_gate", "fail", "operator_disabled_transcoding")
		canTranscode = false
	} else if !ev.ClientEvidence.AllowTranscode {
		logHit("transcode_gate", "fail", "client_rejected_transcoding")
		canTranscode = false
	}

	if canTranscode {
		logHit("mode_decision", "allow", "transcode_selected")
		plan.Outcome = "allow"
		plan.Mode = "transcode"
		plan.DeliveryEngine = "hls"
		resolveMediaTargets(&plan, ev)
		return PlanningResult{Plan: plan, Trace: trace}, nil
	}

	// 4. Deny
	logHit("mode_decision", "deny", "no_compatible_mode_available")
	plan.Outcome = "deny"
	plan.Mode = "none"

	return PlanningResult{Plan: plan, Trace: trace}, nil
}
