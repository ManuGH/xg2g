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
			Kind:       SegmentKindComplete,
			Sequence:   seq,
			PartIndex:  0,
		},
		Location: SegmentLocation{
			Kind:     StorageKindDisk,
			Filename: fmt.Sprintf("test_seg_%d.ts", seq),
			Path:     fmt.Sprintf("/tmp/test_seg_%d.ts", seq),
		},
		Sequence:       seq,
		StartPTS90k:    int64(seq * 90000),
		EndPTS90k:      int64((seq + 1) * 90000),
		PTSEpoch:       1,
		StartWallTime:  start,
		EndWallTime:    end,
		SizeBytes:      size,
		Discontinuity:  disc,
		CodecHash:      codec,
		State:          SegmentActive,
		ReservationIDs: make(map[string]struct{}),
	}
}

func TestParseSegmentFilename(t *testing.T) {
	id1, ok1 := ParseSegmentFilename("ref1", "sess1", "seg_000123.ts")
	if !ok1 || id1.Kind != SegmentKindComplete || id1.Sequence != 123 || id1.PartIndex != 0 {
		t.Errorf("Failed to parse seg_000123.ts correctly: %+v", id1)
	}

	id2, ok2 := ParseSegmentFilename("ref1", "sess1", "part_000123_4.m4s")
	if !ok2 || id2.Kind != SegmentKindPart || id2.Sequence != 123 || id2.PartIndex != 4 {
		t.Errorf("Failed to parse part_000123_4.m4s correctly: %+v", id2)
	}

	_, ok3 := ParseSegmentFilename("ref1", "sess1", "invalid_name.ts")
	if ok3 {
		t.Errorf("Expected parse failure on invalid filename, got success")
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
	defer store.Close()

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
	defer store.Close()

	probe, err := store.ProbeRange(ref, now.Add(-30*time.Minute), now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("ProbeRange failed: %v", err)
	}

	if probe.Completeness != CompletenessPartialStart {
		t.Errorf("Expected PARTIAL_AT_START, got %v", probe.Completeness)
	}
}

func TestMultiJobReservationOwnership(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	idx.AddSegment(seg1)

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	defer store.Close()

	res1, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_1", 5*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange job_1 failed: %v", err)
	}

	res2, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_2", 5*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange job_2 failed: %v", err)
	}

	if len(seg1.ReservationIDs) != 2 {
		t.Errorf("Expected 2 reservation owners on segment, got %d", len(seg1.ReservationIDs))
	}

	// Release job 1
	if err := store.ReleaseReservation(res1.ID); err != nil {
		t.Fatalf("ReleaseReservation job_1 failed: %v", err)
	}

	// Segment MUST still be reserved by job 2!
	if seg1.State != SegmentReserved || len(seg1.ReservationIDs) != 1 {
		t.Errorf("Segment state was reset to active prematurely! State=%s, count=%d", seg1.State, len(seg1.ReservationIDs))
	}

	// Release job 2
	if err := store.ReleaseReservation(res2.ID); err != nil {
		t.Fatalf("ReleaseReservation job_2 failed: %v", err)
	}

	// Now segment MUST be active
	if seg1.State != SegmentActive || len(seg1.ReservationIDs) != 0 {
		t.Errorf("Segment state after all releases expected SegmentActive, got %s", seg1.State)
	}
}

func TestReserveRange_ContextIndependence(t *testing.T) {
	idx := NewSegmentIndex()
	now := time.Now()
	ref := "1:0:1:1:1:1:0:0:0:0:"

	seg1 := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	idx.AddSegment(seg1)

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	res, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_http", 5*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}

	cancel()
	_ = ctx

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

	marked := idx.TryMarkDeleting(seg1.ID)
	if !marked {
		t.Fatalf("Failed to mark segment for deletion")
	}

	store := NewReservationStore(idx, DefaultReservationLimits(), "")
	defer store.Close()

	_, err := store.ReserveRange(ref, now.Add(-28*time.Minute), now.Add(-22*time.Minute), "job_late", 5*time.Minute)
	if err == nil {
		t.Fatalf("Expected error when reserving DELETING segment, but got success!")
	}
}

