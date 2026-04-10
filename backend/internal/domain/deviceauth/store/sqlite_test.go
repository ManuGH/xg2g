// Copyright (c) 2025 ManuGH
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

func TestSqliteStore_PersistsRecordsAcrossReopenAndSupportsTokenHashLookup(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "deviceauth.sqlite")
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	approvedAt := now.Add(-2 * time.Minute)
	rotateAfter := now.Add(7 * 24 * time.Hour)

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	pairing, err := model.PreparePairingRecord(model.PairingRecord{
		PairingID:             "pair-1",
		PairingSecretHash:     model.HashOpaqueSecret("pair-secret"),
		UserCode:              "ABCD1234",
		QRPayload:             "xg2g://pair?pairing_id=pair-1",
		DeviceName:            "Living Room TV",
		DeviceType:            model.DeviceTypeAndroidTV,
		ApprovedPolicyProfile: "tv-default",
		OwnerID:               "owner-1",
		Status:                model.PairingApproved,
		CreatedAt:             now.Add(-5 * time.Minute),
		ExpiresAt:             now.Add(5 * time.Minute),
		ApprovedAt:            &approvedAt,
	})
	if err != nil {
		t.Fatalf("prepare pairing: %v", err)
	}
	device, err := model.PrepareDeviceRecord(model.DeviceRecord{
		DeviceID:      "dev-1",
		OwnerID:       "owner-1",
		DeviceName:    "Living Room TV",
		DeviceType:    model.DeviceTypeAndroidTV,
		PolicyProfile: "tv-default",
		CreatedAt:     now,
	})
	if err != nil {
		t.Fatalf("prepare device: %v", err)
	}
	grant, err := model.PrepareDeviceGrantRecord(model.DeviceGrantRecord{
		GrantID:     "grant-1",
		DeviceID:    device.DeviceID,
		GrantHash:   model.HashOpaqueSecret("grant-secret"),
		IssuedAt:    now,
		ExpiresAt:   now.Add(30 * 24 * time.Hour),
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		t.Fatalf("prepare grant: %v", err)
	}
	session, err := model.PrepareAccessSessionRecord(model.AccessSessionRecord{
		SessionID:     "sess-1",
		SubjectID:     "owner-1",
		DeviceID:      device.DeviceID,
		TokenHash:     model.HashOpaqueSecret("access-token"),
		PolicyVersion: "device-auth-v1",
		Scopes:        []string{"v3:read"},
		AuthStrength:  "paired_device",
		IssuedAt:      now,
		ExpiresAt:     now.Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("prepare session: %v", err)
	}
	webBootstrap, err := model.PrepareWebBootstrapRecord(model.WebBootstrapRecord{
		BootstrapID:           "wb-1",
		BootstrapSecretHash:   model.HashOpaqueSecret("wb-secret"),
		SourceAccessSessionID: session.SessionID,
		TargetPath:            "/ui/",
		CreatedAt:             now,
		ExpiresAt:             now.Add(90 * time.Second),
	})
	if err != nil {
		t.Fatalf("prepare web bootstrap: %v", err)
	}

	if err := store.PutPairing(ctx, &pairing); err != nil {
		t.Fatalf("put pairing: %v", err)
	}
	if err := store.PutDevice(ctx, &device); err != nil {
		t.Fatalf("put device: %v", err)
	}
	if err := store.PutDeviceGrant(ctx, &grant); err != nil {
		t.Fatalf("put device grant: %v", err)
	}
	if err := store.PutAccessSession(ctx, &session); err != nil {
		t.Fatalf("put access session: %v", err)
	}
	if err := store.PutWebBootstrap(ctx, &webBootstrap); err != nil {
		t.Fatalf("put web bootstrap: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial sqlite store: %v", err)
	}

	reopened, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("close reopened sqlite store: %v", err)
		}
	}()

	gotPairing, err := reopened.GetPairing(ctx, pairing.PairingID)
	if err != nil {
		t.Fatalf("get pairing after reopen: %v", err)
	}
	if gotPairing.Status != model.PairingApproved {
		t.Fatalf("expected approved pairing after reopen, got %s", gotPairing.Status)
	}

	gotSession, err := reopened.GetAccessSessionByTokenHash(ctx, model.HashOpaqueSecret("access-token"))
	if err != nil {
		t.Fatalf("get access session by token hash: %v", err)
	}
	if gotSession.SessionID != session.SessionID {
		t.Fatalf("expected session %q, got %q", session.SessionID, gotSession.SessionID)
	}
	gotWebBootstrap, err := reopened.GetWebBootstrap(ctx, webBootstrap.BootstrapID)
	if err != nil {
		t.Fatalf("get web bootstrap after reopen: %v", err)
	}
	if gotWebBootstrap.SourceAccessSessionID != session.SessionID {
		t.Fatalf("expected web bootstrap source session %q, got %q", session.SessionID, gotWebBootstrap.SourceAccessSessionID)
	}

	gotDevice, err := reopened.GetDevice(ctx, device.DeviceID)
	if err != nil {
		t.Fatalf("get device after reopen: %v", err)
	}
	if gotDevice.OwnerID != "owner-1" {
		t.Fatalf("expected device owner to persist, got %q", gotDevice.OwnerID)
	}
}
