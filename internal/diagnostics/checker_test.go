package diagnostics

import (
	"testing"
	"time"
)

// TestComputeOverallStatus verifies the overall status computation logic per ADR-SRE-002 P0-A.
func TestComputeOverallStatus(t *testing.T) {
	tests := []struct {
		name       string
		subsystems map[Subsystem]SubsystemHealth
		want       HealthStatus
	}{
		{
			name: "all ok",
			subsystems: map[Subsystem]SubsystemHealth{
				SubsystemReceiver: {Status: OK},
				SubsystemDVR:      {Status: OK},
				SubsystemEPG:      {Status: OK},
				SubsystemLibrary:  {Status: OK},
				SubsystemPlayback: {Status: OK},
			},
			want: OK,
		},
		{
			name: "playback unavailable → unavailable (critical)",
			subsystems: map[Subsystem]SubsystemHealth{
				SubsystemReceiver: {Status: OK},
				SubsystemDVR:      {Status: OK},
				SubsystemPlayback: {Status: Unavailable},
			},
			want: Unavailable,
		},
		{
			name: "receiver + library both unavailable → unavailable (no media source)",
			subsystems: map[Subsystem]SubsystemHealth{
				SubsystemReceiver: {Status: Unavailable},
				SubsystemDVR:      {Status: Unavailable},
				SubsystemLibrary:  {Status: Unavailable},
				SubsystemPlayback: {Status: OK},
			},
			want: Unavailable,
		},
		{
			name: "DVR unavailable but library ok → degraded (partial functionality)",
			subsystems: map[Subsystem]SubsystemHealth{
				SubsystemReceiver: {Status: OK},
				SubsystemDVR:      {Status: Unavailable},
				SubsystemLibrary:  {Status: OK},
				SubsystemPlayback: {Status: OK},
			},
			want: Degraded,
		},
		{
			name: "any degraded → degraded",
			subsystems: map[Subsystem]SubsystemHealth{
				SubsystemReceiver: {Status: Degraded},
				SubsystemDVR:      {Status: OK},
				SubsystemPlayback: {Status: OK},
			},
			want: Degraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeOverallStatus(tt.subsystems)
			if got != tt.want {
				t.Errorf("ComputeOverallStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLKGCache verifies Last-Known-Good caching with TTL expiry.
func TestLKGCache(t *testing.T) {
	cache := NewLKGCache()

	// Test DVR cache (6h TTL)
	cache.SetDVR("receiver1", 89, 3)

	// Immediate retrieval should succeed
	entry := cache.GetDVR("receiver1")
	if entry == nil {
		t.Fatal("expected DVR cache entry, got nil")
	}
	if entry.RecordingCount != 89 {
		t.Errorf("expected RecordingCount=89, got %d", entry.RecordingCount)
	}

	// Unknown receiver should return nil
	if got := cache.GetDVR("unknown"); got != nil {
		t.Errorf("expected nil for unknown receiver, got %+v", got)
	}
}

// TestLKGCacheTTLExpiry verifies TTL-based expiry (simulated).
func TestLKGCacheTTLExpiry(t *testing.T) {
	cache := NewLKGCache()

	// Override TTL for testing (would normally be 6h)
	// For this test, we manually manipulate LastOK timestamp
	cache.SetDVR("receiver1", 89, 3)

	// Retrieve and manipulate timestamp to simulate expiry
	cache.mu.Lock()
	if entry, ok := cache.dvr["receiver1"]; ok {
		entry.LastOK = time.Now().Add(-7 * time.Hour) // 7h ago (past 6h TTL)
	}
	cache.mu.Unlock()

	// Should return nil (expired)
	if got := cache.GetDVR("receiver1"); got != nil {
		t.Errorf("expected nil for expired entry, got %+v", got)
	}
}

// TestBuildDegradationSummary verifies degradation item generation.
func TestBuildDegradationSummary(t *testing.T) {
	now := time.Now()
	lastOK := now.Add(-1 * time.Hour)

	subsystems := map[Subsystem]SubsystemHealth{
		SubsystemReceiver: {
			Subsystem:  SubsystemReceiver,
			Status:     OK,
			MeasuredAt: now,
		},
		SubsystemDVR: {
			Subsystem:    SubsystemDVR,
			Status:       Unavailable,
			MeasuredAt:   now,
			LastOK:       &lastOK,
			ErrorCode:    ErrUpstreamResultFalse,
			ErrorMessage: "Recording list unavailable",
		},
	}

	summary := BuildDegradationSummary(subsystems)

	if len(summary) != 1 {
		t.Fatalf("expected 1 degradation item, got %d", len(summary))
	}

	item := summary[0]
	if item.Subsystem != SubsystemDVR {
		t.Errorf("expected SubsystemDVR, got %v", item.Subsystem)
	}
	if item.Status != Unavailable {
		t.Errorf("expected Unavailable, got %v", item.Status)
	}
	if item.ErrorCode != ErrUpstreamResultFalse {
		t.Errorf("expected %s, got %s", ErrUpstreamResultFalse, item.ErrorCode)
	}
	if len(item.SuggestedActions) == 0 {
		t.Error("expected suggested actions, got empty slice")
	}
}
