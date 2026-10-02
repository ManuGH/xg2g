// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package admission

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestController_CapacityLimit(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID: "acc-1",
		MaxSlots:  2,
	})
	defer c.Close()

	ctx := context.Background()

	// 1. First distinct source succeeds
	lease1, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-a",
		ClientID:  "client-1",
	})
	if err != nil {
		t.Fatalf("Acquire stream-a failed: %v", err)
	}
	if lease1.IsShared() {
		t.Fatalf("lease1 should not be shared")
	}

	// 2. Second distinct source succeeds
	lease2, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-b",
		ClientID:  "client-2",
	})
	if err != nil {
		t.Fatalf("Acquire stream-b failed: %v", err)
	}
	if lease2.IsShared() {
		t.Fatalf("lease2 should not be shared")
	}

	// 3. Third distinct source MUST be rejected with ErrCapacityExceeded
	_, err = c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-c",
		ClientID:  "client-3",
	})
	if !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("expected ErrCapacityExceeded, got: %v", err)
	}

	// 4. Release one slot and acquire stream-c
	if err := lease1.Release(); err != nil {
		t.Fatalf("lease1 release failed: %v", err)
	}

	lease3, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-c",
		ClientID:  "client-3",
	})
	if err != nil {
		t.Fatalf("Acquire stream-c after release failed: %v", err)
	}
	defer lease3.Release()
	defer lease2.Release()
}

func TestController_SharedIngest_MultiScreen(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID: "acc-1",
		MaxSlots:  2,
	})
	defer c.Close()

	ctx := context.Background()

	// TV starts stream-a (Living Room)
	leaseTV, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-a",
		ClientID:  "client-tv",
	})
	if err != nil {
		t.Fatalf("Acquire TV failed: %v", err)
	}

	// iPhone also tunes to stream-a (Same channel)
	leasePhone, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-a",
		ClientID:  "client-phone",
	})
	if err != nil {
		t.Fatalf("Acquire iPhone failed: %v", err)
	}
	if !leasePhone.IsShared() {
		t.Fatalf("leasePhone should be shared")
	}

	usage, err := c.Usage("acc-1")
	if err != nil {
		t.Fatalf("Usage failed: %v", err)
	}
	if usage.ActiveSlots != 1 {
		t.Fatalf("expected 1 active slot for shared stream, got %d", usage.ActiveSlots)
	}
	if usage.TotalViewers != 2 {
		t.Fatalf("expected 2 viewers, got %d", usage.TotalViewers)
	}

	// Mac starts stream-b (2nd slot)
	leaseMac, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-1",
		SourceID:  "stream-b",
		ClientID:  "client-mac",
	})
	if err != nil {
		t.Fatalf("Acquire Mac failed: %v", err)
	}
	defer leaseMac.Release()

	// Now 2 slots occupied, 3 total viewers
	usage, _ = c.Usage("acc-1")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected 2 active slots, got %d", usage.ActiveSlots)
	}
	if usage.TotalViewers != 3 {
		t.Fatalf("expected 3 viewers, got %d", usage.TotalViewers)
	}

	// TV stops watching stream-a
	if err := leaseTV.Release(); err != nil {
		t.Fatalf("TV release failed: %v", err)
	}

	// iPhone is still watching stream-a: stream-a MUST remain active!
	usage, _ = c.Usage("acc-1")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected 2 active slots, got %d", usage.ActiveSlots)
	}
	if usage.TotalViewers != 2 {
		t.Fatalf("expected 2 viewers, got %d", usage.TotalViewers)
	}

	// iPhone releases stream-a
	if err := leasePhone.Release(); err != nil {
		t.Fatalf("Phone release failed: %v", err)
	}

	usage, _ = c.Usage("acc-1")
	if usage.ActiveSlots != 1 {
		t.Fatalf("expected 1 active slot remaining, got %d", usage.ActiveSlots)
	}
}

