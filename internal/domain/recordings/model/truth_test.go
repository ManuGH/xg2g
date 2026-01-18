package model

import (
	"testing"
	"time"
)

func TestDeriveRecordingStatus(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-10 * time.Minute)
	windowEnd := now.Add(50 * time.Minute)

	tests := []struct {
		name           string
		now            time.Time
		file           FilePresenceClass
		timer          *TimerTruth
		expectedStatus RecordingStatus
		expectedReason string // Partial match ok
	}{
		// --- 1. Filter Logic ---
		{
			name:           "Filter: JustPlay timer is ignored -> Unknown (no file)",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, JustPlay: true},
			expectedStatus: RecordingStatusUnknown,
		},
		{
			name:           "Filter: Disabled timer is ignored -> Unknown (no file)",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, Disabled: true},
			expectedStatus: RecordingStatusUnknown,
		},

		// --- 2. RECORDING (Active Window) ---
		{
			name:           "Recording: Running=true (File Missing) -> RECORDING",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, Running: true},
			expectedStatus: RecordingStatusRecording,
		},
		{
			name:           "Recording: Running=false but File Present (Small) -> RECORDING",
			now:            now,
			file:           FilePresenceSmall,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, Running: false},
			expectedStatus: RecordingStatusRecording,
		},
		{
			name:           "Recording: Running=false but File Present (Partial) -> RECORDING",
			now:            now,
			file:           FilePresencePartial,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, Running: false},
			expectedStatus: RecordingStatusRecording,
		},
		{
			name:           "Recording: Running=false and File Missing -> UNKNOWN (No explicit signal)",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: windowStart, End: windowEnd, Running: false},
			expectedStatus: RecordingStatusUnknown,
		},

		// --- 3. SCHEDULED (Future) ---
		{
			name:           "Scheduled: Timer in future -> SCHEDULED",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour)},
			expectedStatus: RecordingStatusScheduled,
		},

		// --- 4. FAILED (Past + Bad File) ---
		{
			name:           "Failed: Timer Past Grace + File Missing -> FAILED",
			now:            now,
			file:           FilePresenceMissing,
			timer:          &TimerTruth{Begin: now.Add(-2 * time.Hour), End: now.Add(-1*time.Hour - PostRollGracePeriod - time.Second)},
			expectedStatus: RecordingStatusFailed,
		},
		{
			name:           "Failed: Timer Past Grace + File Small -> FAILED",
			now:            now,
			file:           FilePresenceSmall,
			timer:          &TimerTruth{Begin: now.Add(-2 * time.Hour), End: now.Add(-1*time.Hour - PostRollGracePeriod - time.Second)},
			expectedStatus: RecordingStatusFailed,
		},
		{
			name:           "Failed: Timer Past Grace + File Partial -> FAILED",
			now:            now,
			file:           FilePresencePartial,
			timer:          &TimerTruth{Begin: now.Add(-2 * time.Hour), End: now.Add(-1*time.Hour - PostRollGracePeriod - time.Second)},
			expectedStatus: RecordingStatusFailed,
		},

		// --- 5. COMPLETED (File OK + No Active/Relevant Timer) ---
		{
			name:           "Completed: File OK + No Timer -> COMPLETED",
			now:            now,
			file:           FilePresenceOK,
			timer:          nil,
			expectedStatus: RecordingStatusCompleted,
		},
		{
			name:           "Completed: File OK + Past Timer (Grace over) -> COMPLETED",
			now:            now,
			file:           FilePresenceOK,
			timer:          &TimerTruth{Begin: now.Add(-2 * time.Hour), End: now.Add(-1*time.Hour - PostRollGracePeriod - time.Second)},
			expectedStatus: RecordingStatusCompleted, // Not Failed because file is OK
		},

		// --- 6. UNKNOWN / Residuals ---
		{
			name:           "Debris: Small File + No Timer -> FAILED",
			now:            now,
			file:           FilePresenceSmall,
			timer:          nil,
			expectedStatus: RecordingStatusFailed,
		},
		{
			name:           "Debris: Partial File + No Timer -> FAILED",
			now:            now,
			file:           FilePresencePartial,
			timer:          nil,
			expectedStatus: RecordingStatusFailed,
		},
		{
			name:           "Unknown: Nothing -> UNKNOWN",
			now:            now,
			file:           FilePresenceMissing,
			timer:          nil,
			expectedStatus: RecordingStatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := DeriveRecordingStatus(tt.now, tt.file, tt.timer)
			if status != tt.expectedStatus {
				t.Errorf("DeriveRecordingStatus() = %v, want %v", status, tt.expectedStatus)
			}
		})
	}
}
