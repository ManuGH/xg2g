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
const nvencRuntimeFailureThreshold = 3

type HardwareEncoderCapability struct {
	Verified     bool
	ProbeElapsed time.Duration
	AutoEligible bool
}

type VAAPIEncoderCapability = HardwareEncoderCapability
type NVENCEncoderCapability = HardwareEncoderCapability

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
	vaapiEncCaps    map[string]HardwareEncoderCapability

	nvencMu      sync.RWMutex
	nvencChecked bool
	nvencPassed  bool
	// Runtime NVENC encode failures observed after successful startup preflight.
	nvencRuntimeFailures int

	nvencEncMu      sync.RWMutex
	nvencEncChecked bool
	nvencEncCaps    map[string]HardwareEncoderCapability
)

// HasVAAPI checks if the VAAPI render device exists
func HasVAAPI() bool {
	// Check for /dev/dri/renderD128
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		return true
	}
	return false
}

// HasNVENC checks if the NVIDIA container runtime exposed an encoder-capable device.
func HasNVENC() bool {
	for _, path := range []string{"/dev/nvidiactl", "/dev/nvidia0"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
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
	caps := make(map[string]HardwareEncoderCapability, len(verified))
	for k, v := range verified {
		if !v {
			continue
		}
		caps[k] = HardwareEncoderCapability{
			Verified:     true,
			AutoEligible: true,
		}
	}
	SetVAAPIEncoderCapabilities(caps)
}

// SetVAAPIEncoderCapabilities records per-encoder capability state from startup
// preflight, including verification, elapsed probe time, and whether the codec
// is considered auto-eligible for generic negotiation on this host.
func SetVAAPIEncoderCapabilities(capabilities map[string]HardwareEncoderCapability) {
	vaapiEncMu.Lock()
	defer vaapiEncMu.Unlock()
	vaapiEncChecked = true
	if capabilities == nil {
		vaapiEncCaps = nil
		return
	}
	vaapiEncCaps = make(map[string]HardwareEncoderCapability, len(capabilities))
	for k, v := range capabilities {
		if v.Verified {
			vaapiEncCaps[k] = v
		}
	}
}

// SetNVENCPreflightResult records the result of the real NVENC encode preflight.
func SetNVENCPreflightResult(passed bool) {
	nvencMu.Lock()
	defer nvencMu.Unlock()
	nvencChecked = true
	nvencPassed = passed
	nvencRuntimeFailures = 0
}

// SetNVENCEncoderCapabilities records per-encoder NVENC capability state from startup preflight.
func SetNVENCEncoderCapabilities(capabilities map[string]HardwareEncoderCapability) {
	nvencEncMu.Lock()
	defer nvencEncMu.Unlock()
	nvencEncChecked = true
	if capabilities == nil {
		nvencEncCaps = nil
		return
	}
	nvencEncCaps = make(map[string]HardwareEncoderCapability, len(capabilities))
	for k, v := range capabilities {
		if v.Verified {
			nvencEncCaps[k] = v
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

// IsNVENCReady returns true only if startup NVENC preflight ran and passed.
func IsNVENCReady() bool {
	nvencMu.RLock()
	defer nvencMu.RUnlock()
	return nvencChecked && nvencPassed
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

// IsNVENCEncoderReady returns true only if NVENC encoder preflight has run AND the encoder was verified.
func IsNVENCEncoderReady(encoder string) bool {
	nvencEncMu.RLock()
	defer nvencEncMu.RUnlock()
	if !nvencEncChecked || nvencEncCaps == nil {
		return false
	}
	return nvencEncCaps[encoder].Verified
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

// IsNVENCEncoderAutoEligible returns true only if startup preflight verified the
// NVENC encoder and classified it as suitable for generic automatic selection.
func IsNVENCEncoderAutoEligible(encoder string) bool {
	nvencEncMu.RLock()
	defer nvencEncMu.RUnlock()
	if !nvencEncChecked || nvencEncCaps == nil {
		return false
	}
	cap, ok := nvencEncCaps[encoder]
	return ok && cap.Verified && cap.AutoEligible
}

// VAAPIEncoderCapabilityFor returns the stored startup capability for a VAAPI
// encoder. The bool is false if preflight has not run or the encoder was not
// verified.
func VAAPIEncoderCapabilityFor(encoder string) (VAAPIEncoderCapability, bool) {
	vaapiEncMu.RLock()
	defer vaapiEncMu.RUnlock()
	if !vaapiEncChecked || vaapiEncCaps == nil {
		return HardwareEncoderCapability{}, false
	}
	cap, ok := vaapiEncCaps[encoder]
	if !ok || !cap.Verified {
		return HardwareEncoderCapability{}, false
	}
	return cap, true
}

// NVENCEncoderCapabilityFor returns the stored startup capability for an NVENC encoder.
func NVENCEncoderCapabilityFor(encoder string) (NVENCEncoderCapability, bool) {
	nvencEncMu.RLock()
	defer nvencEncMu.RUnlock()
	if !nvencEncChecked || nvencEncCaps == nil {
		return HardwareEncoderCapability{}, false
	}
	cap, ok := nvencEncCaps[encoder]
	if !ok || !cap.Verified {
		return HardwareEncoderCapability{}, false
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
	vaapiEncCaps = map[string]HardwareEncoderCapability{}
	vaapiEncMu.Unlock()
	return failures, true
}

// RecordNVENCRuntimeFailure increments the runtime failure counter after startup preflight.
func RecordNVENCRuntimeFailure() (failures int, demoted bool) {
	nvencMu.Lock()
	if !nvencChecked || !nvencPassed {
		failures = nvencRuntimeFailures
		nvencMu.Unlock()
		return failures, false
	}
	nvencRuntimeFailures++
	failures = nvencRuntimeFailures
	if nvencRuntimeFailures < nvencRuntimeFailureThreshold {
		nvencMu.Unlock()
		return failures, false
	}
	nvencPassed = false
	nvencMu.Unlock()

	nvencEncMu.Lock()
	nvencEncChecked = true
	nvencEncCaps = map[string]HardwareEncoderCapability{}
	nvencEncMu.Unlock()
	return failures, true
}