func TestController_ConcurrentRaceForLastSlot(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID: "acc-race",
		MaxSlots:  2,
	})
	defer c.Close()

	ctx := context.Background()

	// Fill slot 1
	lease1, err := c.Acquire(ctx, AcquireRequest{
		AccountID: "acc-race",
		SourceID:  "fixed-stream",
		ClientID:  "fixed-client",
	})
	if err != nil {
		t.Fatalf("fill slot 1 failed: %v", err)
	}
	defer lease1.Release()

	// 50 goroutines racing for the remaining slot with distinct source IDs
	const numGoroutines = 50
	var successCount atomic.Int32
	var rejectedCount atomic.Int32
	leases := make(chan SlotLease, numGoroutines)

	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-startBarrier

			lease, err := c.Acquire(ctx, AcquireRequest{
				AccountID: "acc-race",
				SourceID:  time.Now().Format("150405.000000000") + string(rune('a'+id)),
				ClientID:  time.Now().Format("150405.000000000") + string(rune('a'+id)),
			})
			if err == nil {
				successCount.Add(1)
				leases <- lease
			} else if errors.Is(err, ErrCapacityExceeded) {
				rejectedCount.Add(1)
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Fatalf("exactly one racer must hold the last slot, got %d", got)
	}
	if got := rejectedCount.Load(); got != numGoroutines-1 {
		t.Fatalf("expected %d capacity rejections, got %d", numGoroutines-1, got)
	}

	usage, err := c.Usage("acc-race")
	if err != nil {
		t.Fatalf("Usage failed: %v", err)
	}
	if usage.ActiveSlots > 2 {
		t.Fatalf("capacity exceeded! active slots: %d", usage.ActiveSlots)
	}
	for len(leases) > 0 {
		if err := (<-leases).Release(); err != nil {
			t.Fatalf("release raced lease: %v", err)
		}
	}
}

func TestController_RepeatedLeaseForClientHasIndependentRelease(t *testing.T) {
	c := NewController(AccountConfig{AccountID: "acc-refs", MaxSlots: 1})
	defer c.Close()

	first, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-refs", SourceID: "same", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-refs", SourceID: "same", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if !second.IsActive() {
		t.Fatal("releasing one lease must leave the second lease active")
	}
	usage, err := c.Usage("acc-refs")
	if err != nil || usage.ActiveSlots != 1 || usage.TotalViewers != 1 {
		t.Fatalf("unexpected usage after one release: usage=%+v err=%v", usage, err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if second.IsActive() {
		t.Fatal("released lease must no longer be active")
	}
}

func TestController_TeardownFailureKeepsSlotReserved(t *testing.T) {
	c := NewController(AccountConfig{AccountID: "acc-stop", MaxSlots: 1})
	defer c.Close()

	lease, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-stop", SourceID: "source-a", ClientID: "viewer-a"})
	if err != nil {
		t.Fatal(err)
	}
	teardownErr := errors.New("upstream did not stop")
	if err := c.RegisterTeardown("acc-stop", "source-a", func() error {
		// Teardown hooks may inspect the controller; they must not run under mu.
		if _, err := c.Usage("acc-stop"); err != nil {
			return err
		}
		return teardownErr
	}); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); !errors.Is(err, ErrTeardownFailed) || !errors.Is(err, teardownErr) {
		t.Fatalf("expected teardown failure to be returned, got %v", err)
	}
	usage, err := c.Usage("acc-stop")
	if err != nil || usage.ActiveSlots != 1 || !usage.ActiveSources[0].IsFenced {
		t.Fatalf("failed teardown must keep the slot fenced and reserved: usage=%+v err=%v", usage, err)
	}
	if _, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-stop", SourceID: "source-b", ClientID: "viewer-b"}); !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("failed teardown must not free capacity, got %v", err)
	}
	closeErr := c.Close()
	if !errors.Is(closeErr, teardownErr) || strings.Count(closeErr.Error(), "teardown acc-stop/source-a") != 1 {
		t.Fatalf("Close must report the failed teardown once, got %v", closeErr)
	}
}

