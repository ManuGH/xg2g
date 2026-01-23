package decision

import "fmt"

// ErrInvariantViolation indicates a semantic breach of the decision contract.
// This is a 500 Internal Error (Stop-the-line), not a 4xx.
type ErrInvariantViolation struct {
	Invariant string
	Detail    string
}

func (e ErrInvariantViolation) Error() string {
	return fmt.Sprintf("invariant %s violation: %s", e.Invariant, e.Detail)
}

// validateOutputInvariants enforces P8 Output Invariants (#9-#11).
// It runs AFTER normalization.
func validateOutputInvariants(dec *Decision, input DecisionInput) error {
	switch dec.Mode {
	case ModeDirectPlay:
		// Invariant #9: Direct Play Eligibility
		// - Protocol must be "mp4" (Kind="file")
		// - SupportsRange CAPABILITY must be explicitly true
		// - Container must be in {mp4, mov, m4v}
		// - Codecs must be supported by client

		if dec.SelectedOutputKind != "file" {
			return ErrInvariantViolation{Invariant: "#9", Detail: fmt.Sprintf("direct_play requires kind='file', got '%s'", dec.SelectedOutputKind)}
		}
		if input.Capabilities.SupportsRange == nil || !*input.Capabilities.SupportsRange {
			return ErrInvariantViolation{Invariant: "#9", Detail: "direct_play requires strict range support (SupportsRange=true)"}
		}

		// Container/Codec safety Check (Redundant to predicates but vital for Invariant)
		if !isMP4Container(input.Source.Container) {
			return ErrInvariantViolation{Invariant: "#9", Detail: fmt.Sprintf("direct_play requires mp4/mov container, got '%s'", input.Source.Container)}
		}

		// Note: We don't re-implement full codec matching logic here (complexity/drift risk),
		// but we rely on the fact that `ModeDirectPlay` implies predicates passed.
		// However, strict invariant technically says "Video+Audio codec supported".
		// To be truly robust against Logic bugs, we should check simple containment if lists are available.
		// But for now, Container check + Range + Mode consistency is a strong start.
		// User requirement: "Container ∈ {mp4, mov} • Video+Audio codec supported"
		// Checking Codec support strictly requires iterating Capabilities.
		// Let's implement basic containment check helper to be safe.
		if !contains(input.Capabilities.VideoCodecs, input.Source.VideoCodec) {
			return ErrInvariantViolation{Invariant: "#9", Detail: fmt.Sprintf("direct_play video codec '%s' not not in client caps", input.Source.VideoCodec)}
		}
		if !contains(input.Capabilities.AudioCodecs, input.Source.AudioCodec) {
			return ErrInvariantViolation{Invariant: "#9", Detail: fmt.Sprintf("direct_play audio codec '%s' not not in client caps", input.Source.AudioCodec)}
		}

	case ModeTranscode:
		// Invariant #10: Transcode Protocol
		// - Protocol must be "hls" (Kind="hls")
		// - NEVER "file"

		if dec.SelectedOutputKind != "hls" {
			return ErrInvariantViolation{Invariant: "#10", Detail: fmt.Sprintf("transcode requires kind='hls', got '%s'", dec.SelectedOutputKind)}
		}

	case ModeDeny:
		// Invariant #11: Deny Hygiene
		// - Outputs must be empty/cleared
		// - URL/Kind must be empty (Strict Option A: "")

		if dec.SelectedOutputURL != "" {
			return ErrInvariantViolation{Invariant: "#11", Detail: "deny mode must have empty output URL"}
		}
		if dec.SelectedOutputKind != "" {
			return ErrInvariantViolation{Invariant: "#11", Detail: fmt.Sprintf("deny mode must have empty output kind, got '%s'", dec.SelectedOutputKind)}
		}
		if len(dec.Outputs) > 0 {
			return ErrInvariantViolation{Invariant: "#11", Detail: "deny mode must have zero outputs"}
		}
	}

	return nil
}

// normalizeDecision ensures safe defaults before validation.
func normalizeDecision(dec *Decision) {
	if dec.Mode == ModeDeny {
		// P8-3 Rule: Deny forces clear outputs.
		dec.SelectedOutputURL = ""
		dec.SelectedOutputKind = "" // Option A: Strict Empty
		dec.Outputs = []Output{}    // Empty slice, not nil? User said "empty or nil". Type safe empty slice is cleaner.
	}
}

// Helpers for Invariants
func isMP4Container(c string) bool {
	return c == "mp4" || c == "mov" || c == "m4v"
}

// contains is already defined in predicates.go
