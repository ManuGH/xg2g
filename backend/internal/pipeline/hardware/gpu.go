// Package hardware provides GPU detection and readiness state.
//
// Two-tier VAAPI check:
//
//  1. HasVAAPI() — device file stat (/dev/dri/renderD128). Cheap, but only
//     proves the device node exists, not that encoding works.
//
//  2. IsVAAPIReady() — fail-closed: returns true only after the FFmpeg adapter's
//     PreflightVAAPI() has run a real 5-frame encode test and called
//     SetVAAPIPreflightResult(true). HTTP handlers use this to gate
//     hwaccel=force and to feed profiles.Resolve(), ensuring that profiles
//     with HWAccel="vaapi" are never produced unless GPU encoding is verified.
package hardware

import (
	"os"
	"sync"
	"time"
)

const vaapiRuntimeFailureThreshold = 3

type VAAPIEncoderCapability struct {
	Verified     bool
	ProbeElapsed time.Duration
	AutoEligible bool
}

var (
	vaapiMu      sync.RWMutex
	vaapiChecked bool
	vaapiPassed  bool
	// Runtime VAAPI encode failures observed after successful startup preflight.
	vaapiRuntimeFailures int

	// Per-encoder preflight results. These are populated by the FFmpeg adapter's
	// PreflightVAAPI(), and allow higher layers to make codec-specific decisions
	// (e.g. AV1 only if av1_vaapi verified).
	vaapiEncMu      sync.RWMutex
	vaapiEncChecked bool
	vaapiEncCaps    map[string]VAAPIEncoderCapability
)

// HasVAAPI checks if the VAAPI render device exists
func HasVAAPI() bool {
	// Check for /dev/dri/renderD128
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		return true
	}
	return false
}

// SetVAAPIPreflightResult records the result of the real VAAPI encode
// preflight. Called once at startup by the FFmpeg adapter after running
// actual encode tests (not just device file stat).
func SetVAAPIPreflightResult(passed bool) {
	vaapiMu.Lock()
	defer vaapiMu.Unlock()
	vaapiChecked = true
	vaapiPassed = passed
	vaapiRuntimeFailures = 0
}

// SetVAAPIEncoderPreflight records per-encoder preflight status (e.g. "av1_vaapi" -> true).
// Called once at startup by the FFmpeg adapter after running encoder-specific encode tests.
func SetVAAPIEncoderPreflight(verified map[string]bool) {
	caps := make(map[string]VAAPIEncoderCapability, len(verified))
	for k, v := range verified {
		if !v {
			continue
		}
		caps[k] = VAAPIEncoderCapability{
			Verified:     true,
			AutoEligible: true,
		}
	}
	SetVAAPIEncoderCapabilities(caps)
}

// SetVAAPIEncoderCapabilities records per-encoder capability state from startup
// preflight, including verification, elapsed probe time, and whether the codec
// is considered auto-eligible for generic negotiation on this host.
func SetVAAPIEncoderCapabilities(capabilities map[string]VAAPIEncoderCapability) {
	vaapiEncMu.Lock()
	defer vaapiEncMu.Unlock()
	vaapiEncChecked = true
	if capabilities == nil {
		vaapiEncCaps = nil
		return
	}
	vaapiEncCaps = make(map[string]VAAPIEncoderCapability, len(capabilities))
	for k, v := range capabilities {
		if v.Verified {
			vaapiEncCaps[k] = v
		}
	}
}

// IsVAAPIReady returns true only if the VAAPI render device exists AND
// the real encode preflight has been run AND passed.
// Fail-closed: returns false if preflight hasn't run yet.
func IsVAAPIReady() bool {
	vaapiMu.RLock()
	defer vaapiMu.RUnlock()
	return vaapiChecked && vaapiPassed
}

// IsVAAPIEncoderReady returns true only if per-encoder preflight has run AND the given encoder
// was verified. Fail-closed: returns false if encoder preflight hasn't run yet.
func IsVAAPIEncoderReady(encoder string) bool {
	vaapiEncMu.RLock()
	defer vaapiEncMu.RUnlock()
	if !vaapiEncChecked || vaapiEncCaps == nil {
		return false
	}
	return vaapiEncCaps[encoder].Verified
}

// IsVAAPIEncoderAutoEligible returns true only if startup preflight verified the
// encoder and classified it as suitable for generic automatic codec selection on
// this host. Fail-closed: returns false if encoder preflight hasn't run yet.
func IsVAAPIEncoderAutoEligible(encoder string) bool {
	vaapiEncMu.RLock()
	defer vaapiEncMu.RUnlock()
	if !vaapiEncChecked || vaapiEncCaps == nil {
		return false
	}
	cap, ok := vaapiEncCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

// VAAPIEncoderCapabilityFor returns the stored startup capability for a VAAPI
// encoder. The bool is false if preflight has not run or the encoder was not
// verified.
func VAAPIEncoderCapabilityFor(encoder string) (VAAPIEncoderCapability, bool) {
	vaapiEncMu.RLock()
	defer vaapiEncMu.RUnlock()
	if !vaapiEncChecked || vaapiEncCaps == nil {
		return VAAPIEncoderCapability{}, false
	}
	cap, ok := vaapiEncCaps[encoder]
	if !ok || !cap.Verified {
		return VAAPIEncoderCapability{}, false
	}
	return cap, true
}

// RecordVAAPIRuntimeFailure increments the runtime failure counter after startup preflight.
// After threshold is reached, VAAPI is demoted to not-ready and encoder readiness is cleared.
func RecordVAAPIRuntimeFailure() (failures int, demoted bool) {
	vaapiMu.Lock()
	if !vaapiChecked || !vaapiPassed {
		failures = vaapiRuntimeFailures
		vaapiMu.Unlock()
		return failures, false
	}
	vaapiRuntimeFailures++
	failures = vaapiRuntimeFailures
	if vaapiRuntimeFailures < vaapiRuntimeFailureThreshold {
		vaapiMu.Unlock()
		return failures, false
	}
	vaapiPassed = false
	vaapiMu.Unlock()

	vaapiEncMu.Lock()
	vaapiEncChecked = true
	vaapiEncCaps = map[string]VAAPIEncoderCapability{}
	vaapiEncMu.Unlock()
	return failures, true
}
