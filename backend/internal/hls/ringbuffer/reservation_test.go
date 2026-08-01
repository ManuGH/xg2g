package ringbuffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestSegment(serviceRef string, seq uint64, start, end time.Time, size int64, codec string, disc bool) *InternalSegment {
	return &InternalSegment{
		ID: SegmentID{
			ServiceRef: serviceRef,
			SessionID:  "sess_1",
			Sequence:   seq,
		},
		Path:          fmt.Sprintf("/tmp/test_seg_%d.ts", seq),
		Sequence:      seq,
		StartPTS90k:   int64(seq * 90000),
		EndPTS90k:     int64((seq + 1) * 90000),
		PTSEpoch:      1,
		StartWallTime: start,
		EndWallTime:   end,
		SizeBytes:     size,
		Discontinuity: disc,
		CodecHash:     codec,
		State:         SegmentActive,
	}
}

func TestProbeRange_Complete(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	idx.AddSegment(createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false))
	idx.AddSegment(createTestSegment(ref, 2, now.Add(-20*time.Minute), now.Add(-10*time.Minute), 1000, "h264", false))
	idx.AddSegment(createTestSegment(ref, 3, now.Add(-10*time.Minute), now, 1000, "h264", false))

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	probe, err := store.ProbeRange(ref, now.Add(-25*time.Minute), now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("ProbeRange failed: %v", err)
	}

	if probe.Completeness != CompletenessComplete {
		t.Errorf("Expected COMPLETE, got %v", probe.Completeness)
	}
	if probe.SegmentCount != 3 {
		t.Errorf("Expected 3 segments, got %d", probe.SegmentCount)
	}
}

func TestProbeRange_PartialStart(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	idx.AddSegment(createTestSegment(ref, 1, now.Add(-15*time.Minute), now.Add(-10*time.Minute), 1000, "h264", false))
	idx.AddSegment(createTestSegment(ref, 2, now.Add(-10*time.Minute), now, 1000, "h264", false))

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	probe, err := store.ProbeRange(ref, now.Add(-30*time.Minute), now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("ProbeRange failed: %v", err)
	}

	if probe.Completeness != CompletenessPartialStart {
		t.Errorf("Expected PARTIAL_AT_START, got %v", probe.Completeness)
	}
}

func TestReserveRange_AtomicAndLease(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	seg2 := createTestSegment(ref, 2, now.Add(-20*time.Minute), now.Add(-10*time.Minute), 1000, "h264", false)
	idx.AddSegment(seg1)
	idx.AddSegment(seg2)

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	res, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-15*time.Minute), "job_1", 2*time.Second)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}

	if seg1.State != SegmentReserved || seg2.State != SegmentReserved {
		t.Errorf("Segments were not marked as SegmentReserved! seg1 state=%s, seg2 state=%s", seg1.State, seg2.State)
	}

	// Cleaner attempt on reserved segment must fail
	if idx.MarkForDeletion(seg1.ID) {
		t.Errorf("Cleaner marked reserved segment for deletion!")
	}

	// Release reservation
	err = store.ReleaseReservation(res.ID)
	if err != nil {
		t.Fatalf("ReleaseReservation failed: %v", err)
	}

	if seg1.State != SegmentActive {
		t.Errorf("Segment state after release expected SegmentActive, got %s", seg1.State)
	}
}

func TestReserveRange_ContextIndependence(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	idx.AddSegment(seg1)

	store := NewReservationStore(idx, DefaultReservationLimits(), "")

	// Simulate an HTTP request context that gets canceled
	ctx, cancel := context.WithCancel(context.Background())
	res, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_http", 5*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}

	// Cancel HTTP context
	cancel()
	_ = ctx

	// Reservation must STILL be valid and active in store
	fetched, err := store.GetReservation(res.ID)
	if err != nil {
		t.Fatalf("Reservation disappeared after HTTP context cancellation: %v", err)
	}
	if fetched.ID != res.ID {
		t.Errorf("Reservation ID mismatch")
	}
}

func TestCleanerDeletingRace(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	idx.AddSegment(seg1)

	// Cleaner marks segment for deletion
	marked := idx.MarkForDeletion(seg1.ID)
	if !marked {
		t.Fatalf("Failed to mark segment for deletion")
	}

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	_, err := store.ReserveRange(ref, now.Add(-28*time.Minute), now.Add(-22*time.Minute), "job_late", 5*time.Minute)
	if err == nil {
		t.Fatalf("Expected error when reserving DELETING segment, but got success!")
	}
}

func TestStartupRecoveryLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ringbuffer_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storagePath := filepath.Join(tmpDir, "reservations.json")
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	segFile := filepath.Join(tmpDir, "seg1.ts")
	if err := os.WriteFile(segFile, []byte("fake_ts_data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	seg1.Path = segFile
	idx.AddSegment(seg1)

	store := NewReservationStore(idx, DefaultReservationLimits(), storagePath)
	res, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_recover", 10*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}

	// Re-create store simulating a process restart
	newIdx := NewSegmentIndex()
	seg1Reloaded := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	seg1Reloaded.Path = segFile
	newIdx.AddSegment(seg1Reloaded)

	newStore := NewReservationStore(newIdx, DefaultReservationLimits(), storagePath)
	lifecycle := NewLifecycleManager(newStore, newIdx)

	if lifecycle.State() != LifecycleStarting {
		t.Errorf("Expected STARTING, got %s", lifecycle.State())
	}

	err = lifecycle.RunRecovery()
	if err != nil {
		t.Fatalf("RunRecovery failed: %v", err)
	}

	if lifecycle.State() != LifecycleCleanupEnabled {
		t.Errorf("Expected CLEANUP_ENABLED, got %s", lifecycle.State())
	}

	// Verify reservation was recovered and segment is reserved
	recoveredRes, err := newStore.GetReservation(res.ID)
	if err != nil {
		t.Fatalf("Failed to recover reservation: %v", err)
	}
	if recoveredRes.ID != res.ID {
		t.Errorf("Recovered reservation ID mismatch")
	}

	if seg1Reloaded.State != SegmentReserved {
		t.Errorf("Reloaded segment expected SegmentReserved, got %s", seg1Reloaded.State)
	}
}
