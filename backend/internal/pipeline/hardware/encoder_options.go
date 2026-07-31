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
	// RateControlICQ is deliberately NOT probed or used. It is quality-targeted
	// but ignores the bitrate ceiling, and a ceiling-less mode has already
	// filled this deployment's /dev/shm segment store once (CQP at QP20 ran
	// ~60 Mbit instead of 14, producing 0-byte segments).
	RateControlICQ = "ICQ"
)

var (
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
