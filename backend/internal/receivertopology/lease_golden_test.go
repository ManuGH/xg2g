// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package receivertopology

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 1. Golden Test: Atomic Concurrency & Race Condition Protection
// When 50 concurrent clients simultaneously request the 8th (last) demodulator slot,
// exactly ONE must succeed, and 49 must be rejected without exceeding physical capacity.
func TestGolden_Concurrency_AtomicReservationRaceCondition(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	svc, err := NewService(topology, EvaluationModeEnforce)
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	reg := svc.Resolver().(*TransponderRegistry)
	for i := 0; i < 20; i++ {
		reg.RegisterTransponder(uint16(0x0400+i), 0x0001, 0x00C00000, TransponderKey{
			DeliverySystem:  DeliverySystemDVBS2,
			OrbitalPosition: 192,
			FrequencyHz:     12544000000 + uint64(i)*1000000,
			Polarization:    PolarizationHorizontal,
			StreamID:        -1,
		})
	}

	// First occupy 7 of the 8 demods with distinct transponders on the same plane
	for i := 0; i < 7; i++ {
		sRef := fmt.Sprintf("1:0:19:%04X:%04X:1:C00000:0:0:0:", 0x0100+i, 0x0400+i)
		_, _, err := svc.ReserveStreamLeaseAtomic(sRef, fmt.Sprintf("pre-sess-%d", i), PriorityLive, time.Minute)
		if err != nil {
			t.Fatalf("failed to seed initial allocation %d: %v", i, err)
		}
	}

	if len(svc.RuntimeSnapshot().ActiveMultiplexes) != 7 {
		t.Fatalf("expected 7 active multiplexes before race")
	}

	// 50 concurrent requests competing for the 8th (final) unique multiplex
	const numConcurrent = 50
	var wg sync.WaitGroup
	var successfulAllocations int64
	var rejectedAllocations int64

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sRef := fmt.Sprintf("1:0:19:%04X:%04X:1:C00000:0:0:0:", 0x0900+idx, 0x0408+idx) // Unique transponder
			sessionID := fmt.Sprintf("race-sess-%d", idx)

			_, _, err := svc.ReserveStreamLeaseAtomic(sRef, sessionID, PriorityLive, time.Minute)
			if err == nil {
				atomic.AddInt64(&successfulAllocations, 1)
			} else {
				atomic.AddInt64(&rejectedAllocations, 1)
			}
		}(i)
	}

	wg.Wait()

	if successfulAllocations != 1 {
		t.Fatalf("expected EXACTLY 1 successful allocation under race, got %d", successfulAllocations)
	}
	if rejectedAllocations != numConcurrent-1 {
		t.Fatalf("expected %d rejections, got %d", numConcurrent-1, rejectedAllocations)
	}

	// Verify total active multiplexes equals exactly 8 (hard hardware limit)
	if len(svc.RuntimeSnapshot().ActiveMultiplexes) != 8 {
		t.Fatalf("expected exactly 8 active multiplexes, got %d", len(svc.RuntimeSnapshot().ActiveMultiplexes))
	}
}

// 2. Golden Test: Upcoming Recording Reservation Protection
// Live stream at 20:14 must not steal the single available RF plane required by a scheduled 20:15 recording.
func TestGolden_UpcomingRecording_BlocksConflictingLiveStream(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	svc, _ := NewService(topology, EvaluationModeEnforce)

	// Scheduled Recording "Tatort" at 20:15 on Das Erste HD (High-H)
	tatortMux := BuildSatMultiplexID(192, 0x00C00000, 0x03FB, 0x0001, BandHigh, PolarizationHorizontal)
	now := time.Date(2026, 8, 16, 20, 14, 0, 0, time.UTC)

	svc.SyncTimers([]RecordingReservation{
		{
			ID:          "timer-tatort-1",
			ServiceRef:  "1:0:19:283D:3FB:1:C00000:0:0:0:",
			MultiplexID: tatortMux,
			StartTime:   time.Date(2026, 8, 16, 20, 15, 0, 0, time.UTC),
			EndTime:     time.Date(2026, 8, 16, 21, 45, 0, 0, time.UTC),
			Title:       "Tatort",
			Priority:    PriorityUpcomingRecording,
		},
	})

	// Scenario A: At 20:14, a user wants to watch a channel on a CONFLICTING plane (High-V) on single cable LNB
	conflictingMux := BuildSatMultiplexID(192, 0x00C00000, 0x03FC, 0x0001, BandHigh, PolarizationVertical)

	// Evaluate with upcoming reservations
	decision := svc.allocator.EvaluateWithUpcomingReservations(
		svc.runtime,
		svc.planner,
		conflictingMux,
		"live-sess-1",
		PriorityLive,
		now,
	)

	if decision.Allowed {
		t.Fatalf("Live stream on conflicting High-V plane MUST BE BLOCKED before 20:15 recording on High-H")
	}
	if decision.Reason == "" {
		t.Fatalf("expected descriptive rejection reason")
	}

	// Scenario B: User wants to watch KiKA HD on the SAME transponder/plane as the upcoming recording
	decisionSamePlane := svc.allocator.EvaluateWithUpcomingReservations(
		svc.runtime,
		svc.planner,
		tatortMux,
		"live-sess-2",
		PriorityLive,
		now,
	)

	if !decisionSamePlane.Allowed {
		t.Fatalf("Live stream sharing same transponder/plane as upcoming recording MUST BE ALLOWED")
	}
}

// 3. Golden Test: Lease Heartbeat and Sweeper Cleanup
func TestGolden_Lease_HeartbeatAndSweep(t *testing.T) {
	topology := buildVuPlusUno4K_FBC_SingleCable()
	svc, _ := NewService(topology, EvaluationModeEnforce)
	seedReceiverTransponders(t, svc)

	sRef := "1:0:19:283D:3FB:1:C00000:0:0:0:"
	sessionID := "sess-heartbeat-test"

	// Reserve lease with short 50ms TTL
	lease, _, err := svc.ReserveStreamLeaseAtomic(sRef, sessionID, PriorityLive, 50*time.Millisecond)
	if err != nil || lease == nil {
		t.Fatalf("failed to reserve lease: %v", err)
	}

	// Heartbeat extends the lease
	extended := svc.HeartbeatStream(sessionID, 500*time.Millisecond)
	if !extended {
		t.Fatalf("expected heartbeat to succeed")
	}

	// Before expiration: sweep should clean nothing
	expired := svc.SweepExpiredLeases(time.Now().UTC())
	if len(expired) != 0 {
		t.Fatalf("expected 0 expired leases before TTL elapsed, got %d", len(expired))
	}

	// After expiration: sweep reclaims the demodulator
	sweepTime := time.Now().UTC().Add(600 * time.Millisecond)
	expired = svc.SweepExpiredLeases(sweepTime)
	if len(expired) != 1 || expired[0] != sessionID {
		t.Fatalf("expected session %s to be swept, got %v", sessionID, expired)
	}

	if len(svc.RuntimeSnapshot().ActiveMultiplexes) != 0 {
		t.Fatalf("expected 0 active multiplexes after lease sweep")
	}
}
