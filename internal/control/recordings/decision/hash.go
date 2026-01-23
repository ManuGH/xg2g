package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// ComputeHash calculates a stable SHA-256 hash of the DecisionInput.
// It normalizes inputs (sorts slices, lowercase) to ensure bit-level determinism.
// This is critical for the Proof System (Prop_Determinism).
func (i DecisionInput) ComputeHash() string {
	b, _ := i.CanonicalJSON()
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalJSON returns the stable, normalized JSON representation of the input.
// Usage: This MUST be used for failure artifacts to ensure reproducibility.
func (i DecisionInput) CanonicalJSON() ([]byte, error) {
	// Create a canonical representation
	c := canonicalInput{
		Source: canonicalSource{
			Container:   norm(i.Source.Container),
			VideoCodec:  norm(i.Source.VideoCodec),
			AudioCodec:  norm(i.Source.AudioCodec),
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
			SupportsRange: i.Capabilities.SupportsRange,
			MaxVideo:      i.Capabilities.MaxVideo, // Struct is already ordered
			DeviceType:    norm(i.Capabilities.DeviceType),
		},
		Policy: canonicalPolicy{
			AllowTranscode: i.Policy.AllowTranscode,
		},
		APIVersion: i.APIVersion, // Opaque string, but we hash it
	}

	// Marshal to JSON (Go's encoder sorts map keys by default, confirming stability)
	return json.Marshal(c)
}

// norm normalizes strings (trim space + lower).
func norm(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

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
		out[i] = norm(out[i])
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
