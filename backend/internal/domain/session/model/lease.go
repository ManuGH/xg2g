package model

import (
	"fmt"
	"math"
	"time"
)

const (
	// NamespaceTunerSlot is the lease namespace for physical tuners
	NamespaceTunerSlot = "tuner"
	// NamespaceService is the lease namespace for service dedup
	NamespaceService = "service"
)

// LeaseKeyTunerSlot returns the standard lease key for a given tuner slot.
// e.g. "tuner:0"
func LeaseKeyTunerSlot(slot int) string {
	return fmt.Sprintf("%s:%d", NamespaceTunerSlot, slot)
}

// LeaseKeyService returns the standard lease key for a service reference.
// e.g. "service:1:0:1:..."
func LeaseKeyService(ref string) string {
	return fmt.Sprintf("%s:%s", NamespaceService, ref)
}

// SessionInactivityTTL returns the time a session may remain unconsumed before
// it is eligible for lease or idle cleanup.
//
// Live-only sessions retain the short operator-configured base timeout. DVR
// sessions retain their complete rolling window and use that base timeout as a
// resume grace period. This keeps mobile browser suspension from destroying a
// timeshift session before its advertised DVR window has elapsed.
func SessionInactivityTTL(base time.Duration, dvrWindowSec int) time.Duration {
	if base < 0 {
		base = 0
	}
	if dvrWindowSec <= 0 {
		return base
	}

	windowSeconds := int64(dvrWindowSec)
	maxWindowSeconds := int64((time.Duration(math.MaxInt64) - base) / time.Second)
	if windowSeconds > maxWindowSeconds {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(windowSeconds)*time.Second + base
}
