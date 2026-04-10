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

func TestStartPairingBuildsPendingRecord(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	record, err := StartPairing(StartInput{
		PairingID:              "pair-1",
		PairingSecretHash:      "hash-1",
		UserCode:               " abcd 1234 ",
		QRPayload:              "xg2g://pair/pair-1?code=ABCD1234",
		DeviceType:             model.DeviceTypeAndroidTV,
		RequestedPolicyProfile: "tv-default",
		Now:                    now,
		ExpiresAt:              now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	if record.Status != model.PairingPending {
		t.Fatalf("expected pending pairing, got %s", record.Status)
	}
	if record.UserCode != "ABCD1234" {
		t.Fatalf("expected normalized user code, got %q", record.UserCode)
	}
}

func TestApprovePairingTransitionsPendingRecord(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	record := approvedSeed(t, now)
	record.Status = model.PairingPending
	record.OwnerID = ""
	record.ApprovedAt = nil

	approved, err := ApprovePairing(record, ApproveInput{
		OwnerID:               "owner-1",
		ApprovedPolicyProfile: "android-tv",
		ApprovedAt:            now.Add(1 * time.Minute),
	}, now)
	if err != nil {
		t.Fatalf("approve pairing: %v", err)
	}
	if approved.Status != model.PairingApproved {
		t.Fatalf("expected approved status, got %s", approved.Status)
	}
	if approved.OwnerID != "owner-1" {
		t.Fatalf("expected owner id to persist, got %q", approved.OwnerID)
	}
	if approved.ApprovedAt == nil {
		t.Fatal("expected approved_at to be set")
	}
}

func TestApprovePairingRejectsExpiredRecord(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	record := approvedSeed(t, now)
	record.Status = model.PairingPending
	record.OwnerID = ""
	record.ApprovedAt = nil
	record.ExpiresAt = now

	_, err := ApprovePairing(record, ApproveInput{
		OwnerID:    "owner-1",
		ApprovedAt: now,
	}, now)
	if !errors.Is(err, ErrPairingAlreadyExpired) {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestConsumePairingRequiresApprovedStateAndMatchingSecret(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	record := approvedSeed(t, now)

	_, err := ConsumePairing(record, ConsumeInput{
		PairingSecretHash: "wrong-hash",
		ConsumedAt:        now.Add(2 * time.Minute),
	}, now)
	if !errors.Is(err, ErrPairingSecretMismatch) {
		t.Fatalf("expected secret mismatch, got %v", err)
	}

	consumed, err := ConsumePairing(record, ConsumeInput{
		PairingSecretHash: "hash-1",
		ConsumedAt:        now.Add(2 * time.Minute),
	}, now)
	if err != nil {
		t.Fatalf("consume pairing: %v", err)
	}
	if consumed.Status != model.PairingConsumed {
		t.Fatalf("expected consumed status, got %s", consumed.Status)
	}
	if consumed.ConsumedAt == nil {
		t.Fatal("expected consumed_at to be set")
	}
}

func TestExpireIfElapsedTerminalizesPendingOrApprovedPairings(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	record := approvedSeed(t, now)
	record.Status = model.PairingPending
	record.OwnerID = ""
	record.ApprovedAt = nil
	record.ExpiresAt = now.Add(-1 * time.Second)

	expired, changed, err := ExpireIfElapsed(record, now)
	if err != nil {
		t.Fatalf("expire pairing: %v", err)
	}
	if !changed {
		t.Fatal("expected elapsed pairing to transition to expired")
	}
	if expired.Status != model.PairingExpired {
		t.Fatalf("expected expired status, got %s", expired.Status)
	}
}

func approvedSeed(t *testing.T, now time.Time) model.PairingRecord {
	t.Helper()
	approvedAt := now.Add(-1 * time.Minute)
	record, err := model.PreparePairingRecord(model.PairingRecord{
		PairingID:         "pair-1",
		PairingSecretHash: "hash-1",
		UserCode:          "ABCD1234",
		DeviceType:        model.DeviceTypeAndroidTV,
		OwnerID:           "owner-1",
		Status:            model.PairingApproved,
		CreatedAt:         now.Add(-5 * time.Minute),
		ExpiresAt:         now.Add(5 * time.Minute),
		ApprovedAt:        &approvedAt,
	})
	if err != nil {
		t.Fatalf("prepare pairing record: %v", err)
	}
	return record
}
