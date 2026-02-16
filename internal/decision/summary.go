package decision

type InputSummary struct {
	Profile           string
	RequestedCodec    string
	SupportedHWCodecs []string
	HWAccelAvailable  bool
}

type OutputSummary struct {
	Path        string
	OutputCodec string
	UseHWAccel  bool
	Reason      string
}

func (in Input) Summary() InputSummary {
	n := normalizeInput(in)
	requested := n.RequestedCodec
	if requested == "" {
		rule := ruleForProfile(n.Profile, n.RequestedCodec)
		switch {
		case rule.preferredCodec != "":
			requested = rule.preferredCodec
		case rule.hardCodec != "":
			requested = rule.hardCodec
		default:
			requested = "auto"
		}
	}

	hwCodecs := make([]string, len(n.Server.SupportedHWCodecs))
	copy(hwCodecs, n.Server.SupportedHWCodecs)

	return InputSummary{
		Profile:           n.Profile,
		RequestedCodec:    requested,
		SupportedHWCodecs: hwCodecs,
		HWAccelAvailable:  n.Server.HWAccelAvailable,
	}
}

func (out Output) Summary() OutputSummary {
	codec := out.OutputCodec
	if codec == "" {
		codec = "none"
	}

	return OutputSummary{
		Path:        string(out.Path),
		OutputCodec: codec,
		UseHWAccel:  out.UseHWAccel,
		Reason:      string(out.Reason),
	}
}
