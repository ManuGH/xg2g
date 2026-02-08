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
)

var (
	vaapiMu      sync.RWMutex
	vaapiChecked bool
	vaapiPassed  bool
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
}

// IsVAAPIReady returns true only if the VAAPI render device exists AND
// the real encode preflight has been run AND passed.
// Fail-closed: returns false if preflight hasn't run yet.
func IsVAAPIReady() bool {
	vaapiMu.RLock()
	defer vaapiMu.RUnlock()
	return vaapiChecked && vaapiPassed
}
