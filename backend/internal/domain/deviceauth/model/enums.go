// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import "strings"

// DeviceType is the backend-truth classification for one enrolled client.
// It is policy-relevant only when the backend explicitly chooses to use it.
type DeviceType string

const (
	DeviceTypeAndroidPhone  DeviceType = "android_phone"
	DeviceTypeAndroidTablet DeviceType = "android_tablet"
	DeviceTypeAndroidTV     DeviceType = "android_tv"
	DeviceTypeBrowser       DeviceType = "browser"
	DeviceTypeUnknown       DeviceType = "unknown"
)

// PairingStatus captures the one-way enrollment lifecycle for a pending device.
type PairingStatus string

const (
	PairingPending  PairingStatus = "pending"
	PairingApproved PairingStatus = "approved"
	PairingExpired  PairingStatus = "expired"
	PairingConsumed PairingStatus = "consumed"
	PairingRevoked  PairingStatus = "revoked"
)

// NormalizeDeviceType folds free-form transport values into backend-truth enums.
func NormalizeDeviceType(value DeviceType) DeviceType {
	switch strings.ToLower(strings.TrimSpace(string(value))) {
	case string(DeviceTypeAndroidPhone):
		return DeviceTypeAndroidPhone
	case string(DeviceTypeAndroidTablet):
		return DeviceTypeAndroidTablet
	case string(DeviceTypeAndroidTV):
		return DeviceTypeAndroidTV
	case string(DeviceTypeBrowser):
		return DeviceTypeBrowser
	default:
		return DeviceTypeUnknown
	}
}

// NormalizePairingStatus rejects transport drift and defaults zero values to pending.
func NormalizePairingStatus(value PairingStatus) PairingStatus {
	switch normalized := strings.ToLower(strings.TrimSpace(string(value))); normalized {
	case "", string(PairingPending):
		return PairingPending
	case string(PairingApproved):
		return PairingApproved
	case string(PairingExpired):
		return PairingExpired
	case string(PairingConsumed):
		return PairingConsumed
	case string(PairingRevoked):
		return PairingRevoked
	default:
		return PairingStatus(normalized)
	}
}

func (s PairingStatus) IsTerminal() bool {
	switch NormalizePairingStatus(s) {
	case PairingExpired, PairingConsumed, PairingRevoked:
		return true
	default:
		return false
	}
}
