package bootstrap

import (
	"context"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
)

// mockRecordingsService implements recservice.Service for testing.
// Only ResolvePlayback is functional; other methods return empty results.
type mockRecordingsService struct {
	resolvePlayback func(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error)
}

func (m *mockRecordingsService) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	if m.resolvePlayback != nil {
		return m.resolvePlayback(ctx, recordingID, profile)
	}
	return recservice.PlaybackResolution{}, nil
}

func (m *mockRecordingsService) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	return recservice.ListResult{}, nil
}

func (m *mockRecordingsService) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}

func (m *mockRecordingsService) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	return recservice.StatusResult{}, nil
}

func (m *mockRecordingsService) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	return recservice.StreamResult{}, nil
}

func (m *mockRecordingsService) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}
