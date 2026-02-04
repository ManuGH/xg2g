package playback

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Definitions (Mocked for Tests) ---

type MockTruthProvider struct {
	mock.Mock
}

func (m *MockTruthProvider) GetMediaTruth(ctx context.Context, id string) (MediaTruth, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(MediaTruth), args.Error(1)
}

type MockProfileResolver struct {
	mock.Mock
}

func (m *MockProfileResolver) Resolve(ctx context.Context, headers map[string]string) (PlaybackCapabilities, error) {
	args := m.Called(ctx, headers)
	return args.Get(0).(PlaybackCapabilities), args.Error(1)
}

// --- Setup ---

func setupEngine(t *testing.T) (*DecisionEngine, *MockTruthProvider, *MockProfileResolver) {
	truth := new(MockTruthProvider)
	profile := new(MockProfileResolver)
	engine := NewDecisionEngine(truth, profile)
	return engine, truth, profile
}

// --- Group 1: Gating ---

func TestPlaybackInfo_G1_Unauthorized_FailsClosed(t *testing.T) {
	e, _, prof := setupEngine(t)
	// G1: Profile Resolver returns Forbidden -> Engine fails closed
	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{}, ErrForbidden)

	_, err := e.Resolve(context.Background(), ResolveRequest{RecordingID: "rec1"})
	assert.ErrorIs(t, err, ErrForbidden)
}

// --- Group 2: NotFound ---

func TestPlaybackInfo_G2_NotFound_Terminal(t *testing.T) {
	e, truth, prof := setupEngine(t)
	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{}, nil)
	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{}, ErrNotFound)

	_, err := e.Resolve(context.Background(), ResolveRequest{RecordingID: "rec1"})
	assert.ErrorIs(t, err, ErrNotFound)
}

// --- Group 3: Preparing Gate ---

func TestPlaybackInfo_G3_Preparing_ReturnsPreparing(t *testing.T) {
	e, truth, prof := setupEngine(t)
	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{}, nil)
	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{State: StatePreparing}, nil)

	_, err := e.Resolve(context.Background(), ResolveRequest{RecordingID: "rec1"})
	assert.ErrorIs(t, err, ErrPreparing)
}

// --- Group 4: DirectPlay MP4 ---

func TestPlaybackInfo_G4_DirectPlay_H264_AAC_MP4(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		Containers:  []string{"mp4"},
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "mp4"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ModeDirectPlay, plan.Mode)
	assert.Equal(t, ProtocolMP4, plan.Protocol)
	assert.Equal(t, ReasonDirectPlayMatch, plan.DecisionReason)
}

func TestPlaybackInfo_G4_DirectPlay_CaseInsensitiveTokens(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "MP4",
		VideoCodec: "H264",
		AudioCodec: "AAC",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		Containers:  []string{"mp4"},
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "MP4"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ModeDirectPlay, plan.Mode)
	assert.Equal(t, ProtocolMP4, plan.Protocol)
	assert.Equal(t, ReasonDirectPlayMatch, plan.DecisionReason)
}

// --- Group 5: DirectPlay HLS on Safari ---

func TestPlaybackInfo_G5_DirectPlay_SafariNative_HLS(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mpegts",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		SupportsHLS: true,
		Containers:  []string{"mpegts"},
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "hls"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ModeDirectPlay, plan.Mode)
	assert.Equal(t, ProtocolHLS, plan.Protocol)
	assert.Equal(t, ReasonDirectPlayMatch, plan.DecisionReason)
}

// --- Group 6: DirectStream Remux ---

func TestPlaybackInfo_G6_DirectStream_RemuxOnly(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mkv",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		SupportsHLS: true,
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "hls"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ModeDirectStream, plan.Mode)
	assert.Equal(t, ReasonDirectStreamMatch, plan.DecisionReason)
}

// --- Group 7: Transcode Video ---

func TestPlaybackInfo_G7_Transcode_VideoIncompatible_MPEG2(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mpegts",
		VideoCodec: "mpeg2video",
		AudioCodec: "mp2",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		VideoCodecs: []string{"h264"}, // No mpeg2
		AudioCodecs: []string{"mp2"},
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "hls"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, ModeTranscode, plan.Mode)
	assert.Equal(t, ReasonTranscodeVideo, plan.DecisionReason)
}

// --- Group 8: Transcode Audio Only ---

func TestPlaybackInfo_G8_Transcode_AudioOnly_AC3(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "ac3",
	}, nil)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"}, // No AC3
	}, nil)

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "hls"}
	plan, err := e.Resolve(context.Background(), req)

	assert.NoError(t, err)
	// Expecting Audio Transcode
	assert.Equal(t, ModeTranscode, plan.Mode)
	assert.Equal(t, ReasonTranscodeAudio, plan.DecisionReason)
}

// --- Group 9: Unknown Truth ---

func TestPlaybackInfo_G9_UnknownCodecTruth_FailsClosed(t *testing.T) {
	e, truth, prof := setupEngine(t)

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{}, nil)
	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State:      StateReady,
		Container:  "mp4",
		VideoCodec: "unknown",
		AudioCodec: "unknown",
	}, nil)

	_, err := e.Resolve(context.Background(), ResolveRequest{RecordingID: "rec1"})

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrDecisionAmbiguous)
}

// --- Group 10: Determinism ---

func TestPlaybackInfo_G10_RepeatedCalls_StableDecision(t *testing.T) {
	e, truth, prof := setupEngine(t)

	truth.On("GetMediaTruth", mock.Anything, "rec1").Return(MediaTruth{
		State: StateReady, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}, nil).Twice()

	prof.On("Resolve", mock.Anything, mock.Anything).Return(PlaybackCapabilities{
		Containers:  []string{"mp4"},
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac"},
	}, nil).Twice()

	req := ResolveRequest{RecordingID: "rec1", ProtocolHint: "mp4"}

	plan1, err1 := e.Resolve(context.Background(), req)
	plan2, err2 := e.Resolve(context.Background(), req)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.Equal(t, plan1, plan2)
}
