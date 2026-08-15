package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
)

// The store keeps only pairing bootstrap state now. Device, grant and session
// writes were removed with the convergence onto identity, so the durable
// surface worth testing is the pairing round trip.
func TestSqliteStore_PersistsPairingAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "deviceauth.sqlite")

	store, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	pairing, err := model.PreparePairingRecord(model.PairingRecord{
		PairingID:              "pair-1",
		PairingSecretHash:      model.HashOpaqueSecret("pair-secret"),
		UserCode:               "ABCD-1234",
		QRPayload:              "xg2g://pair?pairing_id=pair-1&user_code=ABCD-1234",
		DeviceName:             "Living Room TV",
		DeviceType:             model.DeviceTypeAndroidTV,
		RequestedPolicyProfile: "tv-default",
		ApprovedPolicyProfile:  "tv-default",
		OwnerID:                "owner-1",
		Status:                 model.PairingApproved,
		CreatedAt:              now,
		ExpiresAt:              now.Add(10 * time.Minute),
		ApprovedAt:             &now,
	})
	if err != nil {
		t.Fatalf("prepare pairing: %v", err)
	}

	if err := store.PutPairing(ctx, &pairing); err != nil {
		t.Fatalf("put pairing: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	reopened, err := NewSqliteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	got, err := reopened.GetPairing(ctx, pairing.PairingID)
	if err != nil {
		t.Fatalf("get pairing after reopen: %v", err)
	}
	if got.Status != model.PairingApproved {
		t.Fatalf("status = %s, want %s", got.Status, model.PairingApproved)
	}
	if got.OwnerID != "owner-1" {
		t.Fatalf("ownerID = %q, want %q", got.OwnerID, "owner-1")
	}

	byCode, err := reopened.GetPairingByUserCode(ctx, pairing.UserCode)
	if err != nil {
		t.Fatalf("get pairing by user code: %v", err)
	}
	if byCode.PairingID != pairing.PairingID {
		t.Fatalf("user-code lookup returned %q, want %q", byCode.PairingID, pairing.PairingID)
	}
}
