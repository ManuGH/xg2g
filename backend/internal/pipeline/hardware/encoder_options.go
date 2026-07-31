package hardware

import (
	"sort"
	"strings"
	"sync"
)

// Encoder *options* need the same treatment encoders themselves already get:
// proof, not inference. Knowing the silicon vendor does not tell you which
// rate-control modes a driver accepts - an AMD stack advertises QVBR, an Intel
// iHD stack rejects it ("Driver does not support QVBR RC mode (supported modes:
// CQP, CBR, VBR, ICQ)") and fails at encoder-open, killing the session before
// its first frame. Vendor tables would also have to be rewritten for every
// driver release and every future generation.
//
// So the startup preflight tries each candidate mode once, for real, and
// records what worked. Deriving the flags from that result makes the vendor a
// log field rather than a decision input, and a driver that gains or loses a
// mode is picked up on the next restart with no code change.
const (
	// RateControlQVBR targets a quality level while honouring -maxrate as a hard
	// ceiling. Best picture per bit of the capped modes, where available.
	RateControlQVBR = "QVBR"
	// RateControlVBR is the universal fallback: bitrate-targeted, cap honoured.
	RateControlVBR = "VBR"
	// OptionBFrames is the encoder's willingness to reorder frames. Measured on
	// an Intel AV1 encoder at a fixed QP, B-frames cut the bitstream by 9.3%
	// (-bf 7) and 7.0% (-bf 2) at identical quality settings - at a fixed
	// bitrate those bits go into the picture instead. Whether a given encoder
	// honours the option (rather than silently ignoring or rejecting it) is a
	// per-encoder fact, so it is probed like every other one.
	OptionBFrames = "bf"

	// RateControlICQ is deliberately NOT probed or used. It is quality-targeted
	// but ignores the bitrate ceiling, and a ceiling-less mode has already
	// filled this deployment's /dev/shm segment store once (CQP at QP20 ran
	// ~60 Mbit instead of 14, producing 0-byte segments).
	RateControlICQ = "ICQ"
)

var (
	encoderOptMu       sync.RWMutex
	encoderOptChecked  bool
	encoderOptVerified map[string]map[string]bool

	encoderRCMu       sync.RWMutex
	encoderRCChecked  bool
	encoderRCVerified map[string]map[string]bool
)

// SetVAAPIRateControlCapabilities records, per encoder, which rate-control
// modes the driver accepted during startup preflight.
func SetVAAPIRateControlCapabilities(capabilities map[string]map[string]bool) {
	encoderRCMu.Lock()
	defer encoderRCMu.Unlock()
	encoderRCChecked = capabilities != nil
	if capabilities == nil {
		encoderRCVerified = nil
		return
	}
	encoderRCVerified = make(map[string]map[string]bool, len(capabilities))
	for encoder, modes := range capabilities {
		copied := make(map[string]bool, len(modes))
		for mode, ok := range modes {
			copied[normalizeRateControlMode(mode)] = ok
		}
		encoderRCVerified[strings.ToLower(strings.TrimSpace(encoder))] = copied
	}
}

// VAAPIRateControlVerified reports whether the probe proved this encoder
// accepts this rate-control mode. It answers false while no probe has run:
// an unproven mode must never reach a live session, because the failure mode
// is a stream that dies at encoder-open rather than one that looks worse.
func VAAPIRateControlVerified(encoder, mode string) bool {
	encoderRCMu.RLock()
	defer encoderRCMu.RUnlock()
	if !encoderRCChecked {
		return false
	}
	return encoderRCVerified[strings.ToLower(strings.TrimSpace(encoder))][normalizeRateControlMode(mode)]
}

// VAAPIRateControlModes lists the verified modes for an encoder, sorted for
// stable logging.
func VAAPIRateControlModes(encoder string) []string {
	encoderRCMu.RLock()
	defer encoderRCMu.RUnlock()
	modes := make([]string, 0, len(encoderRCVerified[strings.ToLower(strings.TrimSpace(encoder))]))
	for mode, ok := range encoderRCVerified[strings.ToLower(strings.TrimSpace(encoder))] {
		if ok {
			modes = append(modes, mode)
		}
	}
	sort.Strings(modes)
	return modes
}

func normalizeRateControlMode(mode string) string {
	return strings.ToUpper(strings.TrimSpace(mode))
}

// SetVAAPIEncoderOptions records, per encoder, which non-rate-control encoder
// options the driver accepted during startup preflight.
func SetVAAPIEncoderOptions(capabilities map[string]map[string]bool) {
	encoderOptMu.Lock()
	defer encoderOptMu.Unlock()
	encoderOptChecked = capabilities != nil
	if capabilities == nil {
		encoderOptVerified = nil
		return
	}
	encoderOptVerified = make(map[string]map[string]bool, len(capabilities))
	for encoder, options := range capabilities {
		copied := make(map[string]bool, len(options))
		for option, ok := range options {
			copied[strings.ToLower(strings.TrimSpace(option))] = ok
		}
		encoderOptVerified[strings.ToLower(strings.TrimSpace(encoder))] = copied
	}
}

// VAAPIEncoderOptionVerified reports whether the probe proved this encoder
// accepts this option. Unprobed reads as unsupported, for the same reason it
// does for rate-control modes.
func VAAPIEncoderOptionVerified(encoder, option string) bool {
	encoderOptMu.RLock()
	defer encoderOptMu.RUnlock()
	if !encoderOptChecked {
		return false
	}
	return encoderOptVerified[strings.ToLower(strings.TrimSpace(encoder))][strings.ToLower(strings.TrimSpace(option))]
}

var (
	decodeMu       sync.RWMutex
	decodeChecked  bool
	decodeVerified map[string]bool
)

// SetVAAPIDecodeCapabilities records which input codecs the GPU proved it can
// decode. Requesting hardware decode is not free of consequence: with
// -hwaccel_output_format vaapi pinned, a decoder the driver cannot provide
// fails the session outright rather than falling back to software.
func SetVAAPIDecodeCapabilities(capabilities map[string]bool) {
	decodeMu.Lock()
	defer decodeMu.Unlock()
	decodeChecked = capabilities != nil
	if capabilities == nil {
		decodeVerified = nil
		return
	}
	decodeVerified = make(map[string]bool, len(capabilities))
	for codec, ok := range capabilities {
		decodeVerified[normalizeDecodeCodec(codec)] = ok
	}
}

// VAAPIDecodeVerified reports whether the GPU proved it decodes this input
// codec. An empty or unprobed codec reads as false: an unknown input is
// precisely the case where guessing costs the viewer the stream.
func VAAPIDecodeVerified(codec string) bool {
	decodeMu.RLock()
	defer decodeMu.RUnlock()
	if !decodeChecked {
		return false
	}
	return decodeVerified[normalizeDecodeCodec(codec)]
}

// normalizeDecodeCodec folds the spellings a scan, a container and FFmpeg each
// use for the same bitstream onto one key.
func normalizeDecodeCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "avc", "avc1", "libx264":
		return "h264"
	case "hevc", "h265", "hvc1", "hev1", "libx265":
		return "hevc"
	case "mpeg2", "mpeg2video", "mpegvideo":
		return "mpeg2video"
	case "av1", "av01":
		return "av1"
	case "vp9":
		return "vp9"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}
