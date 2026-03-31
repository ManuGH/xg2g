package decision

import mediacodec "github.com/ManuGH/xg2g/internal/media/codec"

// EvaluateVideoCompatibility is the typed successor to the current string-only
// video predicate path. It intentionally does not choose policy; it reports the
// concrete incompatibilities and uncertainties that the decision layer can later
// translate into fail-closed behavior.
func EvaluateVideoCompatibility(source mediacodec.VideoCapability, client mediacodec.VideoCapability) mediacodec.CompatibilityResult {
	var result mediacodec.CompatibilityResult

	if source.Codec == mediacodec.IDUnknown || client.Codec == mediacodec.IDUnknown || source.Codec != client.Codec {
		result.Add(mediacodec.ReasonCodecMismatch)
		return result
	}

	if source.Interlaced {
		result.Add(mediacodec.ReasonInterlacedSource)
	}

	switch compareBitDepth(source, client) {
	case mediacodec.Exceeds:
		result.Add(mediacodec.ReasonBitDepthExceeded)
	case mediacodec.Uncertain:
		result.Add(mediacodec.ReasonBitDepthUnknown)
	}

	switch compareResolution(source, client) {
	case mediacodec.Exceeds:
		result.Add(mediacodec.ReasonResolutionExceeded)
	case mediacodec.Uncertain:
		result.Add(mediacodec.ReasonResolutionUnknown)
	}

	switch compareFrameRate(source, client) {
	case mediacodec.Exceeds:
		result.Add(mediacodec.ReasonFrameRateExceeded)
	case mediacodec.Uncertain:
		result.Add(mediacodec.ReasonFrameRateUnknown)
	}

	return result
}

func compareBitDepth(source mediacodec.VideoCapability, client mediacodec.VideoCapability) mediacodec.CompareResult {
	sourceKnown := source.HasKnownBitDepth()
	clientKnown := client.HasKnownBitDepth()
	if !sourceKnown && !clientKnown {
		// Keep the phase-1 migration path from exploding on legacy inputs that do
		// not surface bit depth anywhere. Treat fully-unknown bit depth as
		// neutral here; once source/client probes fill this field reliably, this
		// can be tightened further.
		return mediacodec.Within
	}
	if !sourceKnown || !clientKnown {
		return mediacodec.Uncertain
	}
	if source.BitDepth > client.BitDepth {
		return mediacodec.Exceeds
	}
	return mediacodec.Within
}

func compareResolution(source mediacodec.VideoCapability, client mediacodec.VideoCapability) mediacodec.CompareResult {
	if !client.MaxRes.Known() {
		return mediacodec.Within
	}
	return source.MaxRes.Compare(client.MaxRes)
}

func compareFrameRate(source mediacodec.VideoCapability, client mediacodec.VideoCapability) mediacodec.CompareResult {
	if !client.MaxFrameRate.Known() {
		return mediacodec.Within
	}
	return source.MaxFrameRate.Compare(client.MaxFrameRate)
}
