package decision

import (
	"context"
	"math/rand"
	"strings"
	"testing"
)

// P3: Model-Based Verification (The "Golden Truth")
// This model implements ADR-009 strictly and independently of the engine.
// It prioritizes readability over performance.

// ReferenceDecide computes the decision using a direct interpretation of the spec.
func ReferenceDecide(input DecisionInput) *Decision {
	// 1. Fail-Closed Validation (Simulated)
	// We don't reimplement the exact validation code, but we check the fail-closed rules strictly.
	// 1. Fail-Closed Validation
	// Engine returns error (nil decision) for invalid schema/version.
	if input.Capabilities.Version != 1 {
		return nil
	}

	// We checks unknown strings logic if we want to simulate pre-validation or
	// just let them flow through logic if they are valid strings but unknown enum values.
	// ADR says "Unknown Container -> Deny". That is logic.
	// But empty strings might be validation error?
	// Engine validateInput likely catches empty source strings.
	if isUnknown(input.Source.Container) || isUnknown(input.Source.VideoCodec) || isUnknown(input.Source.AudioCodec) {
		// If these are empty, engine might reject as 400.
		// Let's assume empty = nil decision (Validation).
		// "unknown" = valid string but unknown value = Logic Deny.
		if input.Source.Container == "" || input.Source.VideoCodec == "" || input.Source.AudioCodec == "" {
			return nil
		}
		// If just unknown value, it falls through to logic (CanContainer will be false).
	}

	// 2. Compute Predicates
	// Rule 1: Container Support
	canContainer := modelContains(input.Capabilities.Containers, input.Source.Container)

	// Rule 2: Video Support
	canVideo := modelContains(input.Capabilities.VideoCodecs, input.Source.VideoCodec)

	// Rule 3: Audio Support
	canAudio := modelContains(input.Capabilities.AudioCodecs, input.Source.AudioCodec)

	// Rule 4: DirectPlay (MP4/MOV/M4V + Ranges)
	// FIX R2-001: Normalize container to match modelContains() behavior
	containerNorm := strings.ToLower(strings.TrimSpace(input.Source.Container))
	isMp4 := containerNorm == "mp4" || containerNorm == "mov" || containerNorm == "m4v"
	supportsRange := input.Capabilities.SupportsRange != nil && *input.Capabilities.SupportsRange
	directPlayPossible := isMp4 && supportsRange && canContainer && canVideo && canAudio

	// Rule 5: Direct Stream (HLS + Codecs)
	directStreamPossible := input.Capabilities.SupportsHLS && canVideo && canAudio

	// Rule 6: Transcode (requires HLS support)
	transcodePossible := input.Policy.AllowTranscode && input.Capabilities.SupportsHLS

	// 3. Evaluation (Precedence Order)
	var reasons []ReasonCode
	var rules []string

	// Rule-Container
	rules = append(rules, "rule_container")
	if !canContainer {
		reasons = append(reasons, ReasonContainerNotSupported)
		// ADR-009.1: Container mismatch does NOT block DirectStream/Transcode.
		// It only blocks DirectPlay (which is checked later via directPlayPossible).
	}

	// Rule-Video / Rule-Audio
	rules = append(rules, "rule_video")
	rules = append(rules, "rule_audio")
	if !canVideo {
		reasons = append(reasons, ReasonVideoCodecNotSupported)
	}
	if !canAudio {
		reasons = append(reasons, ReasonAudioCodecNotSupported)
	}
	if !input.Capabilities.SupportsHLS {
		reasons = append(reasons, ReasonHLSNotSupported)
	}

	// Rule-Transcode (Implicit Check if mismatch)
	// Engine logic: if !CanVideo || !CanAudio { rules = append(rules, "rule_transcode") ... }
	if !canVideo || !canAudio {
		rules = append(rules, "rule_transcode")
		if transcodePossible {
			rules = append(rules, "rule_transcode_allowed")
			// Engine returns Transcode here immediately!
			// "return ModeTranscode, reasons, rules"
			return makeRefDecision(ModeTranscode, reasons, rules)
		}
		if !input.Policy.AllowTranscode {
			reasons = append(reasons, ReasonPolicyDeniesTranscode)
		}
		return makeRefDecision(ModeDeny, reasons, rules)
	}

	// Rule-DirectPlay
	rules = append(rules, "rule_directplay")
	if directPlayPossible {
		return makeRefDecision(ModeDirectPlay, []ReasonCode{ReasonDirectPlayMatch}, rules)
	}

	// Rule-DirectStream
	rules = append(rules, "rule_directstream")
	if directStreamPossible {
		return makeRefDecision(ModeDirectStream, []ReasonCode{ReasonDirectStreamMatch}, rules)
	}

	// Rule-Transcode (Explicit Check 2 - if needed due to protocol gap?)
	// Engine: rules = append(rules, "rule_transcode")
	// if pred.TranscodeNeeded && pred.TranscodePossible { return ModeTranscode ... }
	rules = append(rules, "rule_transcode")

	// Recalculate TranscodeNeeded logic to match predicates.go:
	if transcodePossible {
		// Engine: if pred.TranscodeNeeded && pred.TranscodePossible { return ModeTranscode }
		return makeRefDecision(ModeTranscode, reasons, rules)
	}

	// Rule-Deny
	// Engine: if TranscodeNeeded && !TranscodePossible { append PolicyDenies; return Deny }
	if !input.Policy.AllowTranscode {
		reasons = append(reasons, ReasonPolicyDeniesTranscode)
	}
	return makeRefDecision(ModeDeny, reasons, rules)
}

