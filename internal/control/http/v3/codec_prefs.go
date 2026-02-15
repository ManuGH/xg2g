package v3

import (
	"strings"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// pickProfileForCodecs maps a client-preferred codec list (in order) to a concrete profile ID.
// It returns "" when no override should be applied.
//
// This is intentionally conservative:
// - AV1 is selected only when av1_vaapi is verified (GPU-only).
// - HEVC prefers VAAPI when hevc_vaapi is verified, otherwise falls back to CPU HEVC profile.
// - H.264 maps to a dedicated always-transcode profile, with optional VAAPI when h264_vaapi is verified.
func pickProfileForCodecs(raw string, av1OK, hevcOK, h264OK bool, hwaccelMode profiles.HWAccelMode) string {
	codecs := parseCodecList(raw)
	if len(codecs) == 0 {
		return ""
	}

	// If user explicitly disabled hwaccel, don't pick AV1 (GPU-only).
	hwAllowed := hwaccelMode != profiles.HWAccelOff

	for _, c := range codecs {
		switch c {
		case "av1":
			if hwAllowed && av1OK {
				return profiles.ProfileAV1HW
			}
		case "hevc":
			if hwAllowed && hevcOK {
				return profiles.ProfileSafariHEVCHW
			}
			return profiles.ProfileSafariHEVC
		case "h264":
			_ = h264OK // Resolve() will choose VAAPI when h264OK is passed as hasGPU.
			return profiles.ProfileH264FMP4
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
