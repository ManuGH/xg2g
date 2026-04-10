package deviceauth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
)

func TestRefreshSessionRotatingGrantIsSingleWinnerUnderReplay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	store := deviceauthstore.NewMemoryStateStore()

	device, err := deviceauthmodel.PrepareDeviceRecord(deviceauthmodel.DeviceRecord{
		DeviceID:   "dev-1",
		OwnerID:    "owner-1",
		DeviceName: "Living Room TV",
		DeviceType: deviceauthmodel.DeviceTypeAndroidTV,
		CreatedAt:  now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("prepare device: %v", err)
	}
	rotateAfter := now.Add(-time.Minute)
	grant, err := deviceauthmodel.PrepareDeviceGrantRecord(deviceauthmodel.DeviceGrantRecord{
		GrantID:     "grant-1",
		DeviceID:    device.DeviceID,
		GrantHash:   deviceauthmodel.HashOpaqueSecret("grant-secret"),
		IssuedAt:    now.Add(-time.Hour),
		ExpiresAt:   now.Add(30 * 24 * time.Hour),
		RotateAfter: &rotateAfter,
	})
	if err != nil {
		t.Fatalf("prepare grant: %v", err)
	}
	if err := store.PutDevice(ctx, &device); err != nil {
		t.Fatalf("put device: %v", err)
	}
	if err := store.PutDeviceGrant(ctx, &grant); err != nil {
		t.Fatalf("put grant: %v", err)
	}

	service := NewService(Deps{
		StateStore:             store,
		Now:                    func() time.Time { return now },
		DeviceGrantRotateAfter: time.Minute,
	})

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	successes := make(chan *RefreshSessionResult, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := service.RefreshSession(ctx, RefreshSessionInput{
				DeviceGrantID: "grant-1",
				DeviceGrant:   "grant-secret",
			})
			if err != nil {
				errs <- err
				return
			}
			successes <- result
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(successes)

	if len(successes) != 1 {
		t.Fatalf("expected exactly one replay winner, got %d", len(successes))
	}
	result := <-successes
	if result.RotatedDeviceGrantID == "" || result.RotatedDeviceGrant == "" {
		t.Fatalf("expected winning refresh to rotate grant, got %#v", result)
	}

	for err := range errs {
		var serviceErr *Error
		if !errors.As(err, &serviceErr) || serviceErr.Kind != ErrorRevoked {
			t.Fatalf("expected replay losers to see revoked grant, got %v", err)
		}
	}
}
