// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package worker

import "strconv"

const (
	leasePrefixTuner = "tuner:"   // exclusive hardware slot
	leasePrefixSvc   = "service:" // optional future dedupe
)

// LeaseKeyTunerSlot returns the distributed lock key for a specific tuner slot.
func LeaseKeyTunerSlot(slot int) string {
	return leasePrefixTuner + strconv.Itoa(slot)
}

// LeaseKeyService returns the distributed lock key for a service reference (dedup).
func LeaseKeyService(ref string) string {
	return leasePrefixSvc + ref
}
