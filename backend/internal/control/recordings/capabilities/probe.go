package capabilities

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/normalize"
)

const (
	ClientCapsSourceRuntime        = "runtime"
	ClientCapsSourceFamilyFallback = "family_fallback"
	ClientCapsSourceRuntimePlusFam = "runtime_plus_family"
)

func ResolveRuntimeProbeCapabilities(in PlaybackCapabilities) PlaybackCapabilities {
	base := in
	familyID := normalize.Token(base.ClientFamilyFallback)
	familyCaps, ok := familyFallbackCapabilities(familyID)
	if !ok {
		out := CanonicalizeCapabilities(base)
		if out.RuntimeProbeUsed {
			out.ClientCapsSource = ClientCapsSourceRuntime
		}
		return out
	}

	usedFamily := false

	if deviceType := normalize.Token(base.DeviceType); deviceType == "" || deviceType == "web" {
		base.DeviceType = familyCaps.DeviceType
		usedFamily = true
	}
	if len(base.Containers) == 0 {
		base.Containers = append([]string(nil), familyCaps.Containers...)
		usedFamily = true
	}
	if len(base.VideoCodecs) == 0 {
		base.VideoCodecs = append([]string(nil), familyCaps.VideoCodecs...)
		usedFamily = true
	}
	if len(base.AudioCodecs) == 0 {
		base.AudioCodecs = append([]string(nil), familyCaps.AudioCodecs...)
		usedFamily = true
	}
	if !base.SupportsHLSExplicit {
		base.SupportsHLS = familyCaps.SupportsHLS
		base.SupportsHLSExplicit = familyCaps.SupportsHLSExplicit
		usedFamily = true
	}
	if base.SupportsRange == nil && familyCaps.SupportsRange != nil {
		v := *familyCaps.SupportsRange
		base.SupportsRange = &v
		usedFamily = true
	}
	if base.AllowTranscode == nil && familyCaps.AllowTranscode != nil {
		v := *familyCaps.AllowTranscode
		base.AllowTranscode = &v
		usedFamily = true
	}
	if base.MaxVideo == nil && familyCaps.MaxVideo != nil {
		base.MaxVideo = &MaxVideo{
			Width:  familyCaps.MaxVideo.Width,
			Height: familyCaps.MaxVideo.Height,
			Fps:    familyCaps.MaxVideo.Fps,
		}
		usedFamily = true
	}
	if len(base.HLSEngines) == 0 && len(in.HLSEngines) == 0 {
		base.HLSEngines = append([]string(nil), familyCaps.HLSEngines...)
		usedFamily = true
	}
	if strings.TrimSpace(base.PreferredHLSEngine) == "" && strings.TrimSpace(in.PreferredHLSEngine) == "" {
		base.PreferredHLSEngine = familyCaps.PreferredHLSEngine
		usedFamily = true
	}

	out := CanonicalizeCapabilities(base)
	switch {
	case out.RuntimeProbeUsed && usedFamily:
		out.ClientCapsSource = ClientCapsSourceRuntimePlusFam
	case out.RuntimeProbeUsed:
		out.ClientCapsSource = ClientCapsSourceRuntime
	case usedFamily:
		out.ClientCapsSource = ClientCapsSourceFamilyFallback
	}
	return out
}

func familyFallbackCapabilities(familyID string) (PlaybackCapabilities, bool) {
	fixture, ok := playbackprofile.ClientFixture(familyID)
	if !ok {
		return PlaybackCapabilities{}, false
	}

	caps := PlaybackCapabilities{
		CapabilitiesVersion:  2,
		Containers:           legacyContainersFromClientFixture(fixture),
		VideoCodecs:          append([]string(nil), fixture.VideoCodecs...),
		AudioCodecs:          append([]string(nil), fixture.AudioCodecs...),
		SupportsHLS:          fixture.SupportsHLS,
		SupportsHLSExplicit:  true,
		DeviceType:           fixture.DeviceType,
		HLSEngines:           legacyHLSEnginesFromClientFixture(fixture),
		PreferredHLSEngine:   legacyPreferredHLSEngineFromClientFixture(fixture),
		ClientFamilyFallback: familyID,
	}
	if fixture.AllowTranscode != nil {
		v := *fixture.AllowTranscode
		caps.AllowTranscode = &v
	}
	supportsRange := fixture.SupportsRange
	caps.SupportsRange = &supportsRange
	if fixture.MaxVideo != nil {
		caps.MaxVideo = &MaxVideo{
			Width:  fixture.MaxVideo.Width,
			Height: fixture.MaxVideo.Height,
			Fps:    int(fixture.MaxVideo.FPS),
		}
	}
	return CanonicalizeCapabilities(caps), true
}

func legacyContainersFromClientFixture(fixture playbackprofile.ClientPlaybackProfile) []string {
	out := make([]string, 0, len(fixture.Containers))
	for _, container := range fixture.Containers {
		switch normalize.Token(container) {
		case "mpegts", "ts":
			out = append(out, "ts")
		case "mp4":
			out = append(out, "mp4")
		default:
			out = append(out, normalize.Token(container))
		}
	}
	return out
}

func legacyHLSEnginesFromClientFixture(fixture playbackprofile.ClientPlaybackProfile) []string {
	switch normalize.Token(fixture.PlaybackEngine) {
	case "native_hls":
		return []string{"native"}
	case "hls_js":
		return []string{"hlsjs"}
	default:
		return []string{}
	}
}

func legacyPreferredHLSEngineFromClientFixture(fixture playbackprofile.ClientPlaybackProfile) string {
	engines := legacyHLSEnginesFromClientFixture(fixture)
	if len(engines) == 0 {
		return ""
	}
	return engines[0]
}
