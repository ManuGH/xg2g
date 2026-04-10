// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"errors"
	"testing"
	"time"
)

func TestPreparePairingRecord_NormalizesEnrollmentTruth(t *testing.T) {
	createdAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(5 * time.Minute)

	record, err := PreparePairingRecord(PairingRecord{
		PairingID:              " pairing-1 ",
		PairingSecretHash:      " secret-hash ",
		UserCode:               " abcd-1234 ",
		DeviceType:             "ANDROID_TV",
		RequestedPolicyProfile: " tv-default ",
		CreatedAt:              createdAt,
		ExpiresAt:              expiresAt,
	})
	if err != nil {
		t.Fatalf("prepare pairing record: %v", err)
	}

	if record.PairingID != "pairing-1" {
		t.Fatalf("expected trimmed pairing id, got %q", record.PairingID)
	}
	if record.UserCode != "ABCD1234" {
		t.Fatalf("expected canonical user code, got %q", record.UserCode)
	}
	if record.DeviceType != DeviceTypeAndroidTV {
		t.Fatalf("expected android_tv device type, got %q", record.DeviceType)
	}
	if record.DeviceName != "Android TV" {
		t.Fatalf("expected default device name for TV, got %q", record.DeviceName)
	}
	if record.Status != PairingPending {
		t.Fatalf("expected default pending status, got %q", record.Status)
	}
}

func TestPreparePairingRecord_ApprovedAndConsumedNeedTimestamps(t *testing.T) {
	createdAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(5 * time.Minute)
	approvedAt := createdAt.Add(2 * time.Minute)
	consumedAt := approvedAt.Add(30 * time.Second)

	_, err := PreparePairingRecord(PairingRecord{
		PairingID:         "pairing-2",
		PairingSecretHash: "secret-hash",
		UserCode:          "ABCD1234",
		Status:            PairingApproved,
		CreatedAt:         createdAt,
		ExpiresAt:         expiresAt,
	})
	if !errors.Is(err, ErrPairingApprovalRequired) {
		t.Fatalf("expected approval invariant error, got %v", err)
	}

	_, err = PreparePairingRecord(PairingRecord{
		PairingID:         "pairing-3",
		PairingSecretHash: "secret-hash",
		UserCode:          "ABCD1234",
		OwnerID:           "owner-1",
		Status:            PairingConsumed,
		CreatedAt:         createdAt,
		ExpiresAt:         expiresAt,
		ApprovedAt:        &approvedAt,
	})
	if !errors.Is(err, ErrPairingConsumedAt) {
		t.Fatalf("expected consumed_at invariant error, got %v", err)
	}

	record, err := PreparePairingRecord(PairingRecord{
		PairingID:         "pairing-4",
		PairingSecretHash: "secret-hash",
		UserCode:          "ABCD1234",
		OwnerID:           "owner-1",
		Status:            PairingConsumed,
		CreatedAt:         createdAt,
		ExpiresAt:         expiresAt,
		ApprovedAt:        &approvedAt,
		ConsumedAt:        &consumedAt,
	})
	if err != nil {
		t.Fatalf("expected consumed pairing to normalize, got %v", err)
	}
	if record.Status != PairingConsumed {
		t.Fatalf("expected consumed status, got %q", record.Status)
	}
}

func TestPreparePairingRecord_RejectsUnknownStatusAndBackwardsTimestamps(t *testing.T) {
	createdAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := createdAt.Add(5 * time.Minute)
	approvedAt := createdAt.Add(-1 * time.Minute)

	_, err := PreparePairingRecord(PairingRecord{
		PairingID:         "pairing-5",
		PairingSecretHash: "secret-hash",
		UserCode:          "ABCD1234",
		Status:            "mystery",
		CreatedAt:         createdAt,
		ExpiresAt:         expiresAt,
	})
	if !errors.Is(err, ErrInvalidPairingStatus) {
		t.Fatalf("expected invalid status error, got %v", err)
	}

	_, err = PreparePairingRecord(PairingRecord{
		PairingID:         "pairing-6",
		PairingSecretHash: "secret-hash",
		UserCode:          "ABCD1234",
		OwnerID:           "owner-1",
		Status:            PairingApproved,
		CreatedAt:         createdAt,
		ExpiresAt:         expiresAt,
		ApprovedAt:        &approvedAt,
	})
	if !errors.Is(err, ErrInvalidPairingApprovedAt) {
		t.Fatalf("expected invalid approved_at error, got %v", err)
	}
}

func TestPairingRecord_StateGuardsFailClosed(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	approved := PairingRecord{
		Status:    PairingApproved,
		ExpiresAt: now.Add(5 * time.Minute),
	}
	if !approved.CanExchange(now) {
		t.Fatal("expected approved non-expired pairing to be exchangeable")
	}

	expired := PairingRecord{
		Status:    PairingApproved,
		ExpiresAt: now,
	}
	if expired.CanExchange(now) {
		t.Fatal("expired pairing must not remain exchangeable")
	}

	consumedAt := now.Add(-1 * time.Minute)
	consumed := PairingRecord{
		Status:     PairingApproved,
		ExpiresAt:  now.Add(5 * time.Minute),
		ConsumedAt: &consumedAt,
	}
	if consumed.CanExchange(now) {
		t.Fatal("consumed pairing must not remain exchangeable")
	}
}

