package recordings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/domain/recordings/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAutoPreparePreparer struct {
	mu       sync.Mutex
	prepared []string
	err      error
}

func (m *mockAutoPreparePreparer) EnsurePrepared(ctx context.Context, recordingID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared = append(m.prepared, recordingID)
	return m.err
}

type mockRecordingsServiceForAutoPrepare struct {
	items []RecordingItem
	err   error
}

func (m *mockRecordingsServiceForAutoPrepare) ResolvePlayback(ctx context.Context, recordingID, profile string) (PlaybackResolution, error) {
	return PlaybackResolution{}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) List(ctx context.Context, in ListInput) (ListResult, error) {
	if m.err != nil {
		return ListResult{}, m.err
	}
	return ListResult{Recordings: m.items}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) GetPlaybackInfo(ctx context.Context, in PlaybackInfoInput) (PlaybackInfoResult, error) {
	return PlaybackInfoResult{}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) GetStatus(ctx context.Context, in StatusInput) (StatusResult, error) {
	return StatusResult{}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) Stream(ctx context.Context, in StreamInput) (StreamResult, error) {
	return StreamResult{}, nil
}

func (m *mockRecordingsServiceForAutoPrepare) Delete(ctx context.Context, in DeleteInput) (DeleteResult, error) {
	return DeleteResult{}, nil
}

func TestAutoPrepareWorker_DisabledDoesNotRun(t *testing.T) {
	preparer := &mockAutoPreparePreparer{}
	recSvc := &mockRecordingsServiceForAutoPrepare{
		items: []RecordingItem{
			{ServiceRef: "1:0:0:0:0:0:0:0:0:0:/movie/test.ts", Status: model.RecordingStatusCompleted},
		},
	}

	worker := NewAutoPrepareWorker(AutoPrepareConfig{
		Enabled:  false,
		Interval: 10 * time.Millisecond,
	}, recSvc, preparer, nil)

	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()

	preparer.mu.Lock()
	assert.Empty(t, preparer.prepared)
	preparer.mu.Unlock()
}

func TestAutoPrepareWorker_ProcessesOnlyCompletedRecordings(t *testing.T) {
	preparer := &mockAutoPreparePreparer{}
	recSvc := &mockRecordingsServiceForAutoPrepare{
		items: []RecordingItem{
			{ServiceRef: "1:0:0:0:0:0:0:0:0:0:/movie/in_progress.ts", Title: "Recording Now", Status: model.RecordingStatusRecording},
			{ServiceRef: "1:0:0:0:0:0:0:0:0:0:/movie/completed.ts", Title: "Unser Charly", Status: model.RecordingStatusCompleted},
			{ServiceRef: "1:0:0:0:0:0:0:0:0:0:/movie/scheduled.ts", Title: "Future Event", Status: model.RecordingStatusScheduled},
		},
	}

	worker := NewAutoPrepareWorker(AutoPrepareConfig{
		Enabled:       true,
		Interval:      60 * time.Second,
		MaxConcurrent: 1,
	}, recSvc, preparer, nil)

	worker.RunOnce(context.Background())

	preparer.mu.Lock()
	defer preparer.mu.Unlock()
	require.Len(t, preparer.prepared, 1)
	assert.Equal(t, "1:0:0:0:0:0:0:0:0:0:/movie/completed.ts", preparer.prepared[0])
}

func TestAutoPrepareWorker_BacksOffRepeatedFailures(t *testing.T) {
	preparer := &mockAutoPreparePreparer{
		err: errors.New("file unreadable"),
	}
	ref := "1:0:0:0:0:0:0:0:0:0:/movie/corrupt.ts"
	recSvc := &mockRecordingsServiceForAutoPrepare{
		items: []RecordingItem{
			{ServiceRef: ref, Status: model.RecordingStatusCompleted},
		},
	}

	worker := NewAutoPrepareWorker(AutoPrepareConfig{
		Enabled:       true,
		Interval:      60 * time.Second,
		MaxConcurrent: 1,
	}, recSvc, preparer, nil)

	// Run 4 times
	for i := 0; i < 4; i++ {
		worker.RunOnce(context.Background())
	}

	preparer.mu.Lock()
	defer preparer.mu.Unlock()
	// Should attempt exactly 3 times, and skip the 4th
	assert.Equal(t, 3, len(preparer.prepared))
}
