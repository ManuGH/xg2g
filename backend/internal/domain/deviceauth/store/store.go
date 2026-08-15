// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"errors"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// PairingLookupStore exposes read access to pairing enrollment truth.
type PairingLookupStore interface {
	GetPairing(ctx context.Context, pairingID string) (*model.PairingRecord, error)
	GetPairingByUserCode(ctx context.Context, userCode string) (*model.PairingRecord, error)
}

// PairingStore owns the one-time enrollment state machine backing record.
type PairingStore interface {
	PairingLookupStore
	PutPairing(ctx context.Context, record *model.PairingRecord) error
	UpdatePairing(ctx context.Context, pairingID string, fn func(*model.PairingRecord) error) (*model.PairingRecord, error)
}

// DeviceStore owns canonical enrolled device truth.
type DeviceStore interface {
	GetDevice(ctx context.Context, deviceID string) (*model.DeviceRecord, error)
	ListDevicesByOwner(ctx context.Context, ownerID string) ([]model.DeviceRecord, error)
}

// DeviceGrantStore owns revocable long-lived credentials for one device.
type DeviceGrantStore interface {
	GetDeviceGrant(ctx context.Context, grantID string) (*model.DeviceGrantRecord, error)
	GetActiveDeviceGrantByDevice(ctx context.Context, deviceID string) (*model.DeviceGrantRecord, error)
	ListDeviceGrantsByDevice(ctx context.Context, deviceID string) ([]model.DeviceGrantRecord, error)
}

// AccessSessionStore owns short-lived effective auth context records.
type AccessSessionStore interface {
	GetAccessSession(ctx context.Context, sessionID string) (*model.AccessSessionRecord, error)
	GetAccessSessionByTokenHash(ctx context.Context, tokenHash string) (*model.AccessSessionRecord, error)
	ListAccessSessionsByDevice(ctx context.Context, deviceID string) ([]model.AccessSessionRecord, error)
}

// StateStore is the persistent source of truth for device enrollment and auth state.
type StateStore interface {
	PairingStore
	DeviceStore
	DeviceGrantStore
	AccessSessionStore
}
