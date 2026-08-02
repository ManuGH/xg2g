// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package lease

import (
	"fmt"
	"time"
)

// ID uniquely identifies a lease instance.
type ID string

// Owner identifies the component, worker, or session holding the lease.
type Owner string

// Scope identifies the target resource category or key (e.g., "tuner:0", "encoder:nvenc:0").
type Scope string

// State represents the current lifecycle state of a lease.
type State string

const (
	StateAcquired State = "ACQUIRED"
	StateReleased State = "RELEASED"
	StateExpired  State = "EXPIRED"
	StateRevoked  State = "REVOKED"
)

// ReasonCode explains the domain cause for a state transition or rejection.
type ReasonCode string

const (
	ReasonAcquired            ReasonCode = "LEASE_ACQUIRED"
	ReasonReleasedByOwner     ReasonCode = "LEASE_RELEASED_BY_OWNER"
	ReasonExpired             ReasonCode = "LEASE_EXPIRED"
	ReasonRevokedByAdmin      ReasonCode = "LEASE_REVOKED_BY_ADMIN"
	ReasonPreempted           ReasonCode = "LEASE_PREEMPTED"
	ReasonOwnerMismatch       ReasonCode = "LEASE_OWNER_MISMATCH"
	ReasonAlreadyReleased     ReasonCode = "LEASE_ALREADY_RELEASED"
	ReasonAlreadyExpired      ReasonCode = "LEASE_ALREADY_EXPIRED"
	ReasonScopeConflict       ReasonCode = "LEASE_SCOPE_CONFLICT"
	ReasonInvalidTTL          ReasonCode = "LEASE_INVALID_TTL"
	ReasonInvalidOwner        ReasonCode = "LEASE_INVALID_OWNER"
	ReasonInvalidScope        ReasonCode = "LEASE_INVALID_SCOPE"
	ReasonNotFound            ReasonCode = "LEASE_NOT_FOUND"
	ReasonManagerClosed               ReasonCode = "LEASE_MANAGER_CLOSED"
	ReasonRollback                    ReasonCode = "LEASE_ROLLBACK"
	ReasonReconciliationOrphanCleanup ReasonCode = "LEASE_RECONCILIATION_ORPHAN_CLEANUP"
	ReasonReconciliationRecoveryRequired ReasonCode = "LEASE_RECONCILIATION_RECOVERY_REQUIRED"
)

// Lease represents a time-bounded reservation for a specific resource scope.
type Lease struct {
	ID         ID         `json:"id"`
	Owner      Owner      `json:"owner"`
	Scope      Scope      `json:"scope"`
	State      State      `json:"state"`
	AcquiredAt time.Time  `json:"acquired_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
	ReasonCode ReasonCode `json:"reason_code"`
}

// IsActive returns true if the lease is in StateAcquired and has not passed its expiration time.
func (l *Lease) IsActive(now time.Time) bool {
	if l == nil {
		return false
	}
	return l.State == StateAcquired && now.Before(l.ExpiresAt)
}

// String provides a human-readable representation of a Lease.
func (l *Lease) String() string {
	if l == nil {
		return "<nil lease>"
	}
	return fmt.Sprintf("Lease[id=%s, owner=%s, scope=%s, state=%s, reason=%s, expiresAt=%s]",
		l.ID, l.Owner, l.Scope, l.State, l.ReasonCode, l.ExpiresAt.Format(time.RFC3339))
}
