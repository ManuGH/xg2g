// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package receiverusage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/store"
)

var (
	ErrRestrictedAccessLimitExceeded = errors.New("restricted access capacity limit exceeded")
	ErrInvalidInput                  = errors.New("invalid input for restricted access controller")
)

type RestrictedAccessLeaseHandle struct {
	ReceiverID string
	Scope      string
	Slot       int
	Owner      string
	LeaseID    string
}

type RestrictedAccessController struct {
	store store.LeaseStore
}

func NewRestrictedAccessController(st store.LeaseStore) *RestrictedAccessController {
	return &RestrictedAccessController{
		store: st,
	}
}

func (c *RestrictedAccessController) Acquire(ctx context.Context, receiverID, owner string, capacity int, ttl time.Duration) (RestrictedAccessLeaseHandle, error) {
	if err := ValidateReceiverID(receiverID); err != nil {
		return RestrictedAccessLeaseHandle{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if capacity <= 0 || capacity > MaxRestrictedAccessSlots {
		return RestrictedAccessLeaseHandle{}, fmt.Errorf("%w: invalid capacity (%d)", ErrInvalidInput, capacity)
	}
	if owner == "" {
		return RestrictedAccessLeaseHandle{}, fmt.Errorf("%w: owner cannot be empty", ErrInvalidInput)
	}
	if ttl <= 0 {
		return RestrictedAccessLeaseHandle{}, fmt.Errorf("%w: ttl must be positive (%v)", ErrInvalidInput, ttl)
	}

	scopes, err := FormatRestrictedAccessSlots(receiverID, capacity)
	if err != nil {
		return RestrictedAccessLeaseHandle{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	for slot, scopeKey := range scopes {
		l, acquired, err := c.store.TryAcquireLease(ctx, scopeKey, owner, ttl)
		if err != nil {
			// Technical store error: return immediately unchanged
			return RestrictedAccessLeaseHandle{}, err
		}
		if acquired {
			return RestrictedAccessLeaseHandle{
				ReceiverID: receiverID,
				Scope:      scopeKey,
				Slot:       slot,
				Owner:      owner,
				LeaseID:    l.Key(),
			}, nil
		}
	}

	return RestrictedAccessLeaseHandle{}, ErrRestrictedAccessLimitExceeded
}

func (c *RestrictedAccessController) Renew(ctx context.Context, handle RestrictedAccessLeaseHandle, ttl time.Duration) error {
	if handle.Scope == "" || handle.Owner == "" {
		return fmt.Errorf("%w: handle contains empty scope or owner", ErrInvalidInput)
	}
	if ttl <= 0 {
		return fmt.Errorf("%w: ttl must be positive (%v)", ErrInvalidInput, ttl)
	}

	_, renewed, err := c.store.RenewLease(ctx, handle.Scope, handle.Owner, ttl)
	if err != nil {
		return err
	}
	if !renewed {
		return fmt.Errorf("%w: lease renew failed for scope %s and owner %s", ErrRestrictedAccessLimitExceeded, handle.Scope, handle.Owner)
	}
	return nil
}

func (c *RestrictedAccessController) Release(ctx context.Context, handle RestrictedAccessLeaseHandle) error {
	if handle.Scope == "" || handle.Owner == "" {
		return fmt.Errorf("%w: handle contains empty scope or owner", ErrInvalidInput)
	}
	return c.store.ReleaseLease(ctx, handle.Scope, handle.Owner)
}
