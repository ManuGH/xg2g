package pairing

import (
	"context"
	"errors"
	"testing"
	"time"

	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

// L8: the pairing status endpoint must not leak whether a pairing exists. An unknown pairing
// id and a valid id with the wrong secret must return the SAME forbidden error — previously
// an unknown id returned ErrorNotFound while a wrong secret returned ErrorForbidden, letting
// an attacker enumerate valid pairing ids (and the old order also persisted expiry before
// the secret was validated).
func TestService_Status_DoesNotLeakPairingExistence(t *testing.T) {
	now := time.Date(2026, time.April, 9, 12, 0, 0, 0, time.UTC)
	svc := NewService(Deps{
		StateStore: deviceauthstore.NewMemoryStateStore(),
		Now:        func() time.Time { return now },
		PairingTTL: time.Minute,
	})
	ctx := context.Background()

	started, err := svc.Start(ctx, StartInput{DeviceType: deviceauthmodel.DeviceTypeAndroidTV})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	_, errWrongSecret := svc.Status(ctx, StatusInput{PairingID: started.PairingID, PairingSecret: "not-the-secret"})
	_, errUnknown := svc.Status(ctx, StatusInput{PairingID: "does-not-exist", PairingSecret: "whatever"})

	if k := errorKind(errWrongSecret); k != ErrorForbidden {
		t.Fatalf("valid id + wrong secret should be forbidden, got kind=%d (%v)", k, errWrongSecret)
	}
	if k := errorKind(errUnknown); k != ErrorForbidden {
		t.Fatalf("unknown pairing id must return the SAME forbidden error (no existence leak), got kind=%d (%v)", k, errUnknown)
	}
}

func errorKind(err error) ErrorKind {
	var pErr *Error
	if errors.As(err, &pErr) {
		return pErr.Kind
	}
	return ErrorKind(255)
}