func TestRecoveryCorruptedJSON_FailsSafely(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "ringbuffer_corrupt_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	storagePath := filepath.Join(tmpDir, "reservations.json")
	if err := os.WriteFile(storagePath, []byte("NOT_VALID_JSON{{{"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	idx := NewSegmentIndex()
	store := NewReservationStore(idx, DefaultReservationLimits(), storagePath)
	defer store.Close()

	lifecycle := NewLifecycleManager(store, idx)

	err = lifecycle.RunRecovery()
	if err == nil {
		t.Fatalf("Expected recovery error on corrupted JSON, got nil!")
	}
	if lifecycle.State() != LifecycleDegraded {
		t.Errorf("Expected LifecycleDegraded state, got %s", lifecycle.State())
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
	seg1.Location.Path = segFile
	idx.AddSegment(seg1)

	store := NewReservationStore(idx, DefaultReservationLimits(), storagePath)
	res, err := store.ReserveRange(ref, now.Add(-25*time.Minute), now.Add(-21*time.Minute), "job_recover", 10*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}
	store.Close()

	newIdx := NewSegmentIndex()
	seg1Reloaded := createTestSegment(ref, 1, now.Add(-30*time.Minute), now.Add(-20*time.Minute), 1000, "h264", false)
	seg1Reloaded.Location.Path = segFile
	newIdx.AddSegment(seg1Reloaded)

	newStore := NewReservationStore(newIdx, DefaultReservationLimits(), storagePath)
	defer newStore.Close()

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

func TestRealIngestServer_ReservationEvictionProtection(t *testing.T) {
	reg, err := NewRegistryWithStorage(5, "")
	if err != nil {
		t.Fatalf("NewRegistryWithStorage failed: %v", err)
	}
	defer reg.Stop()

	sessionID := "test_live_session"
	buf := reg.GetOrCreate(sessionID, nil)

	now := time.Now()
	// Put 10 segments
	for i := uint64(1); i <= 10; i++ {
		filename := fmt.Sprintf("seg_%06d.ts", i)
		buf.Put(filename, []byte(fmt.Sprintf("ts_data_%d", i)))
	}

	// Reserve segments 3, 4, 5
	start := now.Add(-10 * time.Minute)
	end := now.Add(10 * time.Minute)
	res, err := reg.Store().ReserveRange(sessionID, start, end, "test_owner", 10*time.Minute)
	if err != nil {
		t.Fatalf("ReserveRange failed: %v", err)
	}

	// Put 20 MORE segments (triggering heavy eviction)
	for i := uint64(11); i <= 30; i++ {
		filename := fmt.Sprintf("seg_%06d.ts", i)
		buf.Put(filename, []byte(fmt.Sprintf("ts_data_%d", i)))
	}

	// Reserved segments MUST NOT be evicted from RAM artifacts or index!
	handles, err := reg.Store().ListReservedSegments(res.ID)
	if err != nil {
		t.Fatalf("ListReservedSegments failed: %v", err)
	}
	if len(handles) == 0 {
		t.Fatalf("Expected reserved segment handles, got 0!")
	}

	for _, segID := range res.SegmentIDs {
		seg, ok := reg.Index().GetByID(segID)
		if !ok || seg.State != SegmentReserved {
			t.Errorf("Reserved segment %s was evicted or lost state!", segID)
		}
	}

	// Release reservation
	if err := reg.Store().ReleaseReservation(res.ID); err != nil {
		t.Fatalf("ReleaseReservation failed: %v", err)
	}

	// Put 10 more segments to trigger eviction of unreserved segments
	for i := uint64(31); i <= 40; i++ {
		filename := fmt.Sprintf("seg_%06d.ts", i)
		buf.Put(filename, []byte(fmt.Sprintf("ts_data_%d", i)))
	}

	// Verify buffer size dropped down to maxSegments
	buf.mu.RLock()
	artCount := len(buf.artifacts)
	buf.mu.RUnlock()

	if artCount > 5 {
		t.Errorf("Expected artifact count <= 5 after release & eviction, got %d", artCount)
	}
}
