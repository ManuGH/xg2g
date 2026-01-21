package capabilities

import (
	"sort"
	"strings"
)

// PlaybackCapabilities represents the core capability set for playback decisions.
// This struct is intended to be the domain truth, mapped to/from OpenAPI or shims.
type PlaybackCapabilities struct {
	CapabilitiesVersion int      `json:"capabilitiesVersion"`
	Containers          []string `json:"containers"`
	VideoCodecs         []string `json:"videoCodecs"`
	AudioCodecs         []string `json:"audioCodecs"`
	SupportsHLS         bool     `json:"supportsHls"`

	// DeviceType is optional but helpful for identity-bound profiles
	DeviceType string `json:"deviceType,omitempty"`

	// Allowed constraints ONLY (per ADR P7):
	AllowTranscode *bool     `json:"allowTranscode,omitempty"`
	MaxVideo       *MaxVideo `json:"maxVideo,omitempty"`
	SupportsRange  *bool     `json:"supportsRange,omitempty"`
}

type MaxVideo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Fps    int `json:"fps"`
}

// CanonicalizeCapabilities normalizes a capabilities struct to a deterministic form.
// Normative rules:
// - nil slices => empty slices
// - trim + lower tokens
// - dedupe + sort
// - no empty tokens
func CanonicalizeCapabilities(in PlaybackCapabilities) PlaybackCapabilities {
	out := in

	out.Containers = canonicalStringSet(out.Containers)
	out.VideoCodecs = canonicalStringSet(out.VideoCodecs)
	out.AudioCodecs = canonicalStringSet(out.AudioCodecs)

	// Ensure non-nil slices for stable JSON + struct equality
	if out.Containers == nil {
		out.Containers = []string{}
	}
	if out.VideoCodecs == nil {
		out.VideoCodecs = []string{}
	}
	if out.AudioCodecs == nil {
		out.AudioCodecs = []string{}
	}

	return out
}

func canonicalStringSet(in []string) []string {
	if in == nil {
		return []string{}
	}
	m := make(map[string]struct{}, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		t = strings.ToLower(t)
		m[t] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
