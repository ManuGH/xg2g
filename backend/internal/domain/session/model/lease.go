package model

import (
	"fmt"
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