func TestPrepareDeviceRecord_NormalizesAndClonesCapabilities(t *testing.T) {
	createdAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	capabilities := map[string]any{" remoteControl ": true}

	record, err := PrepareDeviceRecord(DeviceRecord{
		DeviceID:     " device-1 ",
		OwnerID:      " owner-1 ",
		DeviceType:   "android_phone",
		Capabilities: capabilities,
		CreatedAt:    createdAt,
	})
	if err != nil {
		t.Fatalf("prepare device record: %v", err)
	}

	if record.DeviceName != "Android Phone" {
		t.Fatalf("expected default Android Phone name, got %q", record.DeviceName)
	}
	if _, ok := record.Capabilities["remoteControl"]; !ok {
		t.Fatalf("expected trimmed capability key, got %#v", record.Capabilities)
	}

	capabilities["remoteControl"] = false
	if record.Capabilities["remoteControl"] != true {
		t.Fatal("expected cloned capabilities map to be isolated from caller mutations")
	}
}

func TestPrepareDeviceRecord_RejectsBackwardsTimestamps(t *testing.T) {
	createdAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	lastSeenAt := createdAt.Add(-1 * time.Minute)

	_, err := PrepareDeviceRecord(DeviceRecord{
		DeviceID:   "device-1",
		OwnerID:    "owner-1",
		CreatedAt:  createdAt,
		LastSeenAt: &lastSeenAt,
	})
	if !errors.Is(err, ErrInvalidDeviceLastSeenAt) {
		t.Fatalf("expected invalid last_seen_at error, got %v", err)
	}
}

func TestDeviceRecord_CanIssueSessionsStopsAtRevocation(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-1 * time.Minute)

	record := DeviceRecord{RevokedAt: &revokedAt}
	if record.CanIssueSessions(now) {
		t.Fatal("revoked device must not issue new sessions")
	}
}

func TestPrepareDeviceGrantRecord_EnforcesBindingAndTemporalBounds(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(24 * time.Hour)
	rotateAfter := issuedAt.Add(12 * time.Hour)

	record, err := PrepareDeviceGrantRecord(DeviceGrantRecord{
		GrantID:     " grant-1 ",
		DeviceID:    " device-1 ",
		GrantHash:   " grant-hash ",
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		t.Fatalf("prepare device grant: %v", err)
	}
	if record.GrantID != "grant-1" || record.DeviceID != "device-1" {
		t.Fatalf("expected trimmed grant bindings, got %#v", record)
	}

	badRotateAfter := expiresAt
	_, err = PrepareDeviceGrantRecord(DeviceGrantRecord{
		GrantID:     "grant-2",
		DeviceID:    "device-1",
		GrantHash:   "grant-hash",
		IssuedAt:    issuedAt,
		ExpiresAt:   expiresAt,
		RotateAfter: &badRotateAfter,
	})
	if !errors.Is(err, ErrInvalidGrantRotation) {
		t.Fatalf("expected rotation invariant error, got %v", err)
	}
}

func TestDeviceGrantRecord_UsabilityAndRotationAreFailClosed(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	rotateAfter := now.Add(-1 * time.Minute)
	revokedAt := now.Add(-30 * time.Second)

	active := DeviceGrantRecord{
		ExpiresAt:   now.Add(5 * time.Minute),
		RotateAfter: &rotateAfter,
	}
	if !active.IsUsable(now) {
		t.Fatal("expected non-expired, non-revoked grant to remain usable")
	}
	if !active.NeedsRotation(now) {
		t.Fatal("expected rotate_after in the past to trigger rotation")
	}

	revoked := DeviceGrantRecord{
		ExpiresAt: now.Add(5 * time.Minute),
		RevokedAt: &revokedAt,
	}
	if revoked.IsUsable(now) {
		t.Fatal("revoked grant must not remain usable")
	}
}

func TestPrepareAccessSessionRecord_NormalizesScopesAndRejectsBrokenIdentity(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := issuedAt.Add(15 * time.Minute)

	record, err := PrepareAccessSessionRecord(AccessSessionRecord{
		SessionID: " session-1 ",
		SubjectID: " owner-1 ",
		DeviceID:  " device-1 ",
		TokenHash: " session-hash ",
		Scopes:    []string{" stream:read ", " device:write", "stream:read"},
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("prepare access session: %v", err)
	}

	if len(record.Scopes) != 2 || record.Scopes[0] != "device:write" || record.Scopes[1] != "stream:read" {
		t.Fatalf("expected canonical scopes, got %#v", record.Scopes)
	}

	_, err = PrepareAccessSessionRecord(AccessSessionRecord{
		SessionID: "session-2",
		DeviceID:  "device-1",
		TokenHash: "session-hash",
		IssuedAt:  issuedAt,
		ExpiresAt: expiresAt,
	})
	if !errors.Is(err, ErrInvalidSessionSubjectID) {
		t.Fatalf("expected missing subject invariant error, got %v", err)
	}
}

func TestAccessSessionRecord_IsActiveHonorsExpiryAndRevocation(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	revokedAt := now.Add(-1 * time.Minute)

	active := AccessSessionRecord{ExpiresAt: now.Add(5 * time.Minute)}
	if !active.IsActive(now) {
		t.Fatal("expected future-expiring session to remain active")
	}

	expired := AccessSessionRecord{ExpiresAt: now}
	if expired.IsActive(now) {
		t.Fatal("expired session must not remain active")
	}

	revoked := AccessSessionRecord{
		ExpiresAt: now.Add(5 * time.Minute),
		RevokedAt: &revokedAt,
	}
	if revoked.IsActive(now) {
		t.Fatal("revoked session must not remain active")
	}
}
