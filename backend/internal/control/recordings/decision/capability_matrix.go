package decision

// Unified capability matrix.
//
// The decision engine carried four overlapping answers to the single question
// "what can this device play":
//   1. caps.VideoCodecs   — a flat []string the predicate used directly
//   2. videoCodecSignals  — per-codec smooth/powerEfficient, gathered by the
//                           client but previously dropped before it reached
//                           decision.Capabilities
//   3. caps.MaxVideo       — one global resolution cap, not per codec
//   4. runtimepolicy.MaxQualityRung — the learned per-device-class ceiling
//
// This file collapses (1)-(3) into ONE per-codec model that the predicate, the
// EPG badge, and the learning loop can all read. computePredicates and
// CanKeepVideoCopy now consume it via CanVideoFromMatrix. The migration is
// behaviour-preserving by construction: with minDirectVideoTier == TierUnverified
// and no signals, CanVideoFromMatrix reduces exactly to the old
// contains(caps.VideoCodecs, …) && withinMaxVideo(…) pair; signals only refine
// it. Choosing a higher minDirectVideoTier is the one open policy decision.

// minDirectVideoTier is the policy knob (ZUR ABSTIMMUNG). It is the lowest
// confidence tier at which a source video codec may be kept (direct play /
// direct stream / copy) instead of transcoded.
//
//   - TierUnverified (current default): behaviour-preserving for clients that
//     send no signals (matrix == flat list), and merely *additive* when signals
//     are present (codecs the conservative flat list withheld but the device
//     supports become eligible). Most optimistic; relies on the render-quality
//     learning loop to claw back anything that actually stutters.
//   - TierSoftware: require the browser to predict smooth playback. Honest
//     "we believe it plays well" bar — but rejects every signal-less codec
//     (the flat list carries no tier), so it needs the flat list to be modelled
//     as smooth before it can be the default. That is the open decision.
//   - TierHardware: only hardware-decoded codecs kept; everything else
//     transcoded. Safest, most CPU.
const minDirectVideoTier = TierUnverified

// CapabilityTier expresses HOW WELL a device decodes a codec, not just whether
// it can — the distinction the flat VideoCodecs list throws away.
type CapabilityTier int

const (
	TierNone       CapabilityTier = iota // cannot decode, or over the resolution cap
	TierUnverified                       // decodes, but the browser never confirmed smooth
	TierSoftware                         // decodes smoothly, but in software (CPU cost)
	TierHardware                         // hardware-accelerated (powerEfficient)
)

func (t CapabilityTier) String() string {
	switch t {
	case TierHardware:
		return "hardware"
	case TierSoftware:
		return "software"
	case TierUnverified:
		return "unverified"
	default:
		return "none"
	}
}

// VideoCodecSignal is the per-codec runtime decode probe the client already
// gathers via MediaCapabilities. Today it reaches the backend's capabilities
// layer but is NOT mapped into decision.Capabilities — so the predicate cannot
// see it. Threading this in is what makes the matrix possible.
type VideoCodecSignal struct {
	Codec          string `json:"codec"`
	Supported      bool   `json:"supported"`
	Smooth         *bool  `json:"smooth,omitempty"`
	PowerEfficient *bool  `json:"powerEfficient,omitempty"`
}

// CodecCapability is the device's decode capability for ONE video codec.
type CodecCapability struct {
	Supported      bool
	Smooth         bool
	PowerEfficient bool
	MaxWidth       int // 0 = unbounded / unknown
	MaxHeight      int
	MaxFPS         int
}

// Tier reduces the capability to a confidence level.
func (c CodecCapability) Tier() CapabilityTier {
	switch {
	case !c.Supported:
		return TierNone
	case c.PowerEfficient:
		return TierHardware
	case c.Smooth:
		return TierSoftware
	default:
		return TierUnverified
	}
}

// CapabilityMatrix maps a normalized codec name to its capability.
type CapabilityMatrix map[string]CodecCapability

