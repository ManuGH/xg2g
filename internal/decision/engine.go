package decision

import (
	"sort"
	"strings"
)

type Path string

const (
	PathDirectPlay   Path = "direct"
	PathRemux        Path = "remux"
	PathTranscodeCPU Path = "transcode_cpu"
	PathTranscodeHW  Path = "transcode_hw"
	PathReject       Path = "reject"
)

type Reason string

const (
	ReasonDirectPlaySupported Reason = "direct_play_supported"
	ReasonRemuxRequired       Reason = "remux_required"
	ReasonProfilePreference   Reason = "profile_preference"
	ReasonHWCodecUnavailable  Reason = "hw_codec_unavailable"
	ReasonProfileConstraint   Reason = "profile_constraint"
	ReasonCPUPreferred        Reason = "cost_cpu_preferred"
	ReasonCodecSelected       Reason = "codec_selected"
	ReasonNoCompatibleCodec   Reason = "no_compatible_codec"
)

type Input struct {
	SourceCodec      string
	SourceContainer  string
	ClientCodecs     []string
	ClientContainers []string
	Profile          string
	RequestedCodec   string
	RequireHW        bool
	Server           ServerCapabilities
}

type ServerCapabilities struct {
	HWAccelAvailable  bool
	SupportedHWCodecs []string
}

type Output struct {
	Path        Path
	OutputCodec string
	UseHWAccel  bool
	Reason      Reason
}

type profileRule struct {
	preferredCodec string
	hardCodec      string
	preferHW       bool
	requireHW      bool
}

type candidate struct {
	codec string
	hw    bool
	score int
}

func Decide(in Input) Output {
	in = normalizeInput(in)

	// Direct/remux only applies when source truth + client capabilities are known.
	if in.SourceCodec != "" && len(in.ClientCodecs) > 0 && contains(in.ClientCodecs, in.SourceCodec) {
		if in.SourceContainer != "" && len(in.ClientContainers) > 0 && contains(in.ClientContainers, in.SourceContainer) {
			return Output{Path: PathDirectPlay, OutputCodec: in.SourceCodec, Reason: ReasonDirectPlaySupported}
		}
		return Output{Path: PathRemux, OutputCodec: in.SourceCodec, Reason: ReasonRemuxRequired}
	}

	rule := ruleForProfile(in.Profile, in.RequestedCodec)

	// Explicit hard requirement: codec must be available via HW.
	if rule.requireHW {
		required := rule.preferredCodec
		if required == "" {
			required = in.RequestedCodec
		}
		if required == "" || !hwCodecAvailable(in.Server, required) {
			return Output{Path: PathReject, Reason: ReasonHWCodecUnavailable}
		}
		return Output{
			Path:        PathTranscodeHW,
			OutputCodec: required,
			UseHWAccel:  true,
			Reason:      ReasonProfileConstraint,
		}
	}

	// Adapter-level hard requirement: caller requested strict HW usage.
	if in.RequireHW {
		required := in.RequestedCodec
		if required == "" {
			required = rule.preferredCodec
		}
		if required != "" {
			if !hwCodecAvailable(in.Server, required) {
				return Output{Path: PathReject, Reason: ReasonHWCodecUnavailable}
			}
			return Output{
				Path:        PathTranscodeHW,
				OutputCodec: required,
				UseHWAccel:  true,
				Reason:      ReasonProfileConstraint,
			}
		}
	}

	allowedCodecs := normalizedCodecs(in.ClientCodecs)
	if len(allowedCodecs) == 0 {
		allowedCodecs = []string{"h264", "hevc", "av1"}
	}

	// Hard codec constraint profile (e.g. safari_hevc).
	if rule.hardCodec != "" {
		if !contains(allowedCodecs, rule.hardCodec) {
			return Output{Path: PathReject, Reason: ReasonNoCompatibleCodec}
		}
		out, ok := pickBestCandidate(in, []string{rule.hardCodec}, rule)
		if !ok {
			return Output{Path: PathReject, Reason: ReasonNoCompatibleCodec}
		}
		out.Reason = ReasonProfileConstraint
		return out
	}

	out, ok := pickBestCandidate(in, allowedCodecs, rule)
	if !ok {
		return Output{Path: PathReject, Reason: ReasonNoCompatibleCodec}
	}

	if rule.preferredCodec != "" {
		if out.OutputCodec != rule.preferredCodec || (rule.preferHW && !out.UseHWAccel) {
			out.Reason = ReasonProfilePreference
			return out
		}
	}

	if !out.UseHWAccel && out.OutputCodec == "h264" {
		out.Reason = ReasonCPUPreferred
		return out
	}

	out.Reason = ReasonCodecSelected
	return out
}

