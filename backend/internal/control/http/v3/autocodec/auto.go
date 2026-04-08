package autocodec

import (
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

type candidate struct {
	profileID    string
	probeElapsed time.Duration
	tieOrder     int
}

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
	codecs := ResolveAutoTranscodeCodecs(caps)
	if len(codecs) == 0 {
		return ""
	}
	return PickProfileForCodecs(strings.Join(codecs, ","), hwaccelMode)
}

func PickProfileForCodecs(raw string, hwaccelMode profiles.HWAccelMode) string {
	return PickProfileForCodecsWithCapabilities(raw, hwaccelMode, map[string]hardware.HardwareEncoderCapability{
		"h264": capabilityForAutoCodec("h264"),
		"hevc": capabilityForAutoCodec("hevc"),
		"av1":  capabilityForAutoCodec("av1"),
	})
}

func PickProfileForCodecsWithCapabilities(raw string, hwaccelMode profiles.HWAccelMode, encoderCaps map[string]hardware.HardwareEncoderCapability) string {
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
			candidates = append(candidates, candidate{
				profileID:    profiles.ProfileH264FMP4,
				probeElapsed: cap.ProbeElapsed,
				tieOrder:     0,
			})
		} else {
			candidates = append(candidates, candidate{
				profileID:    profiles.ProfileH264FMP4,
				probeElapsed: 24 * time.Hour,
				tieOrder:     0,
			})
		}
	}

	if hwAllowed {
		if _, ok := requested["hevc"]; ok {
			if cap, exists := capabilityForRequestedCodec(encoderCaps, "hevc"); exists && cap.Verified && cap.AutoEligible && cap.ProbeElapsed > 0 {
				candidates = append(candidates, candidate{
					profileID:    profiles.ProfileSafariHEVCHW,
					probeElapsed: cap.ProbeElapsed,
					tieOrder:     1,
				})
			}
		}
		if _, ok := requested["av1"]; ok {
			if cap, exists := capabilityForRequestedCodec(encoderCaps, "av1"); exists && cap.Verified && cap.AutoEligible && cap.ProbeElapsed > 0 {
				candidates = append(candidates, candidate{
					profileID:    profiles.ProfileAV1HW,
					probeElapsed: cap.ProbeElapsed,
					tieOrder:     2,
				})
			}
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].probeElapsed == candidates[j].probeElapsed {
			return candidates[i].tieOrder < candidates[j].tieOrder
		}
		return candidates[i].probeElapsed < candidates[j].probeElapsed
	})

	return candidates[0].profileID
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
