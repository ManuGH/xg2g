// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

// The SQLite and memory backends must answer identically, or a census taken in
// one environment says nothing about the other.
func forEachInventoryStore(t *testing.T, fn func(t *testing.T, store StateStore, reader UnboundInventoryReader)) {
	t.Helper()

	t.Run("memory", func(t *testing.T) {
		store := NewMemoryStateStore()
		fn(t, store, store)
	})

	t.Run("sqlite", func(t *testing.T) {
		store, err := NewSqliteStore(filepath.Join(t.TempDir(), "deviceauth.sqlite"))
		if err != nil {
			t.Fatalf("open sqlite store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		fn(t, store, store)
	})
}

func seedDevice(t *testing.T, store StateStore, deviceID, ownerID string, revoked *time.Time) {
	t.Helper()
	record := &model.DeviceRecord{
		DeviceID:      deviceID,
		OwnerID:       ownerID,
		DeviceName:    deviceID,
		DeviceType:    model.DeviceTypeAndroidTV,
		PolicyProfile: "default",
		CreatedAt:     time.UnixMilli(1_000_000),
		RevokedAt:     revoked,
	}
	if err := store.PutDevice(context.Background(), record); err != nil {
		t.Fatalf("put device: %v", err)
	}
}

func seedGrant(t *testing.T, store StateStore, grantID, deviceID string, issued, expires time.Time, lastUsed, revoked *time.Time) {
	t.Helper()
	record := &model.DeviceGrantRecord{
		GrantID:    grantID,
		DeviceID:   deviceID,
		GrantHash:  "hash-" + grantID,
		IssuedAt:   issued,
		ExpiresAt:  expires,
		LastUsedAt: lastUsed,
		RevokedAt:  revoked,
	}
	if err := store.PutDeviceGrant(context.Background(), record); err != nil {
		t.Fatalf("put grant: %v", err)
	}
}

func seedSession(t *testing.T, store StateStore, sessionID, deviceID string, expires time.Time, revoked *time.Time) {
	t.Helper()
	record := &model.AccessSessionRecord{
		SessionID:     sessionID,
		SubjectID:     "owner",
		DeviceID:      deviceID,
		TokenHash:     "token-" + sessionID,
		PolicyVersion: "v1",
		Scopes:        []string{"v3:read"},
		AuthStrength:  "device",
		IssuedAt:      time.UnixMilli(1_000_000),
		ExpiresAt:     expires,
		RevokedAt:     revoked,
	}
	if err := store.PutAccessSession(context.Background(), record); err != nil {
		t.Fatalf("put session: %v", err)
	}
}

func TestUnboundInventoryEmptyStore(t *testing.T) {
	forEachInventoryStore(t, func(t *testing.T, _ StateStore, reader UnboundInventoryReader) {
		inventory, err := reader.CountUnboundDeviceAuth(context.Background(), time.UnixMilli(2_000_000))
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if !inventory.IsEmpty() {
			t.Fatalf("expected an empty census, got %+v", inventory)
		}
	})
}

func TestUnboundInventoryCountsLiveMaterialOnly(t *testing.T) {
	now := time.UnixMilli(2_000_000)
	past := time.UnixMilli(1_500_000)
	future := time.UnixMilli(9_000_000)
	revokedAt := time.UnixMilli(1_800_000)

	forEachInventoryStore(t, func(t *testing.T, store StateStore, reader UnboundInventoryReader) {
		seedDevice(t, store, "dev-1", "alice", nil)
		seedDevice(t, store, "dev-2", "alice", nil)
		seedDevice(t, store, "dev-3", "bob", nil)
		seedDevice(t, store, "dev-4", "carol", &revokedAt) // revoked: not counted

		lastUsed := time.UnixMilli(1_900_000)
		seedGrant(t, store, "g-live", "dev-1", time.UnixMilli(1_100_000), future, &lastUsed, nil)
		seedGrant(t, store, "g-newer", "dev-2", time.UnixMilli(1_200_000), future, nil, nil)
		seedGrant(t, store, "g-expired", "dev-3", time.UnixMilli(1_000_000), past, nil, nil)
		seedGrant(t, store, "g-revoked", "dev-3", time.UnixMilli(1_000_000), future, nil, &revokedAt)

		seedSession(t, store, "s-live", "dev-1", future, nil)
		seedSession(t, store, "s-expired", "dev-2", past, nil)
		seedSession(t, store, "s-revoked", "dev-3", future, &revokedAt)

		inventory, err := reader.CountUnboundDeviceAuth(context.Background(), now)
		if err != nil {
			t.Fatalf("count: %v", err)
		}

		if inventory.Devices != 3 {
			t.Errorf("devices = %d, want 3 (revoked device excluded)", inventory.Devices)
		}
		if inventory.Owners != 2 {
			t.Errorf("owners = %d, want 2 (alice, bob)", inventory.Owners)
		}
		if inventory.ActiveGrants != 2 {
			t.Errorf("activeGrants = %d, want 2 (expired and revoked excluded)", inventory.ActiveGrants)
		}
		if inventory.ActiveSessions != 1 {
			t.Errorf("activeSessions = %d, want 1", inventory.ActiveSessions)
		}
		if inventory.OldestGrantIssuedAtMs != 1_100_000 {
			t.Errorf("oldestGrantIssuedAtMs = %d, want 1100000", inventory.OldestGrantIssuedAtMs)
		}
		if inventory.NewestGrantLastUsedAtMs != 1_900_000 {
			t.Errorf("newestGrantLastUsedAtMs = %d, want 1900000", inventory.NewestGrantLastUsedAtMs)
		}
		if inventory.IsEmpty() {
			t.Error("a populated census must not report empty")
		}
	})
}

// A fleet that exists but has never phoned home is a materially different
// cutoff decision, so "never used" must stay distinguishable from "used at 0".
func TestUnboundInventoryNeverUsedGrant(t *testing.T) {
	now := time.UnixMilli(2_000_000)
	future := time.UnixMilli(9_000_000)

	forEachInventoryStore(t, func(t *testing.T, store StateStore, reader UnboundInventoryReader) {
		seedDevice(t, store, "dev-1", "alice", nil)
		seedGrant(t, store, "g-1", "dev-1", time.UnixMilli(1_100_000), future, nil, nil)

		inventory, err := reader.CountUnboundDeviceAuth(context.Background(), now)
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		if inventory.NewestGrantLastUsedAtMs != 0 {
			t.Errorf("newestGrantLastUsedAtMs = %d, want 0 for a never-used grant", inventory.NewestGrantLastUsedAtMs)
		}
		if inventory.ActiveGrants != 1 {
			t.Errorf("activeGrants = %d, want 1", inventory.ActiveGrants)
		}
	})
}
