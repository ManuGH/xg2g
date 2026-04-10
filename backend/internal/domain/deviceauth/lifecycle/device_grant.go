// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package lifecycle

import (
	"errors"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

var (
	ErrDeviceGrantSecretMismatch = errors.New("device grant secret does not match")
	ErrDeviceGrantAlreadyExpired = errors.New("device grant is expired")
	ErrDeviceGrantAlreadyRevoked = errors.New("device grant is revoked")
)

type ClaimDeviceGrantInput struct {
	GrantHash string
	ClaimedAt time.Time
}

// ClaimDeviceGrant marks a device grant use atomically at the persistence
// boundary. If rotation is due, the old grant is revoked before callers issue
// replacement credentials so a replayed old grant cannot win a parallel race.
func ClaimDeviceGrant(record model.DeviceGrantRecord, input ClaimDeviceGrantInput, now time.Time) (model.DeviceGrantRecord, bool, error) {
	record, err := model.PrepareDeviceGrantRecord(record)
	if err != nil {
		return model.DeviceGrantRecord{}, false, err
	}
	if strings.TrimSpace(input.GrantHash) == "" || record.GrantHash != strings.TrimSpace(input.GrantHash) {
		return model.DeviceGrantRecord{}, false, ErrDeviceGrantSecretMismatch
	}
	if record.IsRevoked() {
		return model.DeviceGrantRecord{}, false, ErrDeviceGrantAlreadyRevoked
	}
	if !record.IsUsable(now) {
		return model.DeviceGrantRecord{}, false, ErrDeviceGrantAlreadyExpired
	}

	claimedAt := input.ClaimedAt.UTC()
	if claimedAt.IsZero() {
		claimedAt = now.UTC()
	}
	record.LastUsedAt = &claimedAt

	needsRotation := record.NeedsRotation(now)
	if needsRotation {
		revokedAt := claimedAt
		record.RevokedAt = &revokedAt
	}

	prepared, err := model.PrepareDeviceGrantRecord(record)
	if err != nil {
		return model.DeviceGrantRecord{}, false, err
	}
	return prepared, needsRotation, nil
}