// BuildCapabilityMatrix folds the wire-level capability fields into one model.
// Precedence: the rich per-codec signals win; the flat VideoCodecs list back-
// fills "supported" for codecs without a signal; MaxVideo applies as a (today
// still global) resolution cap. When the client probe later reports resolution
// per codec, ONLY this function changes — CanDeliverVideo, the predicate, and
// the badge stay exactly as they are.
func BuildCapabilityMatrix(signals []VideoCodecSignal, videoCodecs []string, maxVideo *MaxVideoDimensions) CapabilityMatrix {
	m := CapabilityMatrix{}

	for _, sig := range signals {
		codec := robustNorm(sig.Codec)
		if codec == "" {
			continue
		}
		cc := m[codec]
		cc.Supported = cc.Supported || sig.Supported
		if sig.Smooth != nil {
			cc.Smooth = cc.Smooth || *sig.Smooth
		}
		if sig.PowerEfficient != nil {
			cc.PowerEfficient = cc.PowerEfficient || *sig.PowerEfficient
		}
		m[codec] = cc
	}

	for _, codec := range videoCodecs {
		c := robustNorm(codec)
		if c == "" {
			continue
		}
		cc := m[c]
		cc.Supported = true // flat list asserts support; signals already added nuance
		m[c] = cc
	}

	if maxVideo != nil {
		for codec, cc := range m {
			cc.MaxWidth = maxVideo.Width
			cc.MaxHeight = maxVideo.Height
			cc.MaxFPS = maxVideo.FPS
			m[codec] = cc
		}
	}

	return m
}

// CanDeliverVideo is the matrix-native replacement for the predicate's
//
//	contains(caps.VideoCodecs, source.VideoCodec) && withinMaxVideo(source, caps.MaxVideo)
//
// pair: one per-codec, resolution-aware check that also returns the confidence
// tier so policy can require a minimum tier for direct play instead of treating
// "plays in hardware" and "supported but never confirmed smooth" identically.
func (m CapabilityMatrix) CanDeliverVideo(source Source) (bool, CapabilityTier) {
	cc, ok := m[robustNorm(source.VideoCodec)]
	if !ok || !cc.Supported {
		return false, TierNone
	}
	if !withinDimension(source.Width, cc.MaxWidth) ||
		!withinDimension(source.Height, cc.MaxHeight) ||
		!withinFrameRate(source.FPS, cc.MaxFPS) {
		return false, TierNone
	}
	return true, cc.Tier()
}

// withinDimension / withinFrameRate keep withinMaxVideo's semantics: an unknown
// source dimension or an unset cap is treated as "within" (do not block on what
// we cannot measure — the Option A philosophy), now applied per codec.
func withinDimension(sourceVal, capVal int) bool {
	if capVal <= 0 || sourceVal <= 0 {
		return true
	}
	return sourceVal <= capVal
}

func withinFrameRate(sourceFPS float64, capFPS int) bool {
	if capFPS <= 0 || sourceFPS <= 0 {
		return true
	}
	return sourceFPS <= float64(capFPS)
}

// CanVideoFromMatrix shows the exact one-line migration for computePredicates.
//
//	before: canVideo := contains(caps.VideoCodecs, source.VideoCodec) &&
//	                    withinMaxVideo(source, caps.MaxVideo)
//	after:  canVideo := CanVideoFromMatrix(source, caps, signals, minDirectTier)
//
// minDirectTier is the single policy knob that ends the under/over-promise
// argument: require TierSoftware to direct-play (so a not-confirmed-smooth codec
// is transcoded for safety), or accept TierUnverified to lean optimistic. Today
// the flat list behaves as minDirectTier == TierUnverified with no way to choose.
func CanVideoFromMatrix(source Source, caps Capabilities, signals []VideoCodecSignal, minDirectTier CapabilityTier) bool {
	matrix := BuildCapabilityMatrix(signals, caps.VideoCodecs, caps.MaxVideo)
	ok, tier := matrix.CanDeliverVideo(source)
	return ok && tier >= minDirectTier
}
