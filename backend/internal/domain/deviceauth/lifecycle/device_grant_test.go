// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package lifecycle

import (
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

func TestClaimDeviceGrantMarksUseWithoutRotation(t *testing.T) {
	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	record := grantSeed(t, now)

	claimed, rotate, err := ClaimDeviceGrant(record, ClaimDeviceGrantInput{
		GrantHash: model.HashOpaqueSecret("grant-secret"),
		ClaimedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("claim device grant: %v", err)
	}
	if rotate {
		t.Fatal("did not expect rotation before rotate_after")
	}
	if claimed.LastUsedAt == nil || !claimed.LastUsedAt.Equal(now) {
		t.Fatalf("expected last_used_at %v, got %v", now, claimed.LastUsedAt)
	}
	if claimed.RevokedAt != nil {
		t.Fatalf("did not expect grant revocation, got %v", claimed.RevokedAt)
	}
}

func TestClaimDeviceGrantRevokesOldGrantWhenRotationIsDue(t *testing.T) {
	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	record := grantSeed(t, now)
	rotateAfter := now.Add(-1 * time.Minute)
	record.RotateAfter = &rotateAfter

	claimed, rotate, err := ClaimDeviceGrant(record, ClaimDeviceGrantInput{
		GrantHash: model.HashOpaqueSecret("grant-secret"),
		ClaimedAt: now,
	}, now)
	if err != nil {
		t.Fatalf("claim device grant: %v", err)
	}
	if !rotate {
		t.Fatal("expected rotation to be due")
	}
	if claimed.RevokedAt == nil || !claimed.RevokedAt.Equal(now) {
		t.Fatalf("expected revoked_at %v, got %v", now, claimed.RevokedAt)
	}

	_, _, err = ClaimDeviceGrant(claimed, ClaimDeviceGrantInput{
		GrantHash: model.HashOpaqueSecret("grant-secret"),
		ClaimedAt: now.Add(time.Second),
	}, now.Add(time.Second))
	if !errors.Is(err, ErrDeviceGrantAlreadyRevoked) {
		t.Fatalf("expected revoked replay error, got %v", err)
	}
}

func TestClaimDeviceGrantRejectsSecretMismatch(t *testing.T) {
	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	record := grantSeed(t, now)

	_, _, err := ClaimDeviceGrant(record, ClaimDeviceGrantInput{
		GrantHash: model.HashOpaqueSecret("wrong-secret"),
		ClaimedAt: now,
	}, now)
	if !errors.Is(err, ErrDeviceGrantSecretMismatch) {
		t.Fatalf("expected secret mismatch, got %v", err)
	}
}

func grantSeed(t *testing.T, now time.Time) model.DeviceGrantRecord {
	t.Helper()
	rotateAfter := now.Add(time.Hour)
	record, err := model.PrepareDeviceGrantRecord(model.DeviceGrantRecord{
		GrantID:     "grant-1",
		DeviceID:    "dev-1",
		GrantHash:   model.HashOpaqueSecret("grant-secret"),
		IssuedAt:    now.Add(-time.Hour),
		ExpiresAt:   now.Add(24 * time.Hour),
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		t.Fatalf("prepare device grant: %v", err)
	}
	return record
}
