package model

import (
	"testing"
	"time"
)

// M5: during the post-roll grace period (window ended, not yet past grace), a small/partial
// file is still finalizing — it must NOT be reported FAILED.
func TestDeriveRecordingStatus_InGraceSmallFileStillRecording(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	timer := &TimerTruth{
		Begin: now.Add(-1 * time.Hour),
		End:   now.Add(-30 * time.Second), // window ended 30s ago; grace = End+120s → now is in grace
	}

	for _, file := range []FilePresenceClass{FilePresenceSmall, FilePresencePartial} {
		status, reason := DeriveRecordingStatus(now, file, timer)
		if status != RecordingStatusRecording {
			t.Fatalf("file=%v in grace period: got %v (%s), want Recording", file, status, reason)
		}
	}
}

// Regression guard: past the grace period, a small file is still FAILED.
func TestDeriveRecordingStatus_PastGraceSmallFileFailed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	timer := &TimerTruth{
		Begin: now.Add(-2 * time.Hour),
		End:   now.Add(-10 * time.Minute), // well past End + grace
	}
	status, _ := DeriveRecordingStatus(now, FilePresenceSmall, timer)
	if status != RecordingStatusFailed {
		t.Fatalf("past grace + small file: got %v, want Failed", status)
	}
}
