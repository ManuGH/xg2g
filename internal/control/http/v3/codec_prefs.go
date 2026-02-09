package v3

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// pickProfileForCodecs maps a client-preferred codec list (in order) to a concrete profile ID.
// It returns "" when no override should be applied.
//
// This is intentionally conservative: AV1 is only selected when GPU encoding is possible.
// HEVC can fall back to CPU (x265) via safari_hevc(_hw) profiles.
func pickProfileForCodecs(raw string, hasGPU bool, hwaccelMode profiles.HWAccelMode) string {
	codecs := parseCodecList(raw)
	if len(codecs) == 0 {
		return ""
	}

	// If user explicitly disabled hwaccel, don't pick AV1 (GPU-only).
	hwAllowed := hwaccelMode != profiles.HWAccelOff

	for _, c := range codecs {
		switch c {
		case "av1":
			if hwAllowed && hasGPU {
				return profiles.ProfileAV1HW
			}
		case "hevc":
			// Prefer HW profile when possible, but allow CPU fallback via the same profile.
			// Resolve() will choose vaapi when hasGPU+auto/force.
			return profiles.ProfileSafariHEVCHW
		case "h264":
			// Prefer Safari profile for fMP4 output + VAAPI H.264 when available.
			return profiles.ProfileSafari
		}
	}

	return ""
}

func parseCodecList(raw string) []string {
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
		case "h265":
			t = "hevc"
		case "h.265":
			t = "hevc"
		case "h264":
			t = "h264"
		case "avc":
			t = "h264"
		case "avc1":
			t = "h264"
		}
		if t != "av1" && t != "hevc" && t != "h264" {
			continue
		}
		out = append(out, t)
	}
	return out
}

