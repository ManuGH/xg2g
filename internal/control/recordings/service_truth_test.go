package recordings

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
)

// MockResolverForTruth implements Resolver interface for testing truth propagation.
type MockResolverForTruth struct {
	Result PlaybackInfoResult
	Err    error
}

func (m *MockResolverForTruth) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	return m.Result, m.Err
}

func (m *MockResolverForTruth) GetMediaTruth(ctx context.Context, id string) (playback.MediaTruth, error) {
	// Not used in this specific test, but required by interface
	return playback.MediaTruth{}, nil
}

// TestResolvePlayback_TruthPropagation ensures that truthful fields (Container, Codecs)
// are correctly mapped from the Resolver result to the Service Resolution.
// This prevents regression of the "422 Decision Ambiguous" bug where Container was dropped.
func TestResolvePlayback_TruthPropagation(t *testing.T) {
	// Arrange
	container := "ts"
	video := "h264"
	audio := "ac3"

	mockRes := PlaybackInfoResult{
		Decision: playback.Decision{
			Mode: playback.ModeDirectPlay,
		},
		MediaInfo: playback.MediaInfo{
			Container:  container,
			VideoCodec: video,
			AudioCodec: audio,
		},
		Container:  &container,
		VideoCodec: &video,
		AudioCodec: &audio,
	}

	mockResolver := &MockResolverForTruth{
		Result: mockRes,
	}

	// We need a dummy config and nil managers since we only exercise the resolver path
	cfg := &config.AppConfig{}

	// Construct service manually to inject mock
	svc := &service{
		cfg:      cfg,
		resolver: mockResolver,
		// managers can be nil as ResolvePlayback -> GetPlaybackInfo -> resolver.Resolve
		// DOES NOT touch vodManager if resolver handles it.
		// Wait, GetPlaybackInfo calls DecodeRecordingID.
	}

	// Act
	// We use a valid ID format to pass DecodeRecordingID check
	// 1:0:0:0:0:0:0:0:0:0:/path/to/file.ts encoded hex
	validID := "313a303a303a303a303a303a303a303a303a303a2f706174682f746f2f66696c652e7473"

	res, err := svc.ResolvePlayback(context.Background(), validID, "generic")

	// Assert
	if err != nil {
		t.Fatalf("ResolvePlayback returned unexpected error: %v", err)
	}

	if res.Container == nil {
		t.Fatal("Regression: PlaybackResolution.Container is nil. Truth lost in mapping.")
	}
	if *res.Container != container {
		t.Errorf("PlaybackResolution.Container mismatch: got %q, want %q", *res.Container, container)
	}

	if res.VideoCodec == nil || *res.VideoCodec != video {
		t.Errorf("PlaybackResolution.VideoCodec mismatch/nil")
	}
	if res.AudioCodec == nil || *res.AudioCodec != audio {
		t.Errorf("PlaybackResolution.AudioCodec mismatch/nil")
	}
}

// Ensure mock meets interface
var _ Resolver = (*MockResolverForTruth)(nil)
