package autocodec

import (
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type candidate struct {
	profileID       string
	codec           string
	probeElapsed    time.Duration
	legacyTieOrder  int
	qualityPriority int
}

type SelectionTrace struct {
	Policy              string
	RequestedCodecs     string
	SelectedCodec       string
	PerformanceClass    string
	CodecBenchmarkClass string
}

const (
	iosNativeHEVCHWModeCPU        = "cpu"
	iosNativeHEVCHWModeEncodeOnly = "encode_only"
	iosNativeHEVCHWModeFull       = "full"
)

func ResolveAutoTranscodeCodecs(caps capabilities.PlaybackCapabilities) []string {
	out := make([]string, 0, 3)
	signals := caps.VideoCodecSignals
	signalFor := func(codec string) *capabilities.VideoCodecSignal {
		for i := range signals {
			if strings.EqualFold(strings.TrimSpace(signals[i].Codec), codec) {
				return &signals[i]
			}
		}
		return nil
	}

	if av1 := signalFor("av1"); av1 != nil && av1.Supported && av1.PowerEfficient != nil && *av1.PowerEfficient {
		out = append(out, "av1")
	}

	if hevc := signalFor("hevc"); hevc != nil && hevc.Supported && ((hevc.PowerEfficient != nil && *hevc.PowerEfficient) || (hevc.Smooth != nil && *hevc.Smooth)) {
		out = append(out, "hevc")
	}

	if containsCodec(caps.VideoCodecs, "h264") || len(out) == 0 {
		out = append(out, "h264")
	}

	return dedupeOrdered(out)
}

func PickProfileForCapabilities(caps capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode) string {
	return PickProfileForCapabilitiesForClientAndHost(caps, caps.ClientFamilyFallback, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func CodecForProfileID(profileID string) string {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileAV1HW:
		return "av1"
	case profiles.ProfileSafariHEVC, profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		return "hevc"
	case profiles.ProfileH264FMP4:
		return "h264"
	default:
		return ""
	}
}

func DescribeSelection(raw, profileID string, hostRuntime playbackprofile.HostRuntimeSnapshot) SelectionTrace {
	requested := strings.Join(ParseCodecList(raw), ",")
	selectedCodec := CodecForProfileID(profileID)
	if requested == "" || selectedCodec == "" {
		return SelectionTrace{}
	}
	return SelectionTrace{
		Policy:              selectionPolicy(hostRuntime),
		RequestedCodecs:     requested,
		SelectedCodec:       selectedCodec,
		PerformanceClass:    normalize.Token(hostRuntime.PerformanceClass),
		CodecBenchmarkClass: normalize.Token(playbackprofile.BenchmarkClassForCodec(hostRuntime.Benchmark, selectedCodec)),
	}
}

func PickProfileForCapabilitiesForClient(caps capabilities.PlaybackCapabilities, clientFamily string, hwaccelMode profiles.HWAccelMode) string {
	return PickProfileForCapabilitiesForClientAndHost(caps, clientFamily, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickProfileForCapabilitiesForClientAndHost(caps capabilities.PlaybackCapabilities, clientFamily string, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	codecs := ResolveAutoTranscodeCodecs(caps)
	if len(codecs) == 0 {
		return ""
	}
	return PickProfileForCodecsForClientAndHost(strings.Join(codecs, ","), clientFamily, hwaccelMode, hostRuntime)
}

func PickNativeHLSProfile(raw, clientFamily string, clientCaps *capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode) string {
	return PickNativeHLSProfileForClientAndHost(raw, clientFamily, clientCaps, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickNativeHLSProfileForClientAndHost(raw, clientFamily string, clientCaps *capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	if picked := PickNativeHLSProfileForCodecsAndHost(raw, clientFamily, hwaccelMode, hostRuntime); picked != "" {
		return picked
	}
	return PickNativeHLSProfileForCapabilitiesAndHost(clientFamily, clientCaps, hwaccelMode, hostRuntime)
}

func PickNativeHLSProfileForCodecs(raw, clientFamily string, hwaccelMode profiles.HWAccelMode) string {
	return PickNativeHLSProfileForCodecsAndHost(raw, clientFamily, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickNativeHLSProfileForCodecsAndHost(raw, clientFamily string, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	switch normalize.Token(clientFamily) {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
	default:
		return ""
	}

	switch profileID := PickProfileForCodecsForClientAndHost(raw, clientFamily, hwaccelMode, hostRuntime); profileID {
	case profiles.ProfileAV1HW, profiles.ProfileSafariHEVC, profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		return profileID
	default:
		return ""
	}
}

func PickNativeHLSProfileForCapabilities(clientFamily string, clientCaps *capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode) string {
	return PickNativeHLSProfileForCapabilitiesAndHost(clientFamily, clientCaps, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickNativeHLSProfileForCapabilitiesAndHost(clientFamily string, clientCaps *capabilities.PlaybackCapabilities, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	if hwaccelMode == profiles.HWAccelOff {
		return ""
	}

	family := normalize.Token(clientFamily)
	if family == "" && clientCaps != nil {
		family = normalize.Token(clientCaps.ClientFamilyFallback)
	}
	switch family {
	case playbackprofile.ClientSafariNative, playbackprofile.ClientIOSSafariNative:
	default:
		return ""
	}

	if clientCaps != nil {
		source := normalize.Token(clientCaps.ClientCapsSource)
		if source != capabilities.ClientCapsSourceRuntimePlusFam &&
			source != capabilities.ClientCapsSourceFamilyFallback &&
			source != capabilities.ClientCapsSourceRuntime {
			return ""
		}
	}

	candidates := make([]candidate, 0, 2)
	if clientCaps != nil &&
		playbackCapabilitiesHaveCodec(clientCaps.VideoCodecs, "av1") &&
		normalize.Token(clientCaps.ClientCapsSource) != capabilities.ClientCapsSourceFamilyFallback {
		if requiredCodec, ok := requiredVerifiedHardwareCodecForProfile(profiles.ProfileAV1HW); ok && hardware.IsHardwareEncoderReady(requiredCodec) {
			candidates = append(candidates, newCandidate(profiles.ProfileAV1HW, "av1", measuredProbeElapsedForCodec("av1"), 2))
		}
	}

	if clientCaps != nil && !playbackCapabilitiesHaveCodec(clientCaps.VideoCodecs, "hevc") {
		return pickBestCandidate(candidates, hostRuntime)
	}

	if profileID := preferredNativeHLSHEVCProfile(hwaccelMode); profileID != "" {
		candidates = append(candidates, newCandidate(profileID, "hevc", measuredProbeElapsedForCodec("hevc"), 1))
	}
	return pickBestCandidate(candidates, hostRuntime)
}

func preferredNativeHLSHEVCProfile(hwaccelMode profiles.HWAccelMode) string {
	if hwaccelMode == profiles.HWAccelOff {
		return profiles.ProfileSafariHEVC
	}
	if requiredCodec, ok := requiredVerifiedHardwareCodecForProfile(profiles.ProfileSafariHEVCHW); ok && hardware.IsHardwareEncoderReady(requiredCodec) {
		return profiles.ProfileSafariHEVCHW
	}
	return profiles.ProfileSafariHEVC
}

func PickProfileForCodecs(raw string, hwaccelMode profiles.HWAccelMode) string {
	return PickProfileForCodecsForClientAndHost(raw, "", hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickProfileForCodecsForClient(raw, clientFamily string, hwaccelMode profiles.HWAccelMode) string {
	return PickProfileForCodecsForClientAndHost(raw, clientFamily, hwaccelMode, playbackprofile.HostRuntimeSnapshot{})
}

func PickProfileForCodecsForClientAndHost(raw, clientFamily string, hwaccelMode profiles.HWAccelMode, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	return PickProfileForCodecsWithCapabilitiesAndHost(raw, hwaccelMode, map[string]hardware.HardwareEncoderCapability{
		"h264": capabilityForAutoCodec("h264"),
		"hevc": capabilityForAutoCodec("hevc"),
		"av1":  capabilityForAutoCodec("av1"),
	}, hostRuntime)
}

func ApplyClientCompatibilityProfileID(clientFamily, effectiveProfileID string) string {
	if normalize.Token(clientFamily) != playbackprofile.ClientIOSSafariNative {
		return effectiveProfileID
	}

	switch profiles.NormalizeRequestedProfileID(effectiveProfileID) {
	case profiles.ProfileSafariHEVCHW:
		if resolveIOSNativeHEVCHWMode() == iosNativeHEVCHWModeCPU {
			return profiles.ProfileSafariHEVC
		}
	default:
		return effectiveProfileID
	}
	return effectiveProfileID
}

func ApplyClientCompatibilityPolicy(
	clientFamily, effectiveProfileID string,
	profileSpec model.ProfileSpec,
	resolveProfileSpec func(string) model.ProfileSpec,
) (string, model.ProfileSpec) {
	compatibleProfileID := ApplyClientCompatibilityProfileID(clientFamily, effectiveProfileID)
	if compatibleProfileID != effectiveProfileID {
		return compatibleProfileID, resolveProfileSpec(compatibleProfileID)
	}

	if normalize.Token(clientFamily) != playbackprofile.ClientIOSSafariNative {
		return effectiveProfileID, profileSpec
	}

	if !profiles.IsFullVAAPIProfile(profileSpec.HWAccel) && profileSpec.HWAccel != "vaapi_encode_only" {
		return effectiveProfileID, profileSpec
	}

	hevcMode := resolveIOSNativeHEVCHWMode()
	switch profiles.NormalizeRequestedProfileID(effectiveProfileID) {
	case profiles.ProfileSafariHEVCHW:
		switch hevcMode {
		case iosNativeHEVCHWModeFull:
			return profiles.ProfileSafariHEVCHW, profileSpec
		case iosNativeHEVCHWModeCPU:
			return profiles.ProfileSafariHEVC, resolveProfileSpec(profiles.ProfileSafariHEVC)
		default:
			// Default iPhone/iPad native HEVC stays on GPU encode while avoiding
			// the full VAAPI decode/filter path.
			profileSpec.HWAccel = "vaapi_encode_only"
			return profiles.ProfileSafariHEVCHW, profileSpec
		}
	case profiles.ProfileSafariHEVCHWLL:
		switch hevcMode {
		case iosNativeHEVCHWModeFull:
			return profiles.ProfileSafariHEVCHWLL, profileSpec
		case iosNativeHEVCHWModeEncodeOnly:
			profileSpec.HWAccel = "vaapi_encode_only"
			return profiles.ProfileSafariHEVCHWLL, profileSpec
		default:
			// We do not have a dedicated CPU LL-HLS HEVC profile yet, so preserve
			// the LL variant and only drop the hardware request.
			profileSpec.HWAccel = ""
			return profiles.ProfileSafariHEVCHWLL, profileSpec
		}
	default:
		return effectiveProfileID, profileSpec
	}
}

func resolveIOSNativeHEVCHWMode() string {
	mode := normalize.Token(config.ParseString("XG2G_IOS_NATIVE_HEVC_HW_MODE", ""))
	switch mode {
	case iosNativeHEVCHWModeCPU, iosNativeHEVCHWModeEncodeOnly, iosNativeHEVCHWModeFull:
		return mode
	default:
		return iosNativeHEVCHWModeEncodeOnly
	}
}

func PickProfileForCodecsWithCapabilities(raw string, hwaccelMode profiles.HWAccelMode, encoderCaps map[string]hardware.HardwareEncoderCapability) string {
	return PickProfileForCodecsWithCapabilitiesAndHost(raw, hwaccelMode, encoderCaps, playbackprofile.HostRuntimeSnapshot{})
}

func PickProfileForCodecsWithCapabilitiesAndHost(raw string, hwaccelMode profiles.HWAccelMode, encoderCaps map[string]hardware.HardwareEncoderCapability, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	codecs := ParseCodecList(raw)
	if len(codecs) == 0 {
		return ""
	}

	requested := make(map[string]struct{}, len(codecs))
	for _, codec := range codecs {
		requested[codec] = struct{}{}
	}

	candidates := make([]candidate, 0, 3)
	hwAllowed := hwaccelMode != profiles.HWAccelOff

	if _, ok := requested["h264"]; ok {
		if cap, exists := capabilityForRequestedCodec(encoderCaps, "h264"); exists && cap.Verified && cap.AutoEligible && cap.ProbeElapsed > 0 {
			candidates = append(candidates, newCandidate(profiles.ProfileH264FMP4, "h264", cap.ProbeElapsed, 0))
		} else {
			candidates = append(candidates, newCandidate(profiles.ProfileH264FMP4, "h264", 24*time.Hour, 0))
		}
	}

	if hwAllowed {
		if _, ok := requested["hevc"]; ok {
			if cap, exists := capabilityForRequestedCodec(encoderCaps, "hevc"); exists && cap.Verified && cap.AutoEligible && cap.ProbeElapsed > 0 {
				candidates = append(candidates, newCandidate(profiles.ProfileSafariHEVCHW, "hevc", cap.ProbeElapsed, 1))
			}
		}
		if _, ok := requested["av1"]; ok {
			if cap, exists := capabilityForRequestedCodec(encoderCaps, "av1"); exists && cap.Verified && cap.AutoEligible && cap.ProbeElapsed > 0 {
				candidates = append(candidates, newCandidate(profiles.ProfileAV1HW, "av1", cap.ProbeElapsed, 2))
			}
		}
	}

	return pickBestCandidate(candidates, hostRuntime)
}

func newCandidate(profileID, codec string, probeElapsed time.Duration, legacyTieOrder int) candidate {
	return candidate{
		profileID:       profileID,
		codec:           normalize.Token(codec),
		probeElapsed:    probeElapsed,
		legacyTieOrder:  legacyTieOrder,
		qualityPriority: codecQualityPriority(codec),
	}
}

func pickBestCandidate(candidates []candidate, hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	if len(candidates) == 0 {
		return ""
	}

	if hostAwareAutoRankingEnabled(hostRuntime) {
		eligible := make([]candidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidateAllowedForHost(candidate, hostRuntime) {
				eligible = append(eligible, candidate)
			}
		}
		if len(eligible) == 0 {
			return ""
		}
		sort.SliceStable(eligible, func(i, j int) bool {
			iStrength := candidateHostStrength(eligible[i], hostRuntime)
			jStrength := candidateHostStrength(eligible[j], hostRuntime)
			if iStrength != jStrength {
				return iStrength > jStrength
			}
			if eligible[i].qualityPriority != eligible[j].qualityPriority {
				return eligible[i].qualityPriority > eligible[j].qualityPriority
			}
			if eligible[i].probeElapsed == eligible[j].probeElapsed {
				return eligible[i].legacyTieOrder < eligible[j].legacyTieOrder
			}
			return eligible[i].probeElapsed < eligible[j].probeElapsed
		})
		return eligible[0].profileID
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].probeElapsed == candidates[j].probeElapsed {
			return candidates[i].legacyTieOrder < candidates[j].legacyTieOrder
		}
		return candidates[i].probeElapsed < candidates[j].probeElapsed
	})
	return candidates[0].profileID
}

func hostAwareAutoRankingEnabled(hostRuntime playbackprofile.HostRuntimeSnapshot) bool {
	return normalize.Token(hostRuntime.PerformanceClass) != "" ||
		normalize.Token(hostRuntime.Benchmark.Class) != "" ||
		len(hostRuntime.Benchmark.Codecs) > 0
}

func selectionPolicy(hostRuntime playbackprofile.HostRuntimeSnapshot) string {
	if hostAwareAutoRankingEnabled(hostRuntime) {
		return "host_aware_bottleneck"
	}
	return "probe_elapsed"
}

func candidateAllowedForHost(candidate candidate, hostRuntime playbackprofile.HostRuntimeSnapshot) bool {
	if perfTier := hostPerformanceTier(hostRuntime.PerformanceClass); perfTier >= 0 && perfTier < minimumPerformanceTierForCodec(candidate.codec) {
		return false
	}
	if benchmarkTier := hostBenchmarkTier(playbackprofile.BenchmarkClassForCodec(hostRuntime.Benchmark, candidate.codec)); benchmarkTier >= 0 && benchmarkTier < minimumBenchmarkTierForCodec(candidate.codec) {
		return false
	}
	return true
}

func candidateHostStrength(candidate candidate, hostRuntime playbackprofile.HostRuntimeSnapshot) int {
	perfTier := hostPerformanceTier(hostRuntime.PerformanceClass)
	benchmarkTier := hostBenchmarkTier(playbackprofile.BenchmarkClassForCodec(hostRuntime.Benchmark, candidate.codec))
	switch {
	case perfTier >= 0 && benchmarkTier >= 0:
		if perfTier < benchmarkTier {
			return perfTier
		}
		return benchmarkTier
	case perfTier >= 0:
		return perfTier
	case benchmarkTier >= 0:
		return benchmarkTier
	default:
		return -1
	}
}

func hostPerformanceTier(raw string) int {
	switch normalize.Token(raw) {
	case "low":
		return 0
	case "medium":
		return 1
	case "high", "ultra":
		return 2
	default:
		return -1
	}
}

func hostBenchmarkTier(raw string) int {
	switch normalize.Token(raw) {
	case "weak":
		return 0
	case "moderate":
		return 1
	case "strong":
		return 2
	default:
		return -1
	}
}

func minimumPerformanceTierForCodec(codec string) int {
	switch normalize.Token(codec) {
	case "av1":
		return 2
	case "hevc":
		return 1
	default:
		return 0
	}
}

func minimumBenchmarkTierForCodec(codec string) int {
	switch normalize.Token(codec) {
	case "av1", "hevc":
		return 1
	default:
		return 0
	}
}

func codecQualityPriority(codec string) int {
	switch normalize.Token(codec) {
	case "av1":
		return 2
	case "hevc":
		return 1
	default:
		return 0
	}
}

func measuredProbeElapsedForCodec(codec string) time.Duration {
	cap := capabilityForAutoCodec(codec)
	if cap.Verified && cap.ProbeElapsed > 0 {
		return cap.ProbeElapsed
	}
	return 24 * time.Hour
}

func ParseCodecList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\t' || r == '\n' || r == '\r'
	})

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t == "" {
			continue
		}
		switch t {
		case "av01":
			t = "av1"
		case "h265", "h.265":
			t = "hevc"
		case "h264", "avc", "avc1":
			t = "h264"
		}
		if t != "av1" && t != "hevc" && t != "h264" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func capabilityForAutoCodec(codec string) hardware.HardwareEncoderCapability {
	cap, _, ok := hardware.HardwareEncoderCapabilityFor(codec)
	if !ok {
		return hardware.HardwareEncoderCapability{}
	}
	return cap
}

func capabilityForRequestedCodec(encoderCaps map[string]hardware.HardwareEncoderCapability, codec string) (hardware.HardwareEncoderCapability, bool) {
	if cap, ok := encoderCaps[codec]; ok {
		return cap, true
	}
	for _, legacyKey := range []string{codec + "_vaapi", codec + "_nvenc"} {
		if cap, ok := encoderCaps[legacyKey]; ok {
			return cap, true
		}
	}
	return hardware.HardwareEncoderCapability{}, false
}

func containsCodec(codecs []string, want string) bool {
	for _, codec := range codecs {
		if strings.EqualFold(strings.TrimSpace(codec), want) {
			return true
		}
	}
	return false
}

func dedupeOrdered(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		codec := strings.ToLower(strings.TrimSpace(raw))
		if codec == "" {
			continue
		}
		if _, ok := seen[codec]; ok {
			continue
		}
		seen[codec] = struct{}{}
		out = append(out, codec)
	}
	return out
}

func playbackCapabilitiesHaveCodec(codecs []string, want string) bool {
	want = normalize.Token(want)
	if want == "" {
		return false
	}
	for _, codec := range codecs {
		if normalize.Token(codec) == want {
			return true
		}
	}
	return false
}

func requiredVerifiedHardwareCodecForProfile(profileID string) (string, bool) {
	switch profiles.NormalizeRequestedProfileID(profileID) {
	case profiles.ProfileAV1HW:
		return "av1", true
	case profiles.ProfileSafariHEVCHW, profiles.ProfileSafariHEVCHWLL:
		return "hevc", true
	default:
		return "", false
	}
}
