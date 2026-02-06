package decision

import (
	"sort"

	"github.com/ManuGH/xg2g/internal/core/normalize"
)

// NormalizeInput produces a semantically equivalent DecisionInput with all
// fields normalized for deterministic comparison. This is the TRUE oracle
// for breaking Engine/Model coupling (R3-1).
//
// Normalization rules:
// 1. Strings: robustNorm (Trim Unicode Space/Invisible + ToLower)
// 2. Slices: nil -> empty, dedupe, sort, normalize elements
// 3. SupportsRange: nil treated as *false equivalent (engine semantics)
// 4. RequestID: excluded from semantic comparison (kept as-is for tracing)
func NormalizeInput(in DecisionInput) DecisionInput {
	return DecisionInput{
		Source: Source{
			Container:   robustNorm(in.Source.Container),
			VideoCodec:  robustNorm(in.Source.VideoCodec),
			AudioCodec:  robustNorm(in.Source.AudioCodec),
			BitrateKbps: in.Source.BitrateKbps,
			Width:       in.Source.Width,
			Height:      in.Source.Height,
			FPS:         in.Source.FPS,
		},
		Capabilities: Capabilities{
			Version:       in.Capabilities.Version,
			Containers:    normSlice(in.Capabilities.Containers),
			VideoCodecs:   normSlice(in.Capabilities.VideoCodecs),
			AudioCodecs:   normSlice(in.Capabilities.AudioCodecs),
			SupportsHLS:   in.Capabilities.SupportsHLS,
			SupportsRange: normBoolPtr(in.Capabilities.SupportsRange),
			MaxVideo:      in.Capabilities.MaxVideo,
			DeviceType:    robustNorm(in.Capabilities.DeviceType),
		},
		Policy: Policy{
			AllowTranscode: in.Policy.AllowTranscode,
		},
		APIVersion: robustNorm(in.APIVersion), // Normalize for validation + hashing
		RequestID:  in.RequestID,              // Keep as-is (tracing only)
	}
}

// robustNorm normalizes a string: trim Unicode whitespace + invisible characters + lowercase.
func robustNorm(s string) string {
	return normalize.Token(s)
}

// normSlice normalizes a string slice: nil->empty, dedupe, sort, normalize elements.
func normSlice(in []string) []string {
	if len(in) == 0 {
		return []string{} // Explicit empty, not nil
	}

	// Normalize + dedupe
	seen := make(map[string]bool)
	out := make([]string, 0, len(in))
	for _, s := range in {
		norm := robustNorm(s)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}

	sort.Strings(out)
	return out
}

// normBoolPtr normalizes *bool: nil -> *false (engine treats nil as false).
func normBoolPtr(b *bool) *bool {
	if b == nil {
		f := false
		return &f
	}
	return b
}