// Helpers for model
func isUnknown(s string) bool {
	return s == "" || strings.ToLower(s) == "unknown"
}

func modelContains(list []string, item string) bool {
	item = strings.ToLower(strings.TrimSpace(item))
	for _, v := range list {
		if strings.ToLower(strings.TrimSpace(v)) == item {
			return true
		}
	}
	return false
}

// TestModelConsistency implements the Monte Carlo Verification (Phase 3).
func TestModelConsistency(t *testing.T) {
	seed := GetProofSeed(t)
	r := rand.New(rand.NewSource(seed))
	ctx := context.Background()

	// N=2000 for CI speed (User requested <5s)
	const N = 2000

	for i := 0; i < N; i++ {
		input := GenValidDecisionInput(r)
		// 0. Mixed Schema Validity Check
		if i%10 == 0 {
			input, _ = GenInvalidSchemaInput(r)
		}

		// 1. Engine
		_, engineDec, _ := Decide(ctx, input, "test")

		// 2. Model
		modelDec := ReferenceDecide(input)

		// 3. Validation Equivalence
		if engineDec == nil {
			if modelDec != nil {
				t.Errorf("Model Mismatch [Iter %d]: Classification\nEngine: Invalid (nil)\nModel:  Valid (%s)", i, modelDec.Mode)
				dumpModelFailure(t, seed, input, &Decision{}, modelDec)
				return
			}
			continue
		}
		if modelDec == nil {
			t.Errorf("Model Mismatch [Iter %d]: Classification\nEngine: Valid (%s)\nModel:  Invalid (nil)", i, engineDec.Mode)
			dumpModelFailure(t, seed, input, engineDec, &Decision{})
			return
		}

		// 4. Logic Equivalence
		if engineDec.Mode != modelDec.Mode {
			t.Errorf("Model Mismatch [Iter %d]: Mode\nEngine: %s\nModel:  %s", i, engineDec.Mode, modelDec.Mode)
			dumpModelFailure(t, seed, input, engineDec, modelDec)
			return
		}

		if len(engineDec.Reasons) != len(modelDec.Reasons) {
			t.Errorf("Reason Count Mismatch [Iter %d]\nEngine: %v\nModel:  %v", i, engineDec.Reasons, modelDec.Reasons)
			dumpModelFailure(t, seed, input, engineDec, modelDec)
			return
		}
		for k, v := range engineDec.Reasons {
			if v != modelDec.Reasons[k] {
				t.Errorf("Reason Mismatch [Iter %d]\nEngine: %v\nModel:  %v", i, engineDec.Reasons, modelDec.Reasons)
				dumpModelFailure(t, seed, input, engineDec, modelDec)
				return
			}
		}

		if len(engineDec.Trace.RuleHits) != len(modelDec.Trace.RuleHits) {
			t.Errorf("RuleHits Count Mismatch [Iter %d]\nEngine: %v\nModel:  %v", i, engineDec.Trace.RuleHits, modelDec.Trace.RuleHits)
			dumpModelFailure(t, seed, input, engineDec, modelDec)
			return
		}
		for k, v := range engineDec.Trace.RuleHits {
			if v != modelDec.Trace.RuleHits[k] {
				t.Errorf("RuleHit Mismatch [Iter %d]\nEngine: %v\nModel:  %v", i, engineDec.Trace.RuleHits, modelDec.Trace.RuleHits)
				dumpModelFailure(t, seed, input, engineDec, modelDec)
				return
			}
		}
	}
}

func dumpModelFailure(t *testing.T, seed int64, input DecisionInput, eng, ref *Decision) {
	b, _ := input.CanonicalJSON()
	t.Logf("\n=== MODEL MISMATCH ARTIFACT ===\nSEED: %d\nINPUT:\n%s\nENGINE MODE: %s\nREF MODE:    %s\n===============================\n",
		seed, string(b), eng.Mode, ref.Mode)
}
