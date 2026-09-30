// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package admission

import (
	"context"
	"errors"
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
				_ = lease.Release()
			} else if errors.Is(err, ErrCapacityExceeded) {
				rejectedCount.Add(1)
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	if successCount.Load() == 0 {
		t.Fatalf("expected at least one racer to acquire the last slot")
	}

	usage, err := c.Usage("acc-race")
	if err != nil {
		t.Fatalf("Usage failed: %v", err)
	}
	if usage.ActiveSlots > 2 {
		t.Fatalf("capacity exceeded! active slots: %d", usage.ActiveSlots)
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