func pickBestCandidate(in Input, codecs []string, rule profileRule) (Output, bool) {
	var candidates []candidate
	for _, codec := range codecs {
		c := canonicalCodec(codec)
		if c == "" {
			continue
		}
		candidates = append(candidates, candidate{
			codec: c,
			hw:    false,
			score: scoreCandidate(c, false, rule),
		})
		if hwCodecAvailable(in.Server, c) {
			candidates = append(candidates, candidate{
				codec: c,
				hw:    true,
				score: scoreCandidate(c, true, rule),
			})
		}
	}

	if len(candidates) == 0 {
		return Output{}, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].hw != candidates[j].hw {
			return candidates[i].hw
		}
		return codecRank(candidates[i].codec) > codecRank(candidates[j].codec)
	})

	best := candidates[0]
	out := Output{
		OutputCodec: best.codec,
		UseHWAccel:  best.hw,
		Path:        PathTranscodeCPU,
	}
	if best.hw {
		out.Path = PathTranscodeHW
	}
	return out, true
}

func scoreCandidate(codec string, hw bool, rule profileRule) int {
	score := 0

	switch codec {
	case "av1":
		score += 60
	case "hevc":
		score += 50
	case "h264":
		score += 40
	}

	if hw {
		score += 30
	} else {
		switch codec {
		case "h264":
			score += 15
		case "hevc":
			score -= 10
		case "av1":
			score -= 20
		}
	}

	if rule.preferredCodec != "" && codec == rule.preferredCodec {
		score += 20
	}
	if rule.preferHW && hw {
		score += 10
	}

	return score
}

func ruleForProfile(profile string, requestedCodec string) profileRule {
	p := strings.ToLower(strings.TrimSpace(profile))
	switch p {
	case "av1_hw":
		return profileRule{preferredCodec: "av1", preferHW: true}
	case "av1_required":
		return profileRule{preferredCodec: "av1", requireHW: true}
	case "safari_hevc":
		return profileRule{hardCodec: "hevc"}
	case "safari_hevc_hw", "safari_hevc_hw_ll":
		return profileRule{preferredCodec: "hevc", preferHW: true}
	case "safari":
		return profileRule{preferredCodec: "h264"}
	}

	if requestedCodec != "" {
		return profileRule{preferredCodec: canonicalCodec(requestedCodec)}
	}
	return profileRule{}
}

func normalizeInput(in Input) Input {
	in.SourceCodec = canonicalCodec(in.SourceCodec)
	in.SourceContainer = normalizeToken(in.SourceContainer)
	in.Profile = strings.ToLower(strings.TrimSpace(in.Profile))
	in.RequestedCodec = canonicalCodec(in.RequestedCodec)
	in.ClientCodecs = normalizedCodecs(in.ClientCodecs)
	in.ClientContainers = normalizedTokens(in.ClientContainers)
	in.Server.SupportedHWCodecs = normalizedCodecs(in.Server.SupportedHWCodecs)
	return in
}

func normalizedCodecs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		c := canonicalCodec(v)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func normalizedTokens(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		t := normalizeToken(v)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func canonicalCodec(raw string) string {
	v := normalizeToken(raw)
	switch v {
	case "h264", "avc", "avc1", "libx264", "h264_vaapi":
		return "h264"
	case "hevc", "h265", "h.265", "libx265", "hevc_vaapi":
		return "hevc"
	case "av1", "av01", "av1_vaapi", "libsvtav1", "libaom-av1":
		return "av1"
	default:
		return v
	}
}

func normalizeToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func hwCodecAvailable(server ServerCapabilities, codec string) bool {
	if !server.HWAccelAvailable {
		return false
	}
	return contains(server.SupportedHWCodecs, codec)
}

func contains(slice []string, value string) bool {
	v := normalizeToken(value)
	for _, item := range slice {
		if normalizeToken(item) == v {
			return true
		}
	}
	return false
}

func codecRank(codec string) int {
	switch codec {
	case "av1":
		return 3
	case "hevc":
		return 2
	case "h264":
		return 1
	default:
		return 0
	}
}
