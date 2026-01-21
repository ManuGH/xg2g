package recordings

import (
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/recordings/model"
	"github.com/stretchr/testify/assert"
)

// TestService_ClassifyFilePresence_Regression proves that the presence classification
// correctly handles the Filesize input, specifically protecting against the case where
// a missing (nil/zero) filesize leads to a 'Small' (failed) classification.
func TestService_ClassifyFilePresence_Regression(t *testing.T) {
	s := &service{}

	tests := []struct {
		name     string
		filesize interface{}
		want     model.FilePresenceClass
	}{
		{
			name:     "Case A: Filesize Missing (Nil) -> Failure Mode",
			filesize: nil,
			want:     model.FilePresenceSmall,
		},
		{
			name:     "Case B: Filesize Valid (1.35GB) -> Success Mode",
			filesize: "1452310716",
			want:     model.FilePresenceOK,
		},
		{
			name:     "Case C: Filesize 0 -> Small",
			filesize: "0",
			want:     model.FilePresenceSmall,
		},
		{
			name:     "Case D: Filesize Numerical Valid",
			filesize: int64(1452310716),
			want:     model.FilePresenceOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := OWIMovie{Filesize: tt.filesize}
			got := s.classifyFilePresence(m)
			assert.Equal(t, tt.want, got, "Filesize %v should result in %v", tt.filesize, tt.want)
		})
	}
}

// TestStatus_EndToEnd_Regression proves that restoring Filesize truth
// results in the correct 'completed' status for past recordings.
func TestStatus_EndToEnd_Regression(t *testing.T) {
	now := time.Now()
	// Past recording (outside grace period)
	timer := &model.TimerTruth{
		Begin: now.Add(-2 * time.Hour),
		End:   now.Add(-1 * time.Hour),
	}

	// Case 1: Filesize OK (Restored Truth)
	status, reason := model.DeriveRecordingStatus(now, model.FilePresenceOK, timer)
	assert.Equal(t, model.RecordingStatusCompleted, status)
	assert.Equal(t, "file_ok_no_active_timer", reason)

	// Case 2: Filesize Small (Missing/Lost Truth)
	status, reason = model.DeriveRecordingStatus(now, model.FilePresenceSmall, timer)
	assert.Equal(t, model.RecordingStatusFailed, status)
	assert.Contains(t, reason, "timer_past_file_small")
}
