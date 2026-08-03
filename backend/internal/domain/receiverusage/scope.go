// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidReceiverID   = errors.New("invalid receiver id")
	ErrInvalidSlotNumber   = errors.New("invalid slot number")
	ErrMalformedScopeKey   = errors.New("malformed restricted access scope key")
	ErrInvalidSlotQuantity = errors.New("invalid slot quantity")
)

var validReceiverIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

const (
	RestrictedAccessScopePrefix = "receiver:"
	RestrictedAccessScopeMiddle = ":restricted-access:"
	MaxRestrictedAccessSlots    = 256
)

// ValidateReceiverID checks that receiverID is non-empty and contains valid alphanumeric, hyphen, or underscore characters.
func ValidateReceiverID(receiverID string) error {
	if receiverID == "" {
		return fmt.Errorf("%w: receiver id cannot be empty", ErrInvalidReceiverID)
	}
	if !validReceiverIDRegex.MatchString(receiverID) {
		return fmt.Errorf("%w: receiver id %q contains invalid characters", ErrInvalidReceiverID, receiverID)
	}
	return nil
}

// FormatRestrictedAccessScope constructs a deterministic scope key string: receiver:<receiver-id>:restricted-access:<slot>.
func FormatRestrictedAccessScope(receiverID string, slot int) (string, error) {
	if err := ValidateReceiverID(receiverID); err != nil {
		return "", err
	}
	if slot < 0 {
		return "", fmt.Errorf("%w: slot cannot be negative (%d)", ErrInvalidSlotNumber, slot)
	}
	return fmt.Sprintf("receiver:%s:restricted-access:%d", receiverID, slot), nil
}

// ParseRestrictedAccessScope parses a scope string into receiverID and slot number, enforcing canonical scope formatting.
func ParseRestrictedAccessScope(scope string) (receiverID string, slot int, err error) {
	if !strings.HasPrefix(scope, RestrictedAccessScopePrefix) {
		return "", 0, fmt.Errorf("%w: missing prefix %q in scope %q", ErrMalformedScopeKey, RestrictedAccessScopePrefix, scope)
	}
	middleIdx := strings.Index(scope, RestrictedAccessScopeMiddle)
	if middleIdx == -1 {
		return "", 0, fmt.Errorf("%w: missing middle marker %q in scope %q", ErrMalformedScopeKey, RestrictedAccessScopeMiddle, scope)
	}

	recID := scope[len(RestrictedAccessScopePrefix):middleIdx]
	if err := ValidateReceiverID(recID); err != nil {
		return "", 0, fmt.Errorf("%w: %v", ErrMalformedScopeKey, err)
	}

	slotStr := scope[middleIdx+len(RestrictedAccessScopeMiddle):]
	if slotStr == "" {
		return "", 0, fmt.Errorf("%w: missing slot number in scope %q", ErrMalformedScopeKey, scope)
	}

	parsedSlot, err := strconv.Atoi(slotStr)
	if err != nil || parsedSlot < 0 {
		return "", 0, fmt.Errorf("%w: invalid slot number %q in scope %q", ErrMalformedScopeKey, slotStr, scope)
	}

	// Canonicality check: formatted canonical scope must exactly equal input scope string
	canonical, fmtErr := FormatRestrictedAccessScope(recID, parsedSlot)
	if fmtErr != nil || canonical != scope {
		return "", 0, fmt.Errorf("%w: non-canonical scope key %q (expected %q)", ErrMalformedScopeKey, scope, canonical)
	}

	return recID, parsedSlot, nil
}

// FormatRestrictedAccessSlots returns a deterministic slice of scope key strings for slots 0..maxSlots-1.
func FormatRestrictedAccessSlots(receiverID string, maxSlots int) ([]string, error) {
	if err := ValidateReceiverID(receiverID); err != nil {
		return nil, err
	}
	if maxSlots < 0 {
		return nil, fmt.Errorf("%w: maxSlots cannot be negative (%d)", ErrInvalidSlotQuantity, maxSlots)
	}
	if maxSlots > MaxRestrictedAccessSlots {
		return nil, fmt.Errorf("%w: maxSlots (%d) exceeds domain maximum (%d)", ErrInvalidSlotQuantity, maxSlots, MaxRestrictedAccessSlots)
	}
	if maxSlots == 0 {
		return []string{}, nil
	}

	scopes := make([]string, maxSlots)
	for i := 0; i < maxSlots; i++ {
		scope, err := FormatRestrictedAccessScope(receiverID, i)
		if err != nil {
			return nil, err
		}
		scopes[i] = scope
	}
	return scopes, nil
}
