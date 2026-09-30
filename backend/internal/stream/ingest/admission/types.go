// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package admission

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrCapacityExceeded is returned when the configured maximum upstream connections for an account is reached.
	ErrCapacityExceeded = errors.New("both upstream connections are in use; stop an active stream or select a channel already being watched")
	// ErrAccountNotFound is returned when an operation is attempted on an unconfigured account.
	ErrAccountNotFound = errors.New("provider account not configured")
	// ErrInvalidRequest is returned when required parameters are missing or invalid.
	ErrInvalidRequest = errors.New("invalid admission request parameters")
	// ErrLeaseClosed is returned when an operation is performed on an already released lease.
	ErrLeaseClosed = errors.New("slot lease has already been released")
	// ErrSlotFenced is returned when attempting to acquire or operate on a fenced slot undergoing teardown.
	ErrSlotFenced = errors.New("upstream slot is fenced and undergoing teardown")
	// ErrControllerClosed is returned when the admission controller is closed.
	ErrControllerClosed = errors.New("admission controller is closed")
)

// AccountConfig defines capacity limits and timings for a provider account.
type AccountConfig struct {
	AccountID        string        // Opaque account identifier
	MaxSlots         int           // Maximum simultaneous upstream streams (e.g. 2)
	WarmHoldDuration time.Duration // Duration to hold idle upstream open before tearing down (0 to disable)
}

// AcquireRequest parameters for acquiring an upstream stream slot.
type AcquireRequest struct {
	AccountID   string // Opaque account identifier
	SourceID    string // Opaque source/channel identifier
	ClientID    string // Downstream client session/viewer ID
	IsPreflight bool   // True if this is an explicit preflight probe
}

// SwitchRequest parameters for switching a client from an old source to a new source.
type SwitchRequest struct {
	AccountID   string // Opaque account identifier
	OldSourceID string // Current source identifier
	NewSourceID string // Target source identifier
	ClientID    string // Downstream client session/viewer ID
}

// SourceUsage provides sanitized status for an active upstream stream.
type SourceUsage struct {
	SourceID    string    // Opaque source identifier
	Subscribers int       // Number of active downstream subscribers
	IsHolding   bool      // True if idle in warm-hold duration with 0 subscribers
	AllocatedAt time.Time // Timestamp when slot was first allocated
}

// AccountUsage provides sanitized status for an account's slot budget.
type AccountUsage struct {
	AccountID     string        // Opaque account identifier
	ActiveSlots   int           // Number of currently allocated upstream slots
	MaxSlots      int           // Configured maximum simultaneous slots
	TotalViewers  int           // Total downstream clients attached across all slots
	ActiveSources []SourceUsage // List of active sources (no credentials/URLs exposed)
}

// SlotLease represents an active reservation on an upstream slot.
type SlotLease interface {
	AccountID() string
	SourceID() string
	ClientID() string
	IsShared() bool
	Release() error
}

// SlotBudget defines the contract for atomic provider upstream slot admission.
type SlotBudget interface {
	Acquire(ctx context.Context, req AcquireRequest) (SlotLease, error)
	Switch(ctx context.Context, req SwitchRequest) (SlotLease, error)
	Usage(accountID string) (AccountUsage, error)
	Close() error
}
