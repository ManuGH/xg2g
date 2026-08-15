// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func captureCensus(t *testing.T, seed func(t *testing.T, store deviceauthstore.StateStore), now time.Time) map[string]any {
	t.Helper()

	store := deviceauthstore.NewMemoryStateStore()
	if seed != nil {
		seed(t, store)
	}

	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logUnboundDeviceAuthCensus(context.Background(), store, logger, now)

	if buf.Len() == 0 {
		t.Fatal("census produced no log line")
	}

	var fields map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &fields); err != nil {
		t.Fatalf("census log is not valid JSON: %v (%s)", err, buf.String())
	}
	return fields
}

func TestCensusReportsEmptyFleetAtInfo(t *testing.T) {
	fields := captureCensus(t, nil, time.UnixMilli(2_000_000))

	if fields["level"] != "info" {
		t.Errorf("level = %v, want info for an empty fleet", fields["level"])
	}
	if fields["devices"] != float64(0) {
		t.Errorf("devices = %v, want 0", fields["devices"])
	}
	if fields["adr"] != "032" {
		t.Errorf("adr = %v, want 032", fields["adr"])
	}
}

// A remaining legacy fleet has a deadline attached, so it must not be filed
// away as background information.
func TestCensusReportsRemainingFleetAtWarn(t *testing.T) {
	now := time.UnixMilli(2_000_000)
	lastUsed := time.UnixMilli(1_900_000)

	fields := captureCensus(t, func(t *testing.T, store deviceauthstore.StateStore) {
		ctx := context.Background()
		if err := store.PutDevice(ctx, &model.DeviceRecord{
			DeviceID:      "dev-1",
			OwnerID:       "alice",
			DeviceName:    "Shield",
			DeviceType:    model.DeviceTypeAndroidTV,
			PolicyProfile: "default",
			CreatedAt:     time.UnixMilli(1_000_000),
		}); err != nil {
			t.Fatalf("put device: %v", err)
		}
		if err := store.PutDeviceGrant(ctx, &model.DeviceGrantRecord{
			GrantID:    "g-1",
			DeviceID:   "dev-1",
			GrantHash:  "hash",
			IssuedAt:   time.UnixMilli(1_100_000),
			ExpiresAt:  time.UnixMilli(9_000_000),
			LastUsedAt: &lastUsed,
		}); err != nil {
			t.Fatalf("put grant: %v", err)
		}
	}, now)

	if fields["level"] != "warn" {
		t.Errorf("level = %v, want warn while unbound devices remain", fields["level"])
	}
	if fields["devices"] != float64(1) {
		t.Errorf("devices = %v, want 1", fields["devices"])
	}
	if fields["active_grants"] != float64(1) {
		t.Errorf("active_grants = %v, want 1", fields["active_grants"])
	}
	if fields["binding"] != "legacy_unbound" {
		t.Errorf("binding = %v, want legacy_unbound", fields["binding"])
	}
	if _, ok := fields["newest_grant_last_used_at"]; !ok {
		t.Error("expected newest_grant_last_used_at to be reported")
	}
}

// "Never used" must be visible as such rather than as an absent field, because
// it argues for a shorter cutoff window.
func TestCensusMarksNeverUsedGrantsExplicitly(t *testing.T) {
	now := time.UnixMilli(2_000_000)

	fields := captureCensus(t, func(t *testing.T, store deviceauthstore.StateStore) {
		ctx := context.Background()
		if err := store.PutDevice(ctx, &model.DeviceRecord{
			DeviceID:      "dev-1",
			OwnerID:       "alice",
			DeviceName:    "Shield",
			DeviceType:    model.DeviceTypeAndroidTV,
			PolicyProfile: "default",
			CreatedAt:     time.UnixMilli(1_000_000),
		}); err != nil {
			t.Fatalf("put device: %v", err)
		}
		if err := store.PutDeviceGrant(ctx, &model.DeviceGrantRecord{
			GrantID:   "g-1",
			DeviceID:  "dev-1",
			GrantHash: "hash",
			IssuedAt:  time.UnixMilli(1_100_000),
			ExpiresAt: time.UnixMilli(9_000_000),
		}); err != nil {
			t.Fatalf("put grant: %v", err)
		}
	}, now)

	if used, ok := fields["any_grant_ever_used"]; !ok || used != false {
		t.Errorf("any_grant_ever_used = %v (present=%v), want explicit false", used, ok)
	}
	if _, ok := fields["newest_grant_last_used_at"]; ok {
		t.Error("newest_grant_last_used_at must be absent when nothing was ever used")
	}
}

func TestCensusVerifiesInventoryIsEmptyContract(t *testing.T) {
	if !(model.UnboundInventory{}).IsEmpty() {
		t.Error("a zero inventory must report empty")
	}
	if (model.UnboundInventory{Devices: 1}).IsEmpty() {
		t.Error("an inventory with a device must not report empty")
	}
}
