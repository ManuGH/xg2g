package capabilities

import (
	"sort"
	"strings"
)

// PlaybackCapabilities represents the core capability set for playback decisions.
// This struct is intended to be the domain truth, mapped to/from OpenAPI or shims.
type PlaybackCapabilities struct {
	CapabilitiesVersion int                `json:"capabilitiesVersion"`
	Containers          []string           `json:"containers"`
	VideoCodecs         []string           `json:"videoCodecs"`
	VideoCodecSignals   []VideoCodecSignal `json:"videoCodecSignals,omitempty"`
	AudioCodecs         []string           `json:"audioCodecs"`
	SupportsHLS         bool               `json:"supportsHls"`
	SupportsHLSExplicit bool               `json:"supportsHlsExplicit,omitempty"`

	// DeviceType is optional but helpful for identity-bound profiles
	DeviceType           string          `json:"deviceType,omitempty"`
	DeviceContext        *DeviceContext  `json:"deviceContext,omitempty"`
	NetworkContext       *NetworkContext `json:"networkContext,omitempty"`
	HLSEngines           []string        `json:"hlsEngines,omitempty"`
	PreferredHLSEngine   string          `json:"preferredHlsEngine,omitempty"`
	RuntimeProbeUsed     bool            `json:"runtimeProbeUsed,omitempty"`
	RuntimeProbeVersion  int             `json:"runtimeProbeVersion,omitempty"`
	ClientFamilyFallback string          `json:"clientFamilyFallback,omitempty"`
	ClientCapsSource     string          `json:"clientCapsSource,omitempty"`

	// Allowed constraints ONLY (per ADR P7):
	AllowTranscode *bool     `json:"allowTranscode,omitempty"`
	MaxVideo       *MaxVideo `json:"maxVideo,omitempty"`
	SupportsRange  *bool     `json:"supportsRange,omitempty"`
}

type VideoCodecSignal struct {
	Codec          string `json:"codec"`
	Supported      bool   `json:"supported"`
	Smooth         *bool  `json:"smooth,omitempty"`
	PowerEfficient *bool  `json:"powerEfficient,omitempty"`
}

type MaxVideo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Fps    int `json:"fps"`
}

type DeviceContext struct {
	Brand        string `json:"brand,omitempty"`
	Product      string `json:"product,omitempty"`
	Device       string `json:"device,omitempty"`
	Platform     string `json:"platform,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	OSName       string `json:"osName,omitempty"`
	OSVersion    string `json:"osVersion,omitempty"`
	SDKInt       int    `json:"sdkInt,omitempty"`
}

type NetworkContext struct {
	Kind              string `json:"kind,omitempty"`
	DownlinkKbps      int    `json:"downlinkKbps,omitempty"`
	Metered           *bool  `json:"metered,omitempty"`
	InternetValidated *bool  `json:"internetValidated,omitempty"`
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
	out.VideoCodecSignals = canonicalVideoCodecSignals(out.VideoCodecSignals)
	out.AudioCodecs = canonicalStringSet(out.AudioCodecs)
	out.HLSEngines = canonicalStringSet(out.HLSEngines)
	out.DeviceType = strings.ToLower(strings.TrimSpace(out.DeviceType))
	out.DeviceContext = canonicalDeviceContext(out.DeviceContext)
	out.NetworkContext = canonicalNetworkContext(out.NetworkContext)
	out.PreferredHLSEngine = strings.ToLower(strings.TrimSpace(out.PreferredHLSEngine))
	out.ClientFamilyFallback = strings.ToLower(strings.TrimSpace(out.ClientFamilyFallback))
	out.ClientCapsSource = strings.ToLower(strings.TrimSpace(out.ClientCapsSource))
	if out.RuntimeProbeVersion < 0 {
		out.RuntimeProbeVersion = 0
	}

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
	if out.VideoCodecSignals == nil {
		out.VideoCodecSignals = []VideoCodecSignal{}
	}
	if out.HLSEngines == nil {
		out.HLSEngines = []string{}
	}

	return out
}

func canonicalDeviceContext(in *DeviceContext) *DeviceContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Brand = strings.ToLower(strings.TrimSpace(out.Brand))
	out.Product = strings.ToLower(strings.TrimSpace(out.Product))
	out.Device = strings.ToLower(strings.TrimSpace(out.Device))
	out.Platform = strings.ToLower(strings.TrimSpace(out.Platform))
	out.Manufacturer = strings.ToLower(strings.TrimSpace(out.Manufacturer))
	out.Model = strings.ToLower(strings.TrimSpace(out.Model))
	out.OSName = strings.ToLower(strings.TrimSpace(out.OSName))
	out.OSVersion = strings.TrimSpace(out.OSVersion)
	if out.SDKInt < 0 {
		out.SDKInt = 0
	}
	if out.Brand == "" && out.Product == "" && out.Device == "" && out.Platform == "" && out.Manufacturer == "" && out.Model == "" && out.OSName == "" && out.OSVersion == "" && out.SDKInt == 0 {
		return nil
	}
	return &out
}

func canonicalNetworkContext(in *NetworkContext) *NetworkContext {
	if in == nil {
		return nil
	}
	out := *in
	out.Kind = strings.ToLower(strings.TrimSpace(out.Kind))
	if out.DownlinkKbps < 0 {
		out.DownlinkKbps = 0
	}
	if out.Kind == "" && out.DownlinkKbps == 0 && out.Metered == nil && out.InternetValidated == nil {
		return nil
	}
	return &out
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

func canonicalVideoCodecSignals(in []VideoCodecSignal) []VideoCodecSignal {
	if in == nil {
		return []VideoCodecSignal{}
	}

	merged := make(map[string]VideoCodecSignal, len(in))
	for _, raw := range in {
		codec := strings.ToLower(strings.TrimSpace(raw.Codec))
		if codec == "" {
			continue
		}

		next := merged[codec]
		next.Codec = codec
		next.Supported = next.Supported || raw.Supported
		if raw.Smooth != nil && *raw.Smooth {
			v := true
			next.Smooth = &v
		}
		if raw.PowerEfficient != nil && *raw.PowerEfficient {
			v := true
			next.PowerEfficient = &v
		}
		merged[codec] = next
	}

	out := make([]VideoCodecSignal, 0, len(merged))
	for _, signal := range merged {
		out = append(out, signal)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Codec < out[j].Codec
	})
	return out
}
