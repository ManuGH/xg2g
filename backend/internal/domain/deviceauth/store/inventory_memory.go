// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

// CountUnboundDeviceAuth mirrors the SQLite census so tests and the memory
// backend answer identically. See ADR-032 Phase 0.
func (m *MemoryStateStore) CountUnboundDeviceAuth(_ context.Context, now time.Time) (model.UnboundInventory, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inventory := model.UnboundInventory{}
	owners := make(map[string]struct{})

	for _, device := range m.devices {
		if device.RevokedAt != nil {
			continue
		}
		inventory.Devices++
		owners[device.OwnerID] = struct{}{}
	}
	inventory.Owners = len(owners)

	for _, grant := range m.grants {
		if grant.RevokedAt != nil || !grant.ExpiresAt.After(now) {
			continue
		}
		inventory.ActiveGrants++

		issued := grant.IssuedAt.UnixMilli()
		if inventory.OldestGrantIssuedAtMs == 0 || issued < inventory.OldestGrantIssuedAtMs {
			inventory.OldestGrantIssuedAtMs = issued
		}
		if grant.LastUsedAt != nil {
			used := grant.LastUsedAt.UnixMilli()
			if used > inventory.NewestGrantLastUsedAtMs {
				inventory.NewestGrantLastUsedAtMs = used
			}
		}
	}

	for _, session := range m.sessions {
		if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
			continue
		}
		inventory.ActiveSessions++
	}

	return inventory, nil
}