func TestController_CloseWaitsForInFlightTeardownAndConcurrentClose(t *testing.T) {
	c := NewController(AccountConfig{AccountID: "acc-close", MaxSlots: 1})
	lease, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-close", SourceID: "source-a", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}

	teardownStarted := make(chan struct{})
	allowTeardownFinish := make(chan struct{})
	if err := c.RegisterTeardown("acc-close", "source-a", func() error {
		close(teardownStarted)
		<-allowTeardownFinish
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- lease.Release() }()
	<-teardownStarted

	firstCloseDone := make(chan error, 1)
	go func() { firstCloseDone <- c.Close() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := c.ConfigureAccount(AccountConfig{AccountID: "acc-close", MaxSlots: 1})
		if errors.Is(err, ErrControllerClosed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not fence the controller")
		}
		time.Sleep(time.Millisecond)
	}

	secondCloseDone := make(chan error, 1)
	go func() { secondCloseDone <- c.Close() }()
	select {
	case err := <-secondCloseDone:
		t.Fatalf("concurrent Close returned before teardown finished: %v", err)
	case <-time.After(10 * time.Millisecond):
	}

	close(allowTeardownFinish)
	if err := <-releaseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-firstCloseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondCloseDone; err != nil {
		t.Fatal(err)
	}
}

func TestController_ChannelSwitch_Exclusive(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID: "acc-switch",
		MaxSlots:  2,
	})
	defer c.Close()

	ctx := context.Background()

	// Occupy 2 slots
	lease1, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-switch", SourceID: "ch-1", ClientID: "viewer-1"})
	defer lease1.Release()

	lease2, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-switch", SourceID: "ch-2", ClientID: "viewer-2"})
	defer lease2.Release()

	// Teardown tracking
	var ch2TornDown atomic.Bool
	c.RegisterTeardown("acc-switch", "ch-2", func() error {
		ch2TornDown.Store(true)
		return nil
	})

	// viewer-2 (sole subscriber on ch-2) switches to ch-3
	newLease, err := c.Switch(ctx, SwitchRequest{
		AccountID:   "acc-switch",
		OldSourceID: "ch-2",
		NewSourceID: "ch-3",
		ClientID:    "viewer-2",
		LeaseID:     lease2.LeaseID(),
	})
	if err != nil {
		t.Fatalf("Switch failed: %v", err)
	}
	defer newLease.Release()

	if !ch2TornDown.Load() {
		t.Fatalf("expected ch-2 teardown to be executed during switch")
	}

	usage, _ := c.Usage("acc-switch")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected exactly 2 active slots, got %d", usage.ActiveSlots)
	}
}

func TestController_ChannelSwitch_Shared_RejectedWhenCapacityFull(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID: "acc-switch-shared",
		MaxSlots:  2,
	})
	defer c.Close()

	ctx := context.Background()

	// Slot 1: shared by viewer-tv and viewer-phone on ch-1
	leaseTV, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-switch-shared", SourceID: "ch-1", ClientID: "viewer-tv"})
	defer leaseTV.Release()
	leasePhone, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-switch-shared", SourceID: "ch-1", ClientID: "viewer-phone"})
	defer leasePhone.Release()

	// Slot 2: viewer-mac on ch-2
	leaseMac, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-switch-shared", SourceID: "ch-2", ClientID: "viewer-mac"})
	defer leaseMac.Release()

	// viewer-phone tries to switch to ch-3 (distinct channel)
	// Because ch-1 still has viewer-tv, ch-1 CANNOT be closed, and ch-2 is occupied by viewer-mac
	// So capacity is full (2/2) -> switch must be rejected without disrupting viewer-tv or viewer-phone!
	_, err := c.Switch(ctx, SwitchRequest{
		AccountID:   "acc-switch-shared",
		OldSourceID: "ch-1",
		NewSourceID: "ch-3",
		ClientID:    "viewer-phone",
		LeaseID:     leasePhone.LeaseID(),
	})
	if !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("expected ErrCapacityExceeded on switch, got: %v", err)
	}

	// Verify viewer-phone is still attached to ch-1
	usage, _ := c.Usage("acc-switch-shared")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected 2 active slots, got %d", usage.ActiveSlots)
	}
	if usage.TotalViewers != 3 {
		t.Fatalf("expected 3 total viewers preserved, got %d", usage.TotalViewers)
	}
}

