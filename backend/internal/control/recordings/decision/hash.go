package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// ComputeHash calculates a stable SHA-256 hash of the DecisionInput.
// ADR-009.2: Hash is SEMANTIC - excludes RequestID, normalizes nil/false equivalence.
// This is critical for caching, dedup, and drift detection.
func (i DecisionInput) ComputeHash() string {
	b, _ := i.CanonicalJSONForHash()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalJSONForHash returns the semantic canonical JSON (excludes RequestID).
// Use for hash computation and semantic comparison.
func (i DecisionInput) CanonicalJSONForHash() ([]byte, error) {
	// ADR-009.2: Normalize SupportsRange nil -> false
	supportsRange := false
	if i.Capabilities.SupportsRange != nil {
		supportsRange = *i.Capabilities.SupportsRange
	}

	c := canonicalInput{
		Source: canonicalSource{
			Container:   robustNorm(i.Source.Container),
			VideoCodec:  robustNorm(i.Source.VideoCodec),
			AudioCodec:  robustNorm(i.Source.AudioCodec),
			BitrateKbps: i.Source.BitrateKbps,
			Width:       i.Source.Width,
			Height:      i.Source.Height,
			FPS:         i.Source.FPS,
		},
		Capabilities: canonicalCapabilities{
			Version:       i.Capabilities.Version,
			Containers:    sortedUnique(i.Capabilities.Containers),
			VideoCodecs:   sortedUnique(i.Capabilities.VideoCodecs),
			AudioCodecs:   sortedUnique(i.Capabilities.AudioCodecs),
			SupportsHLS:   i.Capabilities.SupportsHLS,
			SupportsRange: &supportsRange, // Always *bool, never nil
			MaxVideo:      i.Capabilities.MaxVideo,
			DeviceType:    robustNorm(i.Capabilities.DeviceType),
		},
		Policy: canonicalPolicy{
			AllowTranscode: i.Policy.AllowTranscode,
		},
		APIVersion: robustNorm(i.APIVersion),
		// RequestID explicitly EXCLUDED (ADR-009.2)
	}

	return json.Marshal(c)
}

// CanonicalJSON returns the full canonical JSON including RequestID.
// Use for failure artifacts and replay (not for hash).
func (i DecisionInput) CanonicalJSON() ([]byte, error) {
	supportsRange := false
	if i.Capabilities.SupportsRange != nil {
		supportsRange = *i.Capabilities.SupportsRange
	}

	c := canonicalInputFull{
		Source: canonicalSource{
			Container:   robustNorm(i.Source.Container),
			VideoCodec:  robustNorm(i.Source.VideoCodec),
			AudioCodec:  robustNorm(i.Source.AudioCodec),
			BitrateKbps: i.Source.BitrateKbps,
			Width:       i.Source.Width,
			Height:      i.Source.Height,
			FPS:         i.Source.FPS,
		},
		Capabilities: canonicalCapabilities{
			Version:       i.Capabilities.Version,
			Containers:    sortedUnique(i.Capabilities.Containers),
			VideoCodecs:   sortedUnique(i.Capabilities.VideoCodecs),
			AudioCodecs:   sortedUnique(i.Capabilities.AudioCodecs),
			SupportsHLS:   i.Capabilities.SupportsHLS,
			SupportsRange: &supportsRange,
			MaxVideo:      i.Capabilities.MaxVideo,
			DeviceType:    robustNorm(i.Capabilities.DeviceType),
		},
		Policy: canonicalPolicy{
			AllowTranscode: i.Policy.AllowTranscode,
		},
		APIVersion: robustNorm(i.APIVersion),
		RequestID:  i.RequestID, // Included for replay artifacts
	}

	return json.Marshal(c)
}

// canonicalInputFull includes RequestID (for replay artifacts)
type canonicalInputFull struct {
	Source       canonicalSource       `json:"source"`
	Capabilities canonicalCapabilities `json:"caps"`
	Policy       canonicalPolicy       `json:"policy"`
	APIVersion   string                `json:"api"`
	RequestID    string                `json:"rid,omitempty"`
}

// robustNormHash is just an alias to remind that normalization is semantic.
// (Already implemented in normalize.go as robustNorm)

// sortedUnique returns a sorted, deduplicated slice.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	// Copy to avoid mutating input
	out := make([]string, len(in))
	copy(out, in)

	// Normalize first
	for i := range out {
		out[i] = robustNorm(out[i])
	}

	sort.Strings(out)

	// Dedupe
	j := 0
	for i := 1; i < len(out); i++ {
		if out[j] == out[i] {
			continue
		}
		j++
		out[j] = out[i]
	}
	return out[:j+1]
}

// Canonical Structs (Private, strict order)
type canonicalInput struct {
	Source       canonicalSource       `json:"source"`
	Capabilities canonicalCapabilities `json:"caps"`
	Policy       canonicalPolicy       `json:"policy"`
	APIVersion   string                `json:"api"`
}

type canonicalSource struct {
	Container   string  `json:"c"`
	VideoCodec  string  `json:"v"`
	AudioCodec  string  `json:"a"`
	BitrateKbps int     `json:"br"`
	Width       int     `json:"w"`
	Height      int     `json:"h"`
	FPS         float64 `json:"fps"`
}

type canonicalCapabilities struct {
	Version       int                 `json:"v"`
	Containers    []string            `json:"c"`
	VideoCodecs   []string            `json:"vc"`
	AudioCodecs   []string            `json:"ac"`
	SupportsHLS   bool                `json:"hls"`
	SupportsRange *bool               `json:"rng,omitempty"`
	MaxVideo      *MaxVideoDimensions `json:"mv,omitempty"`
	DeviceType    string              `json:"dev"`
}

type canonicalPolicy struct {
	AllowTranscode bool `json:"tx"`
}
