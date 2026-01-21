package recordings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// Unique Mocks for service_test.go
// (MockOWIClient, MockProber, MockPathMapper are already in duration_truth_test.go)

type MockResumeStore struct {
	mock.Mock
}

func (m *MockResumeStore) GetResume(ctx context.Context, principalID, serviceRef string) (ResumeData, bool, error) {
	args := m.Called(ctx, principalID, serviceRef)
	return args.Get(0).(ResumeData), args.Bool(1), args.Error(2)
}

type MockResolverForService struct {
	mock.Mock
}

func (m *MockResolverForService) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	args := m.Called(ctx, serviceRef, intent, profile)
	return args.Get(0).(PlaybackInfoResult), args.Error(1)
}

func (m *MockResolverForService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	// Simple stub for tests that don't verify truth specifically
	return playback.MediaTruth{}, nil
}

type MockRunnerForService struct {
	mock.Mock
}

func (m *MockRunnerForService) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	args := m.Called(ctx, spec)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(vod.Handle), args.Error(1)
}

type MockHandleForService struct {
	mock.Mock
}

func (m *MockHandleForService) Wait() error { return m.Called().Error(0) }
func (m *MockHandleForService) Stop(grace, kill time.Duration) error {
	return m.Called(grace, kill).Error(0)
}
func (m *MockHandleForService) Progress() <-chan vod.ProgressEvent {
	return m.Called().Get(0).(chan vod.ProgressEvent)
}
func (m *MockHandleForService) Diagnostics() []string { return m.Called().Get(0).([]string) }

func setupServiceTest(t *testing.T) (*service, *MockOWIClient, *MockResumeStore, *MockResolverForService, *vod.Manager) {
	cfg := &config.AppConfig{
		RecordingRoots: map[string]string{
			"movies": "/media/hdd/movie",
		},
		HLS: config.HLSConfig{
			Root: "/tmp/xg2g-hls",
		},
	}
	owi := new(MockOWIClient)
	res := new(MockResumeStore)
	slv := new(MockResolverForService)

	// Lite VOD Manager
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	mapper := new(MockPathMapper)
	vm, err := vod.NewManager(runner, prober, mapper)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	s := &service{
		cfg:         cfg,
		owiClient:   owi,
		resumeStore: res,
		resolver:    slv,
		vodManager:  vm,
	}
	return s, owi, res, slv, vm
}

const validTestRef = "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"

func TestService_List_Success(t *testing.T) {
	s, owi, _, _, _ := setupServiceTest(t)

	owi.On("GetLocations", mock.Anything).Return([]OWILocation{}, nil)
	owi.On("GetTimers", mock.Anything).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", mock.Anything, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{
				ServiceRef: validTestRef,
				Title:      "Title 1",
				Length:     "01:00:00",
			},
		},
	}, nil)

	result, err := s.List(context.Background(), ListInput{RootID: "movies"})
	assert.NoError(t, err)
	assert.Len(t, result.Recordings, 1)
	assert.Equal(t, "Title 1", result.Recordings[0].Title)
	assert.Equal(t, EncodeRecordingID(validTestRef), result.Recordings[0].RecordingID)
}

func TestService_ResolvePlayback_Success(t *testing.T) {
	s, _, _, slv, _ := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	slv.On("Resolve", mock.Anything, validTestRef, PlaybackIntent("stream"), PlaybackProfile("hd")).Return(PlaybackInfoResult{
		Decision: playback.Decision{
			Artifact: playback.ArtifactHLS,
		},
		MediaInfo: playback.MediaInfo{
			Duration: 3600,
		},
		Reason: "test",
	}, nil)

	result, err := s.ResolvePlayback(context.Background(), hexID, "hd")
	assert.NoError(t, err)
	assert.Equal(t, StrategyHLS, result.Strategy)
	assert.True(t, result.CanSeek)
}

func TestService_GetStatus_Ready(t *testing.T) {
	s, _, _, _, vm := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	vm.SeedMetadata(validTestRef, vod.Metadata{
		State: vod.ArtifactStateReady,
	})

	result, err := s.GetStatus(context.Background(), StatusInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.Equal(t, "READY", result.State)
}

func TestService_Stream_NotReady_TriggersProbe(t *testing.T) {
	s, _, _, _, vm := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	// Metadata doesn't exist yet

	result, err := s.Stream(context.Background(), StreamInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.False(t, result.Ready)
	assert.Equal(t, "UNKNOWN", result.State)

	// Verify probe triggered (using Eventually for async)
	assert.Eventually(t, func() bool {
		meta, ok := vm.GetMetadata(validTestRef)
		return ok && meta.State == vod.ArtifactStatePreparing
	}, 1*time.Second, 10*time.Millisecond)
}

func TestService_Delete_Success(t *testing.T) {
	s, owi, _, _, _ := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	owi.On("DeleteRecording", mock.Anything, validTestRef).Return(nil)

	result, err := s.Delete(context.Background(), DeleteInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.True(t, result.Deleted)
	owi.AssertExpectations(t)
}