func TestController_SwitchMovesOnlySelectedLeaseForRepeatedClient(t *testing.T) {
	c := NewController(AccountConfig{AccountID: "acc-switch-refs", MaxSlots: 2})
	defer c.Close()

	first, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-switch-refs", SourceID: "source-a", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-switch-refs", SourceID: "source-a", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}

	moved, err := c.Switch(context.Background(), SwitchRequest{
		AccountID: "acc-switch-refs", OldSourceID: "source-a", NewSourceID: "source-b", ClientID: "viewer", LeaseID: first.LeaseID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.IsActive() || !second.IsActive() || !moved.IsActive() {
		t.Fatal("switch must move only the selected lease and preserve the other lease")
	}
	usage, err := c.Usage("acc-switch-refs")
	if err != nil || usage.ActiveSlots != 2 || usage.TotalViewers != 2 {
		t.Fatalf("unexpected usage after switch: usage=%+v err=%v", usage, err)
	}

	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := moved.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestController_SwitchRequiresLeaseIDWhenClientHasMultipleLeases(t *testing.T) {
	c := NewController(AccountConfig{AccountID: "acc-switch-ambiguous", MaxSlots: 2})
	defer c.Close()

	_, err := c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-switch-ambiguous", SourceID: "source-a", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Acquire(context.Background(), AcquireRequest{AccountID: "acc-switch-ambiguous", SourceID: "source-a", ClientID: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Switch(context.Background(), SwitchRequest{AccountID: "acc-switch-ambiguous", OldSourceID: "source-a", NewSourceID: "source-b", ClientID: "viewer"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ambiguous switch without lease ID must be rejected, got %v", err)
	}
	usage, err := c.Usage("acc-switch-ambiguous")
	if err != nil || usage.ActiveSlots != 1 || usage.TotalViewers != 1 {
		t.Fatalf("rejected switch must preserve the source, usage=%+v err=%v", usage, err)
	}
}

func TestController_WarmHoldEviction(t *testing.T) {
	c := NewController(AccountConfig{
		AccountID:        "acc-hold",
		MaxSlots:         2,
		WarmHoldDuration: 5 * time.Second, // Long hold
	})
	defer c.Close()

	ctx := context.Background()

	// Slot 1: active stream-1
	lease1, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-hold", SourceID: "stream-1", ClientID: "c1"})
	defer lease1.Release()

	// Slot 2: stream-2
	lease2, _ := c.Acquire(ctx, AcquireRequest{AccountID: "acc-hold", SourceID: "stream-2", ClientID: "c2"})

	var stream2TornDown atomic.Bool
	c.RegisterTeardown("acc-hold", "stream-2", func() error {
		stream2TornDown.Store(true)
		return nil
	})

	// c2 disconnects -> stream-2 goes into warm-hold
	_ = lease2.Release()

	usage, _ := c.Usage("acc-hold")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected 2 active slots (1 holding), got %d", usage.ActiveSlots)
	}

	// Now c3 requests stream-3: should evict stream-2 and succeed immediately!
	lease3, err := c.Acquire(ctx, AcquireRequest{AccountID: "acc-hold", SourceID: "stream-3", ClientID: "c3"})
	if err != nil {
		t.Fatalf("Acquire stream-3 failed: %v", err)
	}
	defer lease3.Release()

	if !stream2TornDown.Load() {
		t.Fatalf("expected stream-2 to be torn down during eviction")
	}

	usage, _ = c.Usage("acc-hold")
	if usage.ActiveSlots != 2 {
		t.Fatalf("expected 2 active slots, got %d", usage.ActiveSlots)
	}
}

func TestController_AccountIsolation(t *testing.T) {
	c := NewController(
		AccountConfig{AccountID: "acc-a", MaxSlots: 1},
		AccountConfig{AccountID: "acc-b", MaxSlots: 1},
	)
	defer c.Close()

	ctx := context.Background()

	// acc-a occupies its only slot
	leaseA, err := c.Acquire(ctx, AcquireRequest{AccountID: "acc-a", SourceID: "s1", ClientID: "c1"})
	if err != nil {
		t.Fatalf("acc-a acquire failed: %v", err)
	}
	defer leaseA.Release()

	// acc-b should be able to acquire its own slot independently
	leaseB, err := c.Acquire(ctx, AcquireRequest{AccountID: "acc-b", SourceID: "s2", ClientID: "c2"})
	if err != nil {
		t.Fatalf("acc-b acquire failed: %v", err)
	}
	defer leaseB.Release()

	// acc-a 2nd acquire fails
	_, err = c.Acquire(ctx, AcquireRequest{AccountID: "acc-a", SourceID: "s3", ClientID: "c3"})
	if !errors.Is(err, ErrCapacityExceeded) {
		t.Fatalf("expected ErrCapacityExceeded for acc-a, got %v", err)
	}
}
