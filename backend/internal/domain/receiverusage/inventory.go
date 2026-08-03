// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

var (
	ErrRestrictedAccessInventoryInconsistent = errors.New("restricted access inventory is inconsistent")
)

type InventoryIssueKind string

const (
	IssueNonCanonicalScope InventoryIssueKind = "NON_CANONICAL_SCOPE"
	IssueSlotOutOfRange    InventoryIssueKind = "SLOT_OUT_OF_RANGE"
	IssueReceiverMismatch  InventoryIssueKind = "RECEIVER_MISMATCH"
	IssueMissingOwner      InventoryIssueKind = "MISSING_OWNER"
	IssueDuplicateSlot     InventoryIssueKind = "DUPLICATE_SLOT"
)

type InventoryIssue struct {
	Kind        InventoryIssueKind
	Scope       string
	Description string
}

type RestrictedAccessLease struct {
	Scope      string
	ReceiverID string
	Slot       int
	Owner      string
	Key        string
	ExpiresAt  time.Time
}

type RestrictedAccessInventory struct {
	ReceiverID       string
	Capacity         int
	Active           []RestrictedAccessLease
	Expired          []RestrictedAccessLease
	FreeSlots        []int
	Inconsistencies  []InventoryIssue
	ObservedAt       time.Time
	InventoryTrusted bool
}

// Inventory reads all leases matching the receiver's restricted access scopes from store and builds a RestrictedAccessInventory snapshot. Read-only operation.
func Inventory(ctx context.Context, st store.LeaseStore, receiverID string, capacity int, now time.Time) (RestrictedAccessInventory, error) {
	if err := ValidateReceiverID(receiverID); err != nil {
		return RestrictedAccessInventory{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if capacity < 0 || capacity > MaxRestrictedAccessSlots {
		return RestrictedAccessInventory{}, fmt.Errorf("%w: invalid capacity (%d)", ErrInvalidInput, capacity)
	}

	allLeases, err := st.ListLeases(ctx)
	if err != nil {
		return RestrictedAccessInventory{}, err
	}

	inv := RestrictedAccessInventory{
		ReceiverID:       receiverID,
		Capacity:         capacity,
		Active:           []RestrictedAccessLease{},
		Expired:          []RestrictedAccessLease{},
		FreeSlots:        []int{},
		Inconsistencies:  []InventoryIssue{},
		ObservedAt:       now,
		InventoryTrusted: true,
	}

	prefix := RestrictedAccessScopePrefix + receiverID + RestrictedAccessScopeMiddle
	occupiedSlots := make(map[int]bool)
	slotOwnerMap := make(map[int]string)

	for _, l := range allLeases {
		key := l.Key()
		// Match leases belonging to this receiver
		if !strings.HasPrefix(key, RestrictedAccessScopePrefix) {
			continue
		}

		recID, slot, parseErr := ParseRestrictedAccessScope(key)
		if parseErr != nil {
			// Check if key belongs to this receiver before marking issue
			if strings.HasPrefix(key, prefix) || strings.Contains(key, ":restricted-access:") {
				inv.Inconsistencies = append(inv.Inconsistencies, InventoryIssue{
					Kind:        IssueNonCanonicalScope,
					Scope:       key,
					Description: parseErr.Error(),
				})
			}
			continue
		}

		if recID != receiverID {
			// Belongs to another receiver, isolate
			continue
		}

		if l.Owner() == "" {
			inv.Inconsistencies = append(inv.Inconsistencies, InventoryIssue{
				Kind:        IssueMissingOwner,
				Scope:       key,
				Description: "lease owner is empty",
			})
		}

		rLease := RestrictedAccessLease{
			Scope:      key,
			ReceiverID: recID,
			Slot:       slot,
			Owner:      l.Owner(),
			Key:        key,
			ExpiresAt:  l.ExpiresAt(),
		}

		isExpired := !now.Before(l.ExpiresAt())
		if isExpired {
			inv.Expired = append(inv.Expired, rLease)
		} else {
			inv.Active = append(inv.Active, rLease)
			if occupiedSlots[slot] {
				inv.Inconsistencies = append(inv.Inconsistencies, InventoryIssue{
					Kind:        IssueDuplicateSlot,
					Scope:       key,
					Description: fmt.Sprintf("slot %d is claimed by multiple active leases (%s and %s)", slot, slotOwnerMap[slot], l.Owner()),
				})
			}
			occupiedSlots[slot] = true
			slotOwnerMap[slot] = l.Owner()

			if slot >= capacity {
				inv.Inconsistencies = append(inv.Inconsistencies, InventoryIssue{
					Kind:        IssueSlotOutOfRange,
					Scope:       key,
					Description: fmt.Sprintf("active slot %d exceeds current capacity limit %d", slot, capacity),
				})
			}
		}
	}

	// Calculate free slots for slots 0..capacity-1
	for slot := 0; slot < capacity; slot++ {
		if !occupiedSlots[slot] {
			inv.FreeSlots = append(inv.FreeSlots, slot)
		}
	}
	sort.Ints(inv.FreeSlots)

	if len(inv.Inconsistencies) > 0 {
		inv.InventoryTrusted = false
	}

	return inv, nil
}

// CleanupExpired safely releases expired leases for target receiver. Mutating operation.
func CleanupExpired(ctx context.Context, st store.LeaseStore, receiverID string, capacity int, now time.Time) (int, error) {
	inv, err := Inventory(ctx, st, receiverID, capacity, now)
	if err != nil {
		return 0, err
	}

	releasedCount := 0
	for _, expLease := range inv.Expired {
		// Double check lease is still expired in authoritative store before releasing
		l, ok, getErr := st.GetLease(ctx, expLease.Scope)
		if getErr != nil || !ok {
			continue
		}
		// If lease was renewed in the meantime or owner changed, do not delete
		if l.Owner() != expLease.Owner || now.Before(l.ExpiresAt()) {
			continue
		}

		if relErr := st.ReleaseLease(ctx, expLease.Scope, expLease.Owner); relErr == nil {
			releasedCount++
		}
	}

	return releasedCount, nil
}
